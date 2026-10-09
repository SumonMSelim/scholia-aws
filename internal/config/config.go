// Package config loads runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/sumonmselim/scholia-aws/internal/embed"
	"github.com/sumonmselim/scholia-aws/internal/model"
	"github.com/sumonmselim/scholia-aws/internal/vector"
)

// Environment names accepted in SCHOLIA_ENV.
const (
	EnvLocal = "local"
	EnvProd  = "prod"
)

// Config is the process configuration shared by the API and the worker.
type Config struct {
	Env      string
	Port     int
	LogLevel slog.Level
	// AWSEndpoint overrides AWS service endpoints; set to floci locally, empty in AWS.
	AWSEndpoint string
	// PublicS3Endpoint signs browser upload URLs when the browser cannot reach
	// AWSEndpoint, as with the compose-internal floci host. Local only.
	PublicS3Endpoint string
	AWSRegion        string
	TableName        string
	UploadsBucket    string
	// QueueName is the local worker's SQS queue. Lambda receives events directly.
	QueueName string
	// VectorBucket is the S3 Vectors bucket. Empty leaves the answer route off.
	VectorBucket string
	// CognitoClientID and SessionSecret turn sign-in on together in prod.
	// In local env, SessionSecret alone enables Fake auth for development.
	// Both empty leaves the API open without sign-in.
	CognitoClientID string
	SessionSecret   string
	// SessionSecretARN names a Secrets Manager secret holding SessionSecret as a
	// plain string. When SessionSecret is empty the API reads it at cold start.
	SessionSecretARN string
	// AnswerRatePerMinute applies to every caller of the answer route.
	AnswerRatePerMinute int
	// AnonDailyAnswers is the anonymous question allowance for one UTC day.
	AnonDailyAnswers int
	// KMSKeyID encrypts provider keys. Empty leaves that route off.
	KMSKeyID string
	// TavilyAPIKey is the server-wide web search key. Empty leaves web search off.
	// It is a secret: it is never logged or returned.
	TavilyAPIKey string
	// ServerBedrock lets the API call Bedrock with its own AWS credentials when a
	// caller has no key. SCHOLIA_BEDROCK is on or off; unset is on in prod only.
	// Floci has no Bedrock, so it cannot be on with AWS_ENDPOINT_URL.
	ServerBedrock bool
	// DefaultModel answers when neither the chat nor the course names a model.
	// It must be a catalog model.
	DefaultModel string
	// MaxOutputTokens caps every model reply to bound cost.
	MaxOutputTokens int
	// EmbedModel indexes course material when a course names none. It must name
	// a vector index, so an unsupported model fails at startup.
	EmbedModel string
	// TavilySecretARN names a Secrets Manager secret holding the Tavily key, as
	// the plain key or {"api_key":"..."}. It is read once at cold start and wins
	// over TavilyAPIKey, which stays for local runs.
	TavilySecretARN string
	// Guests turns on one-click guest sessions. SCHOLIA_GUESTS is on or off; unset is on.
	Guests bool
	// GuestTTLHours is how long a guest session and its records last.
	GuestTTLHours int
	// GuestSessionsPerHour caps new guest sessions per client address.
	GuestSessionsPerHour int
	// Daily allowances per subject. Guests and anonymous callers use the guest tier.
	UserDailyMessages  int
	UserDailyUploads   int
	UserDailyWeb       int
	GuestDailyMessages int
	GuestDailyUploads  int
	GuestDailyWeb      int
	// KillSwitchParam is the SSM parameter holding the pause flags. Empty leaves everything on.
	KillSwitchParam string
	// GuardrailID and GuardrailVersion apply a Bedrock guardrail to server-paid calls.
	// Both are set or both are empty.
	GuardrailID      string
	GuardrailVersion string
}

const maxOutputTokensLimit = 32000

// MinSessionSecret is the shortest session secret the API accepts.
const MinSessionSecret = 16

// Load reads configuration using getenv, typically os.Getenv.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Env:              valueOr(getenv("SCHOLIA_ENV"), EnvLocal),
		AWSEndpoint:      strings.TrimSpace(getenv("AWS_ENDPOINT_URL")),
		PublicS3Endpoint: strings.TrimSpace(getenv("SCHOLIA_PUBLIC_S3_ENDPOINT")),
		AWSRegion:        valueOr(getenv("AWS_REGION"), "us-east-1"),
		TableName:        strings.TrimSpace(getenv("TABLE_NAME")),
		UploadsBucket:    strings.TrimSpace(getenv("UPLOADS_BUCKET")),
		QueueName:        strings.TrimSpace(getenv("QUEUE_NAME")),
		VectorBucket:     strings.TrimSpace(getenv("SCHOLIA_VECTOR_BUCKET")),
		CognitoClientID:  strings.TrimSpace(getenv("SCHOLIA_COGNITO_CLIENT_ID")),
		SessionSecret:    strings.TrimSpace(getenv("SCHOLIA_SESSION_SECRET")),
		SessionSecretARN: strings.TrimSpace(getenv("SCHOLIA_SESSION_SECRET_ARN")),
		KMSKeyID:         strings.TrimSpace(getenv("SCHOLIA_KMS_KEY_ID")),
		TavilyAPIKey:     strings.TrimSpace(getenv("TAVILY_API_KEY")),
		DefaultModel:     valueOr(getenv("SCHOLIA_DEFAULT_MODEL"), model.DefaultModel),
		EmbedModel:       valueOr(getenv("SCHOLIA_EMBED_MODEL"), embed.DefaultModel),
		TavilySecretARN:  strings.TrimSpace(getenv("SCHOLIA_TAVILY_SECRET_ARN")),
		KillSwitchParam:  strings.TrimSpace(getenv("SCHOLIA_KILL_SWITCH_PARAM")),
		GuardrailID:      strings.TrimSpace(getenv("SCHOLIA_GUARDRAIL_ID")),
		GuardrailVersion: strings.TrimSpace(getenv("SCHOLIA_GUARDRAIL_VERSION")),
	}

	var errs []error

	if cfg.Env != EnvLocal && cfg.Env != EnvProd {
		errs = append(errs, fmt.Errorf("SCHOLIA_ENV must be %q or %q, got %q", EnvLocal, EnvProd, cfg.Env))
	}
	// Local turns on development shortcuts such as a fixed sign-in code. A Lambda
	// that forgot SCHOLIA_ENV must not fall back to them.
	if cfg.Env == EnvLocal && getenv("AWS_LAMBDA_RUNTIME_API") != "" {
		errs = append(errs, errors.New("SCHOLIA_ENV must be prod in Lambda"))
	}

	port, err := strconv.Atoi(valueOr(getenv("PORT"), "8787"))
	if err != nil || port < 1 || port > 65535 {
		errs = append(errs, fmt.Errorf("PORT must be an integer between 1 and 65535, got %q", getenv("PORT")))
	}
	cfg.Port = port

	if err := cfg.LogLevel.UnmarshalText([]byte(valueOr(getenv("LOG_LEVEL"), "info"))); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL: %w", err))
	}

	if cfg.Env == EnvProd && cfg.AWSEndpoint != "" {
		errs = append(errs, errors.New("AWS_ENDPOINT_URL must not be set in prod"))
	}
	if cfg.PublicS3Endpoint != "" && cfg.AWSEndpoint == "" {
		errs = append(errs, errors.New("SCHOLIA_PUBLIC_S3_ENDPOINT needs AWS_ENDPOINT_URL"))
	}

	rate, err := strconv.Atoi(valueOr(getenv("SCHOLIA_ANSWER_RATE_PER_MINUTE"), "20"))
	if err != nil || rate < 1 {
		errs = append(errs, fmt.Errorf("SCHOLIA_ANSWER_RATE_PER_MINUTE must be a positive integer, got %q", getenv("SCHOLIA_ANSWER_RATE_PER_MINUTE")))
	}
	cfg.AnswerRatePerMinute = rate

	daily, err := strconv.Atoi(valueOr(getenv("SCHOLIA_ANON_DAILY_ANSWERS"), "50"))
	if err != nil || daily < 1 {
		errs = append(errs, fmt.Errorf("SCHOLIA_ANON_DAILY_ANSWERS must be a positive integer, got %q", getenv("SCHOLIA_ANON_DAILY_ANSWERS")))
	}
	cfg.AnonDailyAnswers = daily

	switch strings.ToLower(strings.TrimSpace(getenv("SCHOLIA_BEDROCK"))) {
	case "":
		cfg.ServerBedrock = cfg.Env == EnvProd
	case "on":
		cfg.ServerBedrock = true
	case "off":
	default:
		errs = append(errs, fmt.Errorf("SCHOLIA_BEDROCK must be on or off, got %q", getenv("SCHOLIA_BEDROCK")))
	}
	if cfg.ServerBedrock && cfg.AWSEndpoint != "" {
		errs = append(errs, errors.New("SCHOLIA_BEDROCK cannot be on with AWS_ENDPOINT_URL: floci has no Bedrock"))
	}

	provider, ok := model.ProviderOf(cfg.DefaultModel)
	if !ok {
		errs = append(errs, fmt.Errorf("SCHOLIA_DEFAULT_MODEL must be a catalog chat model, got %q", cfg.DefaultModel))
	}
	// Server Bedrock falls back to the default for callers without a key, so it must be one Bedrock serves.
	if ok && cfg.ServerBedrock && provider != model.ProviderBedrock {
		errs = append(errs, fmt.Errorf("SCHOLIA_DEFAULT_MODEL must be a Bedrock model when SCHOLIA_BEDROCK is on, got %q", cfg.DefaultModel))
	}
	if _, err := vector.IndexName(cfg.EmbedModel); err != nil || !embed.Supported(cfg.EmbedModel) {
		errs = append(errs, fmt.Errorf("SCHOLIA_EMBED_MODEL must be an Amazon Titan text embedding model, got %q", cfg.EmbedModel))
	}

	tokens, err := strconv.Atoi(valueOr(getenv("SCHOLIA_MAX_OUTPUT_TOKENS"), strconv.Itoa(model.DefaultMaxTokens)))
	if err != nil || tokens < 1 || tokens > maxOutputTokensLimit {
		errs = append(errs, fmt.Errorf("SCHOLIA_MAX_OUTPUT_TOKENS must be an integer from 1 to %d, got %q", maxOutputTokensLimit, getenv("SCHOLIA_MAX_OUTPUT_TOKENS")))
	}
	cfg.MaxOutputTokens = tokens

	switch strings.ToLower(strings.TrimSpace(getenv("SCHOLIA_GUESTS"))) {
	case "", "on":
		cfg.Guests = true
	case "off":
	default:
		errs = append(errs, fmt.Errorf("SCHOLIA_GUESTS must be on or off, got %q", getenv("SCHOLIA_GUESTS")))
	}
	for _, n := range []struct {
		name     string
		fallback int
		max      int
		dest     *int
	}{
		{"SCHOLIA_GUEST_TTL_HOURS", 48, 24 * 30, &cfg.GuestTTLHours},
		{"SCHOLIA_GUEST_SESSIONS_PER_HOUR", 5, 1000, &cfg.GuestSessionsPerHour},
		{"SCHOLIA_USER_DAILY_MESSAGES", 100, 100000, &cfg.UserDailyMessages},
		{"SCHOLIA_USER_DAILY_UPLOADS", 30, 100000, &cfg.UserDailyUploads},
		{"SCHOLIA_USER_DAILY_WEB", 40, 100000, &cfg.UserDailyWeb},
		{"SCHOLIA_GUEST_DAILY_MESSAGES", 30, 100000, &cfg.GuestDailyMessages},
		{"SCHOLIA_GUEST_DAILY_UPLOADS", 5, 100000, &cfg.GuestDailyUploads},
		{"SCHOLIA_GUEST_DAILY_WEB", 10, 100000, &cfg.GuestDailyWeb},
	} {
		v, err := strconv.Atoi(valueOr(getenv(n.name), strconv.Itoa(n.fallback)))
		if err != nil || v < 1 || v > n.max {
			errs = append(errs, fmt.Errorf("%s must be an integer from 1 to %d, got %q", n.name, n.max, getenv(n.name)))
		}
		*n.dest = v
	}
	if (cfg.GuardrailID == "") != (cfg.GuardrailVersion == "") {
		errs = append(errs, errors.New("SCHOLIA_GUARDRAIL_ID and SCHOLIA_GUARDRAIL_VERSION must both be set or both be empty"))
	}

	// The secret is the value itself or a Secrets Manager ARN the API reads at
	// start. Only a value can be length-checked here; the API checks the fetched one.
	secret := cfg.SessionSecret != "" || cfg.SessionSecretARN != ""
	switch {
	case cfg.CognitoClientID == "" && !secret:
	case cfg.Env == EnvLocal && cfg.CognitoClientID == "" && secret:
		if cfg.SessionSecret != "" && len(cfg.SessionSecret) < MinSessionSecret {
			errs = append(errs, errors.New("SCHOLIA_SESSION_SECRET must be at least 16 characters"))
		}
	case cfg.CognitoClientID == "" || !secret:
		errs = append(errs, errors.New("SCHOLIA_COGNITO_CLIENT_ID and SCHOLIA_SESSION_SECRET (or SCHOLIA_SESSION_SECRET_ARN) must both be set or both be empty"))
	case cfg.SessionSecret != "" && len(cfg.SessionSecret) < MinSessionSecret:
		errs = append(errs, errors.New("SCHOLIA_SESSION_SECRET must be at least 16 characters"))
	}

	return cfg, errors.Join(errs...)
}

// RequireSignIn fails in prod unless Cognito and a session secret are both set.
// Without them the API serves every course to anyone, so a prod API must not start.
// The worker does not serve requests and does not call it.
func (c Config) RequireSignIn() error {
	if c.Env != EnvProd {
		return nil
	}
	if c.CognitoClientID == "" || (c.SessionSecret == "" && c.SessionSecretARN == "") {
		return errors.New("prod requires SCHOLIA_COGNITO_CLIENT_ID and SCHOLIA_SESSION_SECRET or SCHOLIA_SESSION_SECRET_ARN")
	}
	return nil
}

func valueOr(v, fallback string) string {
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return fallback
}
