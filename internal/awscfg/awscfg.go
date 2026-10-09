// Package awscfg loads an AWS SDK config that targets floci when an endpoint is set.
package awscfg

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

// Load returns SDK config for region. A non-empty endpoint targets floci and
// uses the emulator's dummy credentials. An empty endpoint uses the default
// credential chain, which is the Lambda role in AWS. Prod config rejects
// AWS_ENDPOINT_URL, so a set endpoint is never a production call.
func Load(ctx context.Context, region, endpoint string) (aws.Config, error) {
	if region == "" {
		return aws.Config{}, errors.New("aws region is required")
	}
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
	}
	if endpoint != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			// floci does not check this pair. It is not a secret.
			credentials.NewStaticCredentialsProvider("test", "test", ""),
		))
	}
	return awsconfig.LoadDefaultConfig(ctx, opts...)
}
