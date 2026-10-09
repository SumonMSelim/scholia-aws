package vector

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/sumonmselim/scholia-aws/internal/awscfg"
	"github.com/sumonmselim/scholia-aws/internal/locator"
)

func TestQueryAgainstFloci(t *testing.T) {
	ctx := context.Background()
	endpoint := startFloci(t)
	cfg, err := awscfg.Load(ctx, "us-east-1", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := Open(cfg, endpoint, "scholia-vectors")
	if err != nil {
		t.Fatal(err)
	}
	if err := bucket.EnsureBucket(ctx); err != nil {
		t.Fatal(err)
	}

	page := locator.Locator{Kind: locator.KindPage, Page: 3, BBox: &locator.BBox{X0: 1, Y0: 2, X1: 3, Y1: 4}}
	near := Vector{
		CourseID: "c1", ChunkID: "child-near", ParentID: "parent-1",
		Locators: []locator.Locator{page}, Values: []float32{1, 0, 0, 0},
	}
	far := Vector{CourseID: "c1", ChunkID: "child-far", ParentID: "parent-1", Values: []float32{0, 1, 0, 0}}
	otherCourse := Vector{CourseID: "c2", ChunkID: "child-other-course", ParentID: "parent-2", Values: []float32{1, 0, 0, 0}}
	if err := bucket.Put(ctx, "model.alpha", []Vector{near, far, otherCourse}); err != nil {
		t.Fatal(err)
	}
	otherModel := Vector{CourseID: "c1", ChunkID: "child-other-model", ParentID: "parent-9", Values: []float32{1, 0, 0, 0}}
	if err := bucket.Put(ctx, "model.beta", []Vector{otherModel}); err != nil {
		t.Fatal(err)
	}

	hits, err := bucket.Query(ctx, "model.alpha", []float32{1, 0, 0, 0}, "c1", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].ChunkID != "child-near" || hits[0].ParentID != "parent-1" || hits[0].CourseID != "c1" {
		t.Fatalf("hits %+v", hits)
	}
	if len(hits[0].Locators) != 1 || hits[0].Locators[0].Page != 3 || hits[0].Locators[0].BBox == nil || hits[0].Locators[0].BBox.X0 != 1 {
		t.Fatalf("locator %+v", hits[0].Locators)
	}
	for _, hit := range hits {
		if hit.ChunkID == "child-other-model" || hit.ChunkID == "child-other-course" {
			t.Fatalf("query crossed a boundary: %+v", hits)
		}
	}

	beta, err := bucket.Query(ctx, "model.beta", []float32{1, 0, 0, 0}, "c1", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(beta) != 1 || beta[0].ChunkID != "child-other-model" {
		t.Fatalf("other index %+v", beta)
	}
	for _, hit := range beta {
		if hit.ChunkID == "child-near" || hit.ChunkID == "child-far" {
			t.Fatalf("other model read the first index: %+v", beta)
		}
	}
}

func startFloci(t *testing.T) string {
	t.Helper()
	// Without a Docker daemon testcontainers panics. Skipping keeps the unit tests runnable.
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()
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
