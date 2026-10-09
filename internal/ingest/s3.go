package ingest

import (
	"bytes"
	"context"
	"io"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Bucket is the uploads bucket. A non-empty endpoint uses path-style URLs for floci.
type Bucket struct {
	client  *s3.Client
	presign *s3.PresignClient
}

// OpenBucket builds an S3 client. endpoint is the floci base URL, or empty in AWS.
func OpenBucket(cfg aws.Config, endpoint string) *Bucket {
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint == "" {
			return
		}
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
		// Default checksums break presigned PUT against floci. AWS applies them when required.
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})
	return &Bucket{client: client, presign: s3.NewPresignClient(client)}
}

// WithPublicEndpoint signs upload URLs for public instead of the client endpoint.
// Compose reaches floci as floci:4566, which a browser on the host cannot resolve.
// Empty keeps the client endpoint.
func (b *Bucket) WithPublicEndpoint(public string) *Bucket {
	if public == "" {
		return b
	}
	b.presign = s3.NewPresignClient(b.client, func(po *s3.PresignOptions) {
		po.ClientOptions = append(po.ClientOptions, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(public)
		})
	})
	return b
}

// PresignPutSized returns a URL the browser can PUT to. The URL is not logged.
// A non-empty tagging, such as guest=true, is a signed header the PUT must repeat.
// size is signed as Content-Length, so the PUT must carry exactly size bytes;
// without it a client could declare one byte and store gigabytes. A size below 1
// leaves the length unsigned.
func (b *Bucket) PresignPutSized(ctx context.Context, bucket, key, contentType, tagging string, size int64) (string, error) {
	in := &s3.PutObjectInput{
		Bucket:      &bucket,
		Key:         &key,
		ContentType: &contentType,
	}
	if size > 0 {
		in.ContentLength = &size
	}
	if tagging != "" {
		in.Tagging = &tagging
	}
	req, err := b.presign.PresignPutObject(ctx, in, s3.WithPresignExpires(15*time.Minute))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

// Stat returns the object size in bytes.
func (b *Bucket) Stat(ctx context.Context, bucket, key string) (int64, error) {
	out, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &bucket, Key: &key})
	if err != nil {
		return 0, err
	}
	if out.ContentLength == nil {
		return 0, nil
	}
	return *out.ContentLength, nil
}

// Put stores a derived object such as a transcript. The caller picks the content type.
// A non-empty tagging, such as guest=true, lets a lifecycle rule expire the object.
func (b *Bucket) Put(ctx context.Context, bucket, key, contentType, tagging string, body []byte) error {
	in := &s3.PutObjectInput{
		Bucket:      &bucket,
		Key:         &key,
		Body:        bytes.NewReader(body),
		ContentType: &contentType,
	}
	if tagging != "" {
		in.Tagging = &tagging
	}
	_, err := b.client.PutObject(ctx, in)
	return err
}

// Get reads an object that the worker will parse, such as a transcript.
// An object larger than maxImportBytes is refused instead of being buffered.
func (b *Bucket) Get(ctx context.Context, bucket, key string) ([]byte, error) {
	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &key})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	body, err := io.ReadAll(io.LimitReader(out.Body, maxImportBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxImportBytes {
		return nil, errTooLarge
	}
	return body, nil
}

// Prefix reads the first n bytes. n is small; it is the magic-byte window.
func (b *Bucket) Prefix(ctx context.Context, bucket, key string, n int) ([]byte, error) {
	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
		Range:  aws.String("bytes=0-" + strconv.Itoa(n-1)),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(io.LimitReader(out.Body, int64(n)))
}

// Delete removes one object. S3 reports success for a key that does not exist.
func (b *Bucket) Delete(ctx context.Context, bucket, key string) error {
	_, err := b.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: &key})
	return err
}
