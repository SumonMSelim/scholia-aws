// Package secret reads an API key from AWS Secrets Manager at cold start.
// The value is returned to the caller and never logged.
package secret

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// Reader returns one secret's SecretString.
type Reader interface {
	SecretString(ctx context.Context, id string) (string, error)
}

// APIKey reads id and returns the key it holds: the plain string, or the
// api_key field when the secret is JSON. An empty secret is an error.
func APIKey(ctx context.Context, r Reader, id string) (string, error) {
	raw, err := r.SecretString(ctx, id)
	if err != nil {
		return "", err
	}
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "{") {
		var body struct {
			APIKey string `json:"api_key"`
		}
		if err := json.Unmarshal([]byte(raw), &body); err != nil {
			return "", errors.New("secret: JSON secret is not valid")
		}
		raw = strings.TrimSpace(body.APIKey)
	}
	if raw == "" {
		return "", errors.New("secret: the secret is empty")
	}
	return raw, nil
}

// SecretsManager reads secrets with the AWS SDK.
type SecretsManager struct {
	Client *secretsmanager.Client
}

// OpenSecretsManager builds a reader. Secrets Manager is not on floci, so there is no endpoint.
func OpenSecretsManager(cfg aws.Config) *SecretsManager {
	return &SecretsManager{Client: secretsmanager.NewFromConfig(cfg)}
}

// SecretString returns the secret's string value.
func (s *SecretsManager) SecretString(ctx context.Context, id string) (string, error) {
	out, err := s.Client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: &id})
	if err != nil {
		return "", err
	}
	if out.SecretString == nil {
		return "", errors.New("secret: the secret has no string value")
	}
	return *out.SecretString, nil
}

// Plain reads id as a plain string secret, such as the session signing key.
// min is the shortest value accepted, so a truncated secret fails at start.
func Plain(ctx context.Context, r Reader, id string, min int) (string, error) {
	raw, err := r.SecretString(ctx, id)
	if err != nil {
		return "", err
	}
	raw = strings.TrimSpace(raw)
	if len(raw) < min || raw == "" {
		return "", errors.New("secret: the secret is empty or too short")
	}
	return raw, nil
}
