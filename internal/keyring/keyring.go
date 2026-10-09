// Package keyring builds a chat client from the key a user stored for a provider.
// The plaintext key lives only in memory for the request and is never logged.
package keyring

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/sumonmselim/scholia-aws/internal/auth"
	"github.com/sumonmselim/scholia-aws/internal/killswitch"
	"github.com/sumonmselim/scholia-aws/internal/model"
	"github.com/sumonmselim/scholia-aws/internal/seal"
	"github.com/sumonmselim/scholia-aws/internal/store"
)

// Keys reads one user's sealed key for one provider. It returns store.ErrNotFound when none is stored.
type Keys interface {
	GetProviderKey(ctx context.Context, userID, provider string) ([]byte, error)
}

// Resolver picks the provider of a model and opens the caller's key for it.
// A caller's own key always wins. Without one, Server, the API's own Bedrock
// client, answers: with the requested model when it is a Bedrock model, and
// with Fallback otherwise. See answer.Bound for the whole precedence.
type Resolver struct {
	Keys Keys
	Box  seal.Box
	// Server calls Bedrock with the API's AWS credentials. Nil leaves it off.
	Server model.Provider
	// Fallback is the Bedrock model Server answers with when the requested model
	// is from a provider the caller has no key for: SCHOLIA_DEFAULT_MODEL.
	Fallback string
	// ServerAllowed reports whether server-paid models are on. Nil means on.
	// When it is off a caller without a key gets killswitch.ErrPaused.
	ServerAllowed func(ctx context.Context) bool
	// AWS is the region config for Bedrock. The key replaces its credentials.
	AWS aws.Config
	// OpenAIBaseURL defaults to model.OpenAIBaseURL. Tests point it at a local server.
	OpenAIBaseURL string
}

// For returns the client and the model id it should answer with. It returns
// nil, modelID when neither a key nor the server can serve, so the service's
// own model answers; with Server set that never happens. A catalog model names
// its provider; another id has none, so no user key serves it. An anonymous
// caller has no key.
func (r *Resolver) For(ctx context.Context, userID, modelID string) (model.Provider, string, error) {
	provider, _ := model.ProviderOf(modelID)
	if provider == model.ProviderOpenAI || provider == model.ProviderBedrock {
		own, err := r.userProvider(ctx, userID, modelID, provider)
		if err != nil || own != nil {
			return own, modelID, err
		}
	}
	if r.Server == nil {
		return nil, modelID, nil
	}
	if r.ServerAllowed != nil && !r.ServerAllowed(ctx) {
		return nil, modelID, killswitch.ErrPaused
	}
	// Guests and anonymous callers are not accountable for what they spend, so
	// the server answers them with the deployment default, never a premium pick.
	if provider == model.ProviderBedrock && (r.Fallback == "" || !unaccountable(userID)) {
		return r.Server, modelID, nil
	}
	// No key for an OpenAI or unknown model: server Bedrock answers with the
	// deployment default rather than the mock.
	return r.Server, r.Fallback, nil
}

func unaccountable(userID string) bool {
	return userID == "" || auth.IsGuest(userID)
}

func (r *Resolver) userProvider(ctx context.Context, userID, modelID, provider string) (model.Provider, error) {
	if r.Keys == nil || r.Box == nil || userID == "" {
		return nil, nil
	}
	sealed, err := r.Keys.GetProviderKey(ctx, userID, provider)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s key: %w", provider, err)
	}
	plain, err := r.Box.Open(ctx, userID, sealed)
	if err != nil {
		return nil, fmt.Errorf("open %s key: %w", provider, err)
	}
	key := string(plain)
	if provider == model.ProviderOpenAI {
		base := r.OpenAIBaseURL
		if base == "" {
			base = model.OpenAIBaseURL
		}
		return model.OpenCompatible(base, modelID, key)
	}
	return model.OpenBedrockKey(r.AWS, modelID, key)
}
