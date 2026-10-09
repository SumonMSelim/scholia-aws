package killswitch

import (
	"context"

	"github.com/sumonmselim/scholia-aws/internal/model"
	"github.com/sumonmselim/scholia-aws/internal/vision"
)

// Guard wraps a server-paid provider so every call first checks the switch.
// Answers check earlier, in the key resolver, so the reply can be a clean 503;
// Guard covers the other server-paid calls such as the study plan.
type Guard struct {
	Next   model.Provider
	Switch *Switch
}

func (g Guard) allowed(ctx context.Context) error {
	if !g.Switch.State(ctx).ServerModels {
		return ErrPaused
	}
	return nil
}

// Complete calls the wrapped provider unless server models are paused.
func (g Guard) Complete(ctx context.Context, req model.Request) (string, error) {
	if err := g.allowed(ctx); err != nil {
		return "", err
	}
	return g.Next.Complete(ctx, req)
}

// Stream calls the wrapped provider unless server models are paused.
func (g Guard) Stream(ctx context.Context, req model.Request, emit func(delta string) error) error {
	if err := g.allowed(ctx); err != nil {
		return err
	}
	return g.Next.Stream(ctx, req, emit)
}

// Observe calls the wrapped provider unless server models are paused.
func (g Guard) Observe(ctx context.Context, contentType string, image []byte) (vision.Observation, error) {
	if err := g.allowed(ctx); err != nil {
		return vision.Observation{}, err
	}
	return g.Next.Observe(ctx, contentType, image)
}

// Read calls the wrapped provider unless server models are paused.
func (g Guard) Read(ctx context.Context, contentType string, image []byte) (string, error) {
	if err := g.allowed(ctx); err != nil {
		return "", err
	}
	return g.Next.Read(ctx, contentType, image)
}
