package secret

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

func TestSecretsManagerSecretString(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    string
		wantErr bool
	}{
		{"string", http.StatusOK, `{"Name":"tavily","SecretString":"tvly-x"}`, "tvly-x", false},
		{"binary only", http.StatusOK, `{"Name":"tavily"}`, "", true},
		{"missing", http.StatusBadRequest, `{"__type":"ResourceNotFoundException","message":"no"}`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/x-amz-json-1.1")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			cfg := aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("a", "b", ""), RetryMaxAttempts: 1}
			s := OpenSecretsManager(cfg)
			s.Client = secretsmanager.NewFromConfig(cfg, func(o *secretsmanager.Options) { o.BaseEndpoint = aws.String(srv.URL) })
			got, err := s.SecretString(t.Context(), "arn")
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("SecretString = %q, %v", got, err)
			}
		})
	}
}
