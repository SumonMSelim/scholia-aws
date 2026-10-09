package killswitch

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

func TestSSMGetParameter(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    string
		wantErr bool
	}{
		{"value", http.StatusOK, `{"Parameter":{"Name":"p","Value":"{\"guests\":false}"}}`, `{"guests":false}`, false},
		{"no value", http.StatusOK, `{"Parameter":{"Name":"p"}}`, "", true},
		{"denied", http.StatusBadRequest, `{"__type":"AccessDeniedException","message":"no"}`, "", true},
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
			s := OpenSSM(cfg)
			s.Client = ssm.NewFromConfig(cfg, func(o *ssm.Options) { o.BaseEndpoint = aws.String(srv.URL) })
			got, err := s.GetParameter(t.Context(), "p")
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("GetParameter = %q, %v", got, err)
			}
		})
	}
}
