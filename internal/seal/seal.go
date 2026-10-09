// Package seal encrypts a user's provider key with KMS.
// The encryption context is the user id, so a ciphertext sealed for one user
// does not open for another. Plaintext is not written to errors or logs.
package seal

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
)

const contextKey = "user_id"

// Box seals and opens a provider key for one user id.
type Box interface {
	Seal(ctx context.Context, userID string, plaintext []byte) ([]byte, error)
	Open(ctx context.Context, userID string, ciphertext []byte) ([]byte, error)
}

type kmsAPI interface {
	Encrypt(context.Context, *kms.EncryptInput, ...func(*kms.Options)) (*kms.EncryptOutput, error)
	Decrypt(context.Context, *kms.DecryptInput, ...func(*kms.Options)) (*kms.DecryptOutput, error)
}

// KMS uses one customer key. keyID is an alias or a key ARN, such as alias/scholia-prod.
type KMS struct {
	api   kmsAPI
	keyID string
}

// OpenKMS builds a client that always calls Amazon KMS.
// A process-wide AWS_ENDPOINT_URL points DynamoDB and S3 at floci.
// Floci does not emulate these calls, so this client ignores that endpoint.
func OpenKMS(cfg aws.Config, keyID string) *KMS {
	endpoint := "https://kms." + cfg.Region + ".amazonaws.com"
	return &KMS{
		api: kms.NewFromConfig(cfg, func(o *kms.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		}),
		keyID: keyID,
	}
}

// Seal encrypts plaintext for userID. The context is required on every call.
func (b *KMS) Seal(ctx context.Context, userID string, plaintext []byte) ([]byte, error) {
	if strings.TrimSpace(userID) == "" || len(plaintext) == 0 {
		return nil, errors.New("could not seal key")
	}
	out, err := b.api.Encrypt(ctx, &kms.EncryptInput{
		KeyId:             aws.String(b.keyID),
		Plaintext:         plaintext,
		EncryptionContext: map[string]string{contextKey: userID},
	})
	if err != nil || len(out.CiphertextBlob) == 0 {
		return nil, errors.New("could not seal key")
	}
	return out.CiphertextBlob, nil
}

// Open decrypts ciphertext for userID. A different user id fails.
func (b *KMS) Open(ctx context.Context, userID string, ciphertext []byte) ([]byte, error) {
	if strings.TrimSpace(userID) == "" || len(ciphertext) == 0 {
		return nil, errors.New("could not open key")
	}
	out, err := b.api.Decrypt(ctx, &kms.DecryptInput{
		KeyId:             aws.String(b.keyID),
		CiphertextBlob:    ciphertext,
		EncryptionContext: map[string]string{contextKey: userID},
	})
	if err != nil || len(out.Plaintext) == 0 {
		return nil, errors.New("could not open key")
	}
	return out.Plaintext, nil
}
