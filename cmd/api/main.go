// Command api serves the Scholia HTTP API, locally or as a Lambda function URL.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-lambda-go/lambdaurl"
	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/sumonmselim/scholia-aws/internal/answer"
	"github.com/sumonmselim/scholia-aws/internal/auth"
	"github.com/sumonmselim/scholia-aws/internal/awscfg"
	"github.com/sumonmselim/scholia-aws/internal/config"
	"github.com/sumonmselim/scholia-aws/internal/embed"
	"github.com/sumonmselim/scholia-aws/internal/httpapi"
	"github.com/sumonmselim/scholia-aws/internal/ingest"
	"github.com/sumonmselim/scholia-aws/internal/keyring"
	"github.com/sumonmselim/scholia-aws/internal/killswitch"
	"github.com/sumonmselim/scholia-aws/internal/limit"
	"github.com/sumonmselim/scholia-aws/internal/model"
	"github.com/sumonmselim/scholia-aws/internal/retrieve"
	"github.com/sumonmselim/scholia-aws/internal/seal"
	"github.com/sumonmselim/scholia-aws/internal/secret"
	"github.com/sumonmselim/scholia-aws/internal/serve"
	"github.com/sumonmselim/scholia-aws/internal/store"
	"github.com/sumonmselim/scholia-aws/internal/vector"
	"github.com/sumonmselim/scholia-aws/internal/version"
	"github.com/sumonmselim/scholia-aws/internal/websearch"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	if err := cfg.RequireSignIn(); err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	if cfg.SessionSecret == "" && cfg.SessionSecretARN != "" {
		// Secrets Manager is not on floci, so this reads the real service.
		smCfg, err := awscfg.Load(context.Background(), cfg.AWSRegion, "")
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		value, err := secret.Plain(ctx, secret.OpenSecretsManager(smCfg), cfg.SessionSecretARN, config.MinSessionSecret)
		cancel()
		if err != nil {
			// The error names the operation only; the value never reaches the log.
			return fmt.Errorf("read the session secret: %w", err)
		}
		cfg.SessionSecret = value
	}

	opts := httpapi.Options{
		Logger:  log,
		Version: version.Version,
		Commit:  version.Commit,
		Env:     cfg.Env,
	}
	if cfg.TableName != "" && cfg.UploadsBucket != "" {
		awsCfg, err := awscfg.Load(context.Background(), cfg.AWSRegion, cfg.AWSEndpoint)
		if err != nil {
			return err
		}
		db, err := store.NewClient(context.Background(), cfg.AWSRegion, cfg.AWSEndpoint)
		if err != nil {
			return err
		}
		repo, err := store.New(db, cfg.TableName)
		if err != nil {
			return err
		}
		opts.Sources = repo
		bucket := ingest.OpenBucket(awsCfg, cfg.AWSEndpoint).WithPublicEndpoint(cfg.PublicS3Endpoint)
		opts.Presign = bucket
		// A user's Bedrock key calls AWS itself, so its config has no floci endpoint.
		bedrockCfg, err := awscfg.Load(context.Background(), cfg.AWSRegion, "")
		if err != nil {
			return err
		}
		// The kill switch is operator config in SSM, which floci does not serve.
		// In prod an unreadable switch pauses server-paid models until a good read.
		var sw *killswitch.Switch
		if cfg.KillSwitchParam != "" {
			sw = &killswitch.Switch{Name: cfg.KillSwitchParam, Params: killswitch.OpenSSM(bedrockCfg), FailClosed: cfg.Env == config.EnvProd}
		}
		opts.Switch = sw
		resolver := &keyring.Resolver{Keys: repo, AWS: bedrockCfg, Fallback: cfg.DefaultModel}
		// Server Bedrock is paid by the deployment and uses the API role. Config
		// refuses it with a floci endpoint, so awsCfg reaches the real service here.
		// Only this client carries the guardrail: a user's own key is their account.
		var server model.Provider
		if cfg.ServerBedrock {
			server = model.OpenBedrock(awsCfg, "", cfg.DefaultModel).WithGuardrail(cfg.GuardrailID, cfg.GuardrailVersion)
			resolver.Server = server
			resolver.ServerAllowed = func(ctx context.Context) bool { return sw.State(ctx).ServerModels }
		}
		limits := httpapi.Limits{
			User:  httpapi.Quota{Messages: cfg.UserDailyMessages, Uploads: cfg.UserDailyUploads, Web: cfg.UserDailyWeb},
			Guest: httpapi.Quota{Messages: cfg.GuestDailyMessages, Uploads: cfg.GuestDailyUploads, Web: cfg.GuestDailyWeb},
		}
		opts.Usage = repo
		opts.Limits = limits
		opts.DefaultChatModel = cfg.DefaultModel
		opts.EmbedModel = cfg.EmbedModel
		opts.ServerModels = server != nil
		opts.UploadsBucket = cfg.UploadsBucket
		if cfg.VectorBucket != "" {
			vectors, err := vector.Open(awsCfg, cfg.AWSEndpoint, cfg.VectorBucket)
			if err != nil {
				return err
			}
			// A stored key calls its provider directly. Without one, a Bedrock model
			// uses server Bedrock when it is on, and anything else gets the mock.
			// Floci does not emulate Bedrock. The local vector index is
			// the Titan width, so a compose run uses a deterministic embedder
			// of that width and still ranks chunks with BM25.
			var embedder embed.Embedder = embed.Open(awsCfg, cfg.AWSEndpoint)
			if cfg.AWSEndpoint != "" {
				embedder = &embed.Fake{Dim: 1024}
			}
			// A web.Searcher holding a nil *Tavily would not be nil, so only set a real one.
			var web websearch.Searcher
			if tavily := websearch.OpenTavily(tavilyKey(log, cfg, bedrockCfg)); tavily != nil {
				web = tavily
				opts.WebSearch = true
			}
			bound := answer.Bound{
				Service: &answer.Service{
					Search: &retrieve.Searcher{
						Chunks:  repo,
						Vectors: vectors,
						Embed:   embedder,
					},
					Model:      model.Mock{},
					Web:        web,
					ChatModel:  cfg.DefaultModel,
					EmbedModel: cfg.EmbedModel,
					MaxTokens:  cfg.MaxOutputTokens,
					WebAllowed: httpapi.NewWebMeter(repo, limits, time.Now),
				},
				Courses: repo,
				// resolver.Box is set below, before the handler serves.
				Providers:   resolver,
				Preferences: repo,
			}
			opts.Answers = bound
			opts.Tutor = answer.TutorMode{Bound: bound}
		}
		opts.Keys = repo
		opts.Settings = repo
		opts.Chats = repo
		opts.Attachments = repo
		opts.Objects = bucket
		switch {
		case cfg.KMSKeyID != "":
			// KMS is not on floci. OpenKMS pins the real host.
			opts.Box = seal.OpenKMS(awsCfg, cfg.KMSKeyID)
		case cfg.Env == config.EnvLocal && cfg.SessionSecret != "":
			// Compose has no KMS, so keys are sealed with the local session secret.
			box, err := seal.OpenLocal(cfg.SessionSecret)
			if err != nil {
				return err
			}
			opts.Box = box
		}
		resolver.Box = opts.Box
	}
	if cfg.CognitoClientID != "" {
		// Empty endpoint: Cognito is not on floci. OpenCognito also pins the real host.
		awsCfg, err := awscfg.Load(context.Background(), cfg.AWSRegion, "")
		if err != nil {
			return err
		}
		opts.Sessions = auth.OpenCognito(awsCfg, cfg.CognitoClientID)
		opts.SessionSecret = cfg.SessionSecret
		opts.Gate = limit.New(cfg.AnswerRatePerMinute, cfg.AnonDailyAnswers)
	} else if cfg.SessionSecret != "" && cfg.Env == config.EnvLocal {
		opts.Sessions = &auth.Fake{Subject: "local-owner", Code: "123456"}
		opts.SessionSecret = cfg.SessionSecret
		opts.Gate = limit.New(cfg.AnswerRatePerMinute, cfg.AnonDailyAnswers)
	}
	opts.Guests = cfg.Guests
	opts.GuestTTL = time.Duration(cfg.GuestTTLHours) * time.Hour
	opts.GuestGate = limit.NewWindow(cfg.GuestSessionsPerHour, time.Hour)
	handler := httpapi.New(opts)

	if serve.InLambda(os.Getenv) {
		lambdaurl.Start(handler)
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serve.HTTP(ctx, log, fmt.Sprintf(":%d", cfg.Port), handler)
}

// tavilyKey reads the web search key. A Secrets Manager secret wins over the
// env var. Any failure turns web search off with one warning that never
// includes the value.
func tavilyKey(log *slog.Logger, cfg config.Config, awsCfg aws.Config) string {
	if cfg.TavilySecretARN == "" {
		return cfg.TavilyAPIKey
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	key, err := secret.APIKey(ctx, secret.OpenSecretsManager(awsCfg), cfg.TavilySecretARN)
	if err != nil {
		log.Warn("web search is off: the Tavily secret could not be read")
		return ""
	}
	return key
}
