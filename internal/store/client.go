package store

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"github.com/sumonmselim/scholia-aws/internal/awscfg"
)

// NewClient builds a DynamoDB client from awscfg.Load.
func NewClient(ctx context.Context, region, endpoint string) (*dynamodb.Client, error) {
	cfg, err := awscfg.Load(ctx, region, endpoint)
	if err != nil {
		return nil, err
	}
	return dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	}), nil
}
