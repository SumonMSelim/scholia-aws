package config

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/sumonmselim/scholia-aws/internal/model"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		Env: EnvLocal, Port: 8787, LogLevel: slog.LevelInfo, AWSRegion: "us-east-1",
		AnswerRatePerMinute: 20, AnonDailyAnswers: 50,
		DefaultModel: model.DefaultModel, MaxOutputTokens: model.DefaultMaxTokens,
		EmbedModel: "amazon.titan-embed-text-v2:0",
		Guests:     true, GuestTTLHours: 48, GuestSessionsPerHour: 5,
		UserDailyMessages: 100, UserDailyUploads: 30, UserDailyWeb: 40,
		GuestDailyMessages: 30, GuestDailyUploads: 5, GuestDailyWeb: 10,
	}
	if cfg != want {
		t.Fatalf("got %+v, want %+v", cfg, want)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"SCHOLIA_ENV":      "prod",
		"PORT":             " 9000 ",
		"LOG_LEVEL":        "debug",
		"AWS_REGION":       "eu-central-1",
		"AWS_ENDPOINT_URL": "",
		"TABLE_NAME":       "scholia-prod",
		"UPLOADS_BUCKET":   "scholia-prod-uploads",
		"QUEUE_NAME":       "scholia-local-uploads",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Env != EnvProd || cfg.Port != 9000 || cfg.LogLevel != slog.LevelDebug || cfg.AWSRegion != "eu-central-1" ||
		cfg.TableName != "scholia-prod" || cfg.UploadsBucket != "scholia-prod-uploads" || cfg.QueueName != "scholia-local-uploads" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"unknown env", map[string]string{"SCHOLIA_ENV": "staging"}, "SCHOLIA_ENV"},
		{"non-numeric port", map[string]string{"PORT": "abc"}, "PORT"},
		{"port out of range", map[string]string{"PORT": "70000"}, "PORT"},
		{"zero port", map[string]string{"PORT": "0"}, "PORT"},
		{"bad log level", map[string]string{"LOG_LEVEL": "loud"}, "LOG_LEVEL"},
		{"endpoint in prod", map[string]string{"SCHOLIA_ENV": "prod", "AWS_ENDPOINT_URL": "http://floci:4566"}, "AWS_ENDPOINT_URL"},
		{"public s3 without floci", map[string]string{"SCHOLIA_PUBLIC_S3_ENDPOINT": "http://localhost:4566"}, "SCHOLIA_PUBLIC_S3_ENDPOINT needs"},
		{"bedrock with floci", map[string]string{"SCHOLIA_BEDROCK": "on", "AWS_ENDPOINT_URL": "http://floci:4566"}, "SCHOLIA_BEDROCK cannot"},
		{"bad bedrock switch", map[string]string{"SCHOLIA_BEDROCK": "yes"}, "SCHOLIA_BEDROCK must"},
		{"default model outside catalog", map[string]string{"SCHOLIA_DEFAULT_MODEL": "gpt-9"}, "SCHOLIA_DEFAULT_MODEL"},
		{"zero output tokens", map[string]string{"SCHOLIA_MAX_OUTPUT_TOKENS": "0"}, "SCHOLIA_MAX_OUTPUT_TOKENS"},
		{"huge output tokens", map[string]string{"SCHOLIA_MAX_OUTPUT_TOKENS": "999999"}, "SCHOLIA_MAX_OUTPUT_TOKENS"},
		{"client without secret", map[string]string{"SCHOLIA_COGNITO_CLIENT_ID": "abc"}, "SCHOLIA_SESSION_SECRET"},
		{"secret without client in prod", map[string]string{"SCHOLIA_ENV": "prod", "SCHOLIA_SESSION_SECRET": "session-secret-16"}, "SCHOLIA_COGNITO_CLIENT_ID"},
		{"short secret", map[string]string{"SCHOLIA_COGNITO_CLIENT_ID": "abc", "SCHOLIA_SESSION_SECRET": "short"}, "at least 16"},
		{"bad rate", map[string]string{"SCHOLIA_ANSWER_RATE_PER_MINUTE": "0"}, "SCHOLIA_ANSWER_RATE_PER_MINUTE"},
		{"bad daily cap", map[string]string{"SCHOLIA_ANON_DAILY_ANSWERS": "no"}, "SCHOLIA_ANON_DAILY_ANSWERS"},
		{"bad guests switch", map[string]string{"SCHOLIA_GUESTS": "maybe"}, "SCHOLIA_GUESTS must"},
		{"zero guest ttl", map[string]string{"SCHOLIA_GUEST_TTL_HOURS": "0"}, "SCHOLIA_GUEST_TTL_HOURS"},
		{"bad user cap", map[string]string{"SCHOLIA_USER_DAILY_MESSAGES": "lots"}, "SCHOLIA_USER_DAILY_MESSAGES"},
		{"huge guest cap", map[string]string{"SCHOLIA_GUEST_DAILY_WEB": "1000000"}, "SCHOLIA_GUEST_DAILY_WEB"},
		{"guardrail without version", map[string]string{"SCHOLIA_GUARDRAIL_ID": "gr-1"}, "SCHOLIA_GUARDRAIL_VERSION"},
		{"unsupported embed model", map[string]string{"SCHOLIA_EMBED_MODEL": "cohere.embed-english-v3"}, "SCHOLIA_EMBED_MODEL"},
		{"embed model as default chat model", map[string]string{"SCHOLIA_DEFAULT_MODEL": "amazon.titan-embed-text-v2:0"}, "catalog chat model"},
		{"openai default with server bedrock", map[string]string{"SCHOLIA_BEDROCK": "on", "SCHOLIA_DEFAULT_MODEL": "gpt-4.1"}, "must be a Bedrock model"},
		{"secret arn without client in prod", map[string]string{"SCHOLIA_ENV": "prod", "SCHOLIA_SESSION_SECRET_ARN": "arn:aws:secretsmanager:us-east-1:1:secret:s"}, "SCHOLIA_COGNITO_CLIENT_ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(env(tt.env))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got error %v, want one mentioning %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadLocalSecretWithoutCognito(t *testing.T) {
	cfg, err := Load(env(map[string]string{"SCHOLIA_SESSION_SECRET": "local-dev-session-secret-min-16"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SessionSecret != "local-dev-session-secret-min-16" || cfg.CognitoClientID != "" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadReportsAllErrors(t *testing.T) {
	_, err := Load(env(map[string]string{"SCHOLIA_ENV": "x", "PORT": "y"}))
	if err == nil || !strings.Contains(err.Error(), "SCHOLIA_ENV") || !strings.Contains(err.Error(), "PORT") {
		t.Fatalf("expected both errors, got %v", err)
	}
}

func TestServerBedrockSwitch(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"off by default locally", nil, false},
		{"on by default in prod", map[string]string{"SCHOLIA_ENV": "prod"}, true},
		{"off in prod on request", map[string]string{"SCHOLIA_ENV": "prod", "SCHOLIA_BEDROCK": "off"}, false},
		{"on locally without floci", map[string]string{"SCHOLIA_BEDROCK": " ON "}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(env(tt.env))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.ServerBedrock != tt.want {
				t.Fatalf("server bedrock = %v", cfg.ServerBedrock)
			}
		})
	}
	cfg, err := Load(env(map[string]string{"SCHOLIA_DEFAULT_MODEL": "gpt-4.1-mini", "SCHOLIA_MAX_OUTPUT_TOKENS": "512", "TAVILY_API_KEY": " tvly-x "}))
	if err != nil || cfg.DefaultModel != "gpt-4.1-mini" || cfg.MaxOutputTokens != 512 || cfg.TavilyAPIKey != "tvly-x" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

func TestLoadHardeningOverrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"SCHOLIA_GUESTS":               "off",
		"SCHOLIA_GUEST_TTL_HOURS":      "24",
		"SCHOLIA_GUEST_DAILY_MESSAGES": "3",
		"SCHOLIA_KILL_SWITCH_PARAM":    " /scholia/prod/kill-switch ",
		"SCHOLIA_GUARDRAIL_ID":         "gr-abc",
		"SCHOLIA_GUARDRAIL_VERSION":    "2",
		"SCHOLIA_TAVILY_SECRET_ARN":    "arn:aws:secretsmanager:us-east-1:111122223333:secret:tavily",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Guests || cfg.GuestTTLHours != 24 || cfg.GuestDailyMessages != 3 || cfg.KillSwitchParam != "/scholia/prod/kill-switch" ||
		cfg.GuardrailID != "gr-abc" || cfg.GuardrailVersion != "2" || cfg.TavilySecretARN == "" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadSessionSecretARN(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"SCHOLIA_ENV":                "prod",
		"SCHOLIA_COGNITO_CLIENT_ID":  "client",
		"SCHOLIA_SESSION_SECRET_ARN": "arn:aws:secretsmanager:us-east-1:111122223333:secret:session",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SessionSecret != "" || cfg.SessionSecretARN == "" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadRejectsLocalInLambda(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{"local off lambda", nil, false},
		{"unset env in lambda", map[string]string{"AWS_LAMBDA_RUNTIME_API": "127.0.0.1:9001"}, true},
		{"prod in lambda", map[string]string{"AWS_LAMBDA_RUNTIME_API": "127.0.0.1:9001", "SCHOLIA_ENV": "prod"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(env(tt.env))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRequireSignIn(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"local open", Config{Env: EnvLocal}, false},
		{"prod open", Config{Env: EnvProd}, true},
		{"prod cognito only", Config{Env: EnvProd, CognitoClientID: "c"}, true},
		{"prod secret only", Config{Env: EnvProd, SessionSecretARN: "arn"}, true},
		{"prod secret arn", Config{Env: EnvProd, CognitoClientID: "c", SessionSecretARN: "arn"}, false},
		{"prod secret value", Config{Env: EnvProd, CognitoClientID: "c", SessionSecret: strings.Repeat("s", MinSessionSecret)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.RequireSignIn(); (err != nil) != tt.wantErr {
				t.Fatalf("err %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
