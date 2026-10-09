package seal

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awskms "github.com/aws/aws-sdk-go-v2/service/kms"
)

// contextKMS stores the encryption context with the ciphertext and refuses a mismatch.
// That is the KMS rule this package relies on.
type contextKMS struct {
	sealed map[string]item
	next   int
}

type item struct {
	userID    string
	plaintext []byte
}

func (c *contextKMS) Encrypt(_ context.Context, in *awskms.EncryptInput, _ ...func(*awskms.Options)) (*awskms.EncryptOutput, error) {
	if in.EncryptionContext["user_id"] == "" || aws.ToString(in.KeyId) != "alias/scholia-prod" {
		return nil, errors.New("missing context")
	}
	c.next++
	id := string(rune('a' + c.next))
	if c.sealed == nil {
		c.sealed = map[string]item{}
	}
	c.sealed[id] = item{userID: in.EncryptionContext["user_id"], plaintext: append([]byte(nil), in.Plaintext...)}
	return &awskms.EncryptOutput{CiphertextBlob: []byte(id)}, nil
}

func (c *contextKMS) Decrypt(_ context.Context, in *awskms.DecryptInput, _ ...func(*awskms.Options)) (*awskms.DecryptOutput, error) {
	got, ok := c.sealed[string(in.CiphertextBlob)]
	if !ok || got.userID != in.EncryptionContext["user_id"] {
		return nil, errors.New("context mismatch")
	}
	return &awskms.DecryptOutput{Plaintext: append([]byte(nil), got.plaintext...)}, nil
}

func TestOpenKMSBuildsAClient(t *testing.T) {
	if OpenKMS(aws.Config{Region: "us-east-1"}, "alias/scholia-prod") == nil {
		t.Fatal("nil client")
	}
}

func TestContextBindsTheUser(t *testing.T) {
	box := &KMS{api: &contextKMS{}, keyID: "alias/scholia-prod"}
	secret := []byte("sk-live-secret")
	ct, err := box.Seal(context.Background(), "user-a", secret)
	if err != nil {
		t.Fatal(err)
	}
	if string(ct) == string(secret) {
		t.Fatal("ciphertext is the raw key")
	}
	if _, err := box.Open(context.Background(), "user-b", ct); err == nil || stringsContains(err.Error(), "sk-live") {
		t.Fatalf("user b opened the key or the error leaked it: %v", err)
	}
	got, err := box.Open(context.Background(), "user-a", ct)
	if err != nil || string(got) != string(secret) {
		t.Fatalf("user a: %q %v", got, err)
	}
	if _, err := box.Seal(context.Background(), " ", secret); err == nil {
		t.Fatal("empty user was sealed")
	}
}

func stringsContains(msg, part string) bool {
	return len(msg) >= len(part) && (part == "" || index(msg, part) >= 0)
}

func index(msg, part string) int {
	for i := 0; i+len(part) <= len(msg); i++ {
		if msg[i:i+len(part)] == part {
			return i
		}
	}
	return -1
}
