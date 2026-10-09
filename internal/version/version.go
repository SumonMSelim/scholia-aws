// Package version exposes build metadata injected with -ldflags.
package version

// Set at build time: -ldflags "-X github.com/sumonmselim/scholia-aws/internal/version.Version=..."
var (
	Version = "dev"
	Commit  = "unknown"
)
