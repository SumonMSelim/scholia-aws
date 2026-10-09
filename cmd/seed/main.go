// Command seed downloads the public demo course and queues it for ingestion.
// It needs the same AWS settings as the worker: TABLE_NAME, UPLOADS_BUCKET,
// AWS_REGION, and AWS_ENDPOINT_URL for floci. Running it again only queues
// files that are missing or failed.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/sumonmselim/scholia-aws/internal/awscfg"
	"github.com/sumonmselim/scholia-aws/internal/config"
	"github.com/sumonmselim/scholia-aws/internal/demo"
	"github.com/sumonmselim/scholia-aws/internal/ingest"
	"github.com/sumonmselim/scholia-aws/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run() error {
	dir := flag.String("dir", "data/demo", "directory the course files are downloaded to")
	fetchOnly := flag.Bool("fetch-only", false, "download the files and stop")
	flag.Parse()

	ctx := context.Background()
	f := demo.Fetcher{
		Client:  &http.Client{Timeout: 2 * time.Minute},
		OCW:     demo.OCWOrigin,
		BookURL: demo.BookURL,
		Dir:     *dir,
	}
	files, err := f.Fetch(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("downloaded %d files to %s\n", len(files), *dir)
	if *fetchOnly {
		return nil
	}

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	if cfg.TableName == "" || cfg.UploadsBucket == "" {
		return fmt.Errorf("TABLE_NAME and UPLOADS_BUCKET are required")
	}
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
	res, err := demo.Seed(ctx, repo, ingest.OpenBucket(awsCfg, cfg.AWSEndpoint), cfg.UploadsBucket, files)
	if err != nil {
		return err
	}
	fmt.Printf("course %s: %d files queued, %d already there\n", demo.CourseID, res.Queued, res.Skipped)
	return nil
}
