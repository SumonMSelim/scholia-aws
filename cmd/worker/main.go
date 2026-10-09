// Command worker consumes upload notifications.
// In Lambda it is an SQS handler. Locally it polls the queue against floci.
// The processing function lives in internal/ingest.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/sumonmselim/scholia-aws/internal/awscfg"
	"github.com/sumonmselim/scholia-aws/internal/config"
	"github.com/sumonmselim/scholia-aws/internal/embed"
	"github.com/sumonmselim/scholia-aws/internal/ingest"
	"github.com/sumonmselim/scholia-aws/internal/serve"
	"github.com/sumonmselim/scholia-aws/internal/store"
	"github.com/sumonmselim/scholia-aws/internal/vector"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "worker:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	if cfg.TableName == "" || cfg.UploadsBucket == "" {
		return fmt.Errorf("TABLE_NAME and UPLOADS_BUCKET are required")
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	ctx := context.Background()
	awsCfg, err := awscfg.Load(ctx, cfg.AWSRegion, cfg.AWSEndpoint)
	if err != nil {
		return err
	}
	db, err := store.NewClient(ctx, cfg.AWSRegion, cfg.AWSEndpoint)
	if err != nil {
		return err
	}
	repo, err := store.New(db, cfg.TableName)
	if err != nil {
		return err
	}
	proc := &ingest.Processor{
		Store:         repo,
		Objects:       ingest.OpenBucket(awsCfg, cfg.AWSEndpoint),
		UploadsBucket: cfg.UploadsBucket,
		Log:           log,
	}
	if cfg.VectorBucket != "" {
		vectors, err := vector.Open(awsCfg, cfg.AWSEndpoint, cfg.VectorBucket)
		if err != nil {
			return err
		}
		// Floci has no Bedrock, so compose embeds with the deterministic fake of Titan's width,
		// the same one the API queries with.
		var embedder embed.Embedder = embed.Open(awsCfg, cfg.AWSEndpoint)
		if cfg.AWSEndpoint != "" {
			embedder = &embed.Fake{Dim: 1024}
		}
		proc.Embed, proc.Vectors, proc.EmbedModel = embedder, vectors, cfg.EmbedModel
	}

	if serve.InLambda(os.Getenv) {
		lambda.Start(func(ctx context.Context, event events.SQSEvent) error {
			for _, rec := range event.Records {
				if err := proc.HandleMessage(ctx, rec.Body); err != nil {
					return err
				}
			}
			return nil
		})
		return nil
	}

	if cfg.QueueName == "" {
		return fmt.Errorf("QUEUE_NAME is required when not running in Lambda")
	}
	client := sqs.NewFromConfig(awsCfg, func(o *sqs.Options) {
		if cfg.AWSEndpoint != "" {
			o.BaseEndpoint = aws.String(cfg.AWSEndpoint)
		}
	})
	out, err := client.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: &cfg.QueueName})
	if err != nil {
		return fmt.Errorf("queue %s: %w", cfg.QueueName, err)
	}
	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return poll(runCtx, log, client, *out.QueueUrl, proc)
}

func poll(ctx context.Context, log *slog.Logger, client *sqs.Client, queueURL string, proc *ingest.Processor) error {
	log.Info("polling uploads", slog.String("queue", queueURL))
	for {
		if ctx.Err() != nil {
			log.Info("worker stopped")
			return nil
		}
		out, err := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            &queueURL,
			MaxNumberOfMessages: 1,
			WaitTimeSeconds:     20,
		})
		if err != nil {
			if ctx.Err() != nil {
				log.Info("worker stopped")
				return nil
			}
			log.Error("receive upload message", slog.String("err", err.Error()))
			continue
		}
		for _, msg := range out.Messages {
			if msg.Body == nil || msg.ReceiptHandle == nil {
				continue
			}
			if err := proc.HandleMessage(ctx, *msg.Body); err != nil {
				log.Error("process upload", slog.String("err", err.Error()))
				continue
			}
			if _, err := client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl:      &queueURL,
				ReceiptHandle: msg.ReceiptHandle,
			}); err != nil {
				log.Error("delete upload message", slog.String("err", err.Error()))
			}
		}
	}
}
