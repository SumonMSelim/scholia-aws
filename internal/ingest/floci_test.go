package ingest

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/sumonmselim/scholia-aws/internal/awscfg"
	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/store"
)

func TestUploadAgainstFloci(t *testing.T) {
	ctx := t.Context()
	endpoint := startFloci(t)
	cfg, err := awscfg.Load(ctx, "us-east-1", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.NewClient(ctx, "us-east-1", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	const table = "scholia-upload"
	const bucket = "scholia-upload"
	createUploadTable(t, db, table)
	createUploadBucket(t, cfg, endpoint, bucket)
	repo, err := store.New(db, table)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.PutCourse(ctx, domain.Course{ID: "c1", Title: "Nets"}); err != nil {
		t.Fatal(err)
	}
	objects := OpenBucket(cfg, endpoint)
	proc := &Processor{Store: repo, Objects: objects}

	readyID := putAndProcess(t, ctx, repo, objects, proc, bucket, "notes.txt", "text/plain", []byte("hello notes\n"))
	got, err := repo.GetSource(ctx, "c1", readyID)
	if err != nil || got.Status != domain.SourceReady {
		t.Fatalf("ready source %+v err %v", got, err)
	}

	failedID := putAndProcess(t, ctx, repo, objects, proc, bucket, "bad.pdf", "application/pdf", []byte{0x89, 'P', 'N', 'G'})
	got, err = repo.GetSource(ctx, "c1", failedID)
	if err != nil || got.Status != domain.SourceFailed || got.FailureReason == "" {
		t.Fatalf("failed source %+v err %v", got, err)
	}

	transcriptKey := "courses/c1/sources/" + readyID + "/derived/transcript.json"
	if err := objects.Put(ctx, bucket, transcriptKey, "application/json", "", []byte(`{"sentences":[]}`)); err != nil {
		t.Fatal(err)
	}
	prefix, err := objects.Prefix(ctx, bucket, transcriptKey, 16)
	if err != nil || string(prefix) != `{"sentences":[]}` {
		t.Fatalf("prefix %q err %v", prefix, err)
	}
	gotBody, err := objects.Get(ctx, bucket, transcriptKey)
	if err != nil || string(gotBody) != `{"sentences":[]}` {
		t.Fatalf("get %q err %v", gotBody, err)
	}
}

func putAndProcess(t *testing.T, ctx context.Context, repo *store.Repository, objects *Bucket, proc *Processor, bucket, name, contentType string, body []byte) string {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	src := domain.Source{ID: id, CourseID: "c1", Name: name, ContentType: contentType, Status: domain.SourceQueued}
	if err := repo.PutSource(ctx, src); err != nil {
		t.Fatal(err)
	}
	key := ObjectKey("c1", id, name)
	url, err := objects.PresignPutSized(ctx, bucket, key, contentType, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusNoContent {
		t.Fatalf("put %s: status %d", name, res.StatusCode)
	}
	if err := proc.HandleUpload(ctx, bucket, key); err != nil {
		t.Fatal(err)
	}
	return id
}

func startFloci(t *testing.T) string {
	t.Helper()
	// Without a Docker daemon testcontainers panics. Skipping keeps the unit tests runnable.
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := t.Context()
	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "floci/floci:2.1.0",
			ExposedPorts: []string{"4566/tcp"},
			Env:          map[string]string{"FLOCI_SERVICES_UI_ENABLED": "false"},
			WaitingFor:   wait.ForListeningPort("4566/tcp").WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("floci: %v", err)
	}
	t.Cleanup(func() {
		_ = testcontainers.TerminateContainer(ctr)
	})
	host, err := ctr.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := ctr.MappedPort(ctx, "4566/tcp")
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("http://%s:%s", host, port.Port())
}

func createUploadTable(t *testing.T, client *dynamodb.Client, table string) {
	t.Helper()
	ctx := t.Context()
	deadline := time.Now().Add(30 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		_, last = client.ListTables(ctx, &dynamodb.ListTablesInput{})
		if last == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if last != nil {
		t.Fatalf("floci dynamodb: %v", last)
	}
	_, err := client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(table),
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("pk"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("sk"), AttributeType: types.ScalarAttributeTypeS},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("pk"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("sk"), KeyType: types.KeyTypeRange},
		},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func createUploadBucket(t *testing.T, cfg aws.Config, endpoint, bucket string) {
	t.Helper()
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})
	_, err := client.CreateBucket(t.Context(), &s3.CreateBucketInput{Bucket: &bucket})
	if err != nil {
		t.Fatal(err)
	}
}
