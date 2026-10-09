// Package killswitch reads the operator's pause flags from SSM Parameter Store.
//
// The parameter holds JSON such as {"server_models":false}. A missing field is
// true. Reads are cached for a minute so a request never waits on SSM, and a
// failed read keeps the last good value. Before the first good read, a switch
// that fails closed pauses server-paid models: in production an unreadable
// switch must not leave spend uncapped.
package killswitch

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// ErrPaused means the operator paused the feature. Handlers answer 503.
var ErrPaused = errors.New("the demo is paused")

// DefaultTTL is how long one read is trusted.
const DefaultTTL = time.Minute

// State is the current set of pause flags. True means enabled.
type State struct {
	ServerModels bool
	Guests       bool
	Uploads      bool
}

// Open is every flag on.
var Open = State{ServerModels: true, Guests: true, Uploads: true}

// Parameters reads one SSM parameter's value.
type Parameters interface {
	GetParameter(ctx context.Context, name string) (string, error)
}

// Switch caches the parameter. A nil Switch, or one with no Name, is always Open.
type Switch struct {
	Name   string
	Params Parameters
	// FailClosed pauses server models until the first good read.
	FailClosed bool
	TTL        time.Duration
	Now        func() time.Time

	mu      sync.Mutex
	state   State
	good    bool
	checked time.Time
}

// State returns the flags, reading SSM when the cached value is older than TTL.
func (s *Switch) State(ctx context.Context) State {
	if s == nil || s.Name == "" || s.Params == nil {
		return Open
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	ttl := s.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.checked.IsZero() && now().Sub(s.checked) < ttl {
		return s.current()
	}
	// A failed read is retried after TTL too, so SSM is not hammered while it is down.
	s.checked = now()
	raw, err := s.Params.GetParameter(ctx, s.Name)
	if err == nil {
		if state, perr := Parse(raw); perr == nil {
			s.state, s.good = state, true
		}
	}
	return s.current()
}

func (s *Switch) current() State {
	if s.good {
		return s.state
	}
	state := Open
	if s.FailClosed {
		state.ServerModels = false
	}
	return state
}

// Parse reads the parameter JSON. Unknown fields are ignored and missing ones are true.
func Parse(raw string) (State, error) {
	var flags struct {
		ServerModels *bool `json:"server_models"`
		Guests       *bool `json:"guests"`
		Uploads      *bool `json:"uploads"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &flags); err != nil {
		return State{}, err
	}
	on := func(v *bool) bool { return v == nil || *v }
	return State{ServerModels: on(flags.ServerModels), Guests: on(flags.Guests), Uploads: on(flags.Uploads)}, nil
}

// SSM reads parameters with the AWS SDK.
type SSM struct {
	Client *ssm.Client
}

// OpenSSM builds an SSM reader. The switch is operator config, so there is no floci endpoint.
func OpenSSM(cfg aws.Config) *SSM {
	return &SSM{Client: ssm.NewFromConfig(cfg)}
}

// GetParameter returns the parameter's value.
func (s *SSM) GetParameter(ctx context.Context, name string) (string, error) {
	out, err := s.Client.GetParameter(ctx, &ssm.GetParameterInput{Name: &name})
	if err != nil {
		return "", err
	}
	if out.Parameter == nil || out.Parameter.Value == nil {
		return "", errors.New("killswitch: parameter has no value")
	}
	return *out.Parameter.Value, nil
}
