package ingest

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

func TestPresignPutEndpoint(t *testing.T) {
	cfg := aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test", "test", "")}
	tests := []struct {
		name   string
		public string
		want   string
	}{
		{"client endpoint", "", "http://floci:4566/b/k"},
		{"public endpoint", "http://localhost:4566", "http://localhost:4566/b/k"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket := OpenBucket(cfg, "http://floci:4566").WithPublicEndpoint(tt.public)
			url, err := bucket.PresignPutSized(context.Background(), "b", "k", "text/plain", "", 0)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(url, tt.want+"?") {
				t.Fatalf("url %q does not start with %q", url, tt.want)
			}
			tagged, err := bucket.PresignPutSized(context.Background(), "b", "k", "text/plain", "guest=true", 0)
			if err != nil {
				t.Fatal(err)
			}
			// The tag is a signed header, so the browser has to send it with the PUT.
			if !strings.Contains(tagged, "x-amz-tagging") {
				t.Fatalf("tagged url %q does not sign x-amz-tagging", tagged)
			}
		})
	}
}

func TestPresignPutSizedSignsLength(t *testing.T) {
	cfg := aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test", "test", "")}
	bucket := OpenBucket(cfg, "http://floci:4566")
	tests := []struct {
		name   string
		size   int64
		signed bool
	}{
		{"declared size", 42, true},
		{"no size", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, err := bucket.PresignPutSized(context.Background(), "b", "k", "text/plain", "", tt.size)
			if err != nil {
				t.Fatal(err)
			}
			// A signed length makes S3 reject a PUT whose body is any other size.
			if got := strings.Contains(url, "content-length"); got != tt.signed {
				t.Fatalf("url %q signs content-length = %v, want %v", url, got, tt.signed)
			}
		})
	}
}

// A browser PUT sends no checksum, so a presigned URL must not demand one. The
// SDK's default "when supported" checksums would sign a CRC32 of an empty body.
func TestPresignPutAWSHasNoChecksum(t *testing.T) {
	cfg := aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test", "test", "")}
	url, err := OpenBucket(cfg, "").PresignPutSized(context.Background(), "scholia-test-uploads", "k", "text/plain", "guest=true", 42)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, "https://scholia-test-uploads.s3.us-east-1.amazonaws.com/k?") {
		t.Fatalf("url %q is not on the bucket host the CSP allows", url)
	}
	if strings.Contains(strings.ToLower(url), "checksum") {
		t.Fatalf("url %q asks for a checksum the browser will not send", url)
	}
}
