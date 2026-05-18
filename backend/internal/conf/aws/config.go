// Package awscfg builds an aws-sdk-go-v2 config with optional LocalStack
// endpoint, credentials overrides, and retry tuning.
//
// The package name doesn't match its directory (conf/aws/) on purpose:
// "aws" would shadow the ubiquitous aws-sdk-go-v2/aws package at every
// call site. awscfg sidesteps that without forcing an alias.
package awscfg

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

type Options struct {
	Region       string
	EndpointURL  string
	AccessKey    string
	SecretKey    string
	SessionToken string

	// MaxRetryAttempts overrides smithy's default (3) when > 0.
	MaxRetryAttempts int
}

// Load returns an aws.Config honoring the options. When EndpointURL is set
// every service client routes through it (LocalStack convention).
func Load(ctx context.Context, o Options) (aws.Config, error) {
	loaders := []func(*config.LoadOptions) error{
		config.WithRegion(o.Region),
	}
	if o.AccessKey != "" || o.SecretKey != "" {
		loaders = append(loaders, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(o.AccessKey, o.SecretKey, o.SessionToken)))
	}
	if o.MaxRetryAttempts > 0 {
		attempts := o.MaxRetryAttempts
		loaders = append(loaders, config.WithRetryer(func() aws.Retryer {
			return retry.NewStandard(func(s *retry.StandardOptions) {
				s.MaxAttempts = attempts
			})
		}))
	}
	cfg, err := config.LoadDefaultConfig(ctx, loaders...)
	if err != nil {
		return aws.Config{}, err
	}
	if o.EndpointURL != "" {
		cfg.BaseEndpoint = aws.String(o.EndpointURL)
	}
	return cfg, nil
}
