package vector

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors/document"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors/types"

	"github.com/sumonmselim/scholia-aws/internal/embed"
	"github.com/sumonmselim/scholia-aws/internal/locator"
)

func TestIndexName(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr string
	}{
		{in: embed.DefaultModel, want: "amazon.titan-embed-text-v2-0"},
		{in: " Model.Beta ", want: "model.beta"},
		{in: "", wantErr: "required"},
		{in: "a", wantErr: "cannot name"},
		{in: strings.Repeat("a", 80), want: strings.Repeat("a", 63)},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := IndexName(tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err %v", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q err %v", got, err)
			}
		})
	}
}

func TestInitScriptCreatesDefaultIndex(t *testing.T) {
	name, err := IndexName(embed.DefaultModel)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile("../../deploy/init/init-aws.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(name)) || !bytes.Contains(body, []byte("dimension 1024")) {
		t.Fatalf("init script missing index %s", name)
	}
}

func TestOpenRejectsEmptyBucket(t *testing.T) {
	if _, err := Open(aws.Config{Region: "us-east-1"}, "", " "); err == nil {
		t.Fatal("expected error")
	}
	opened := mustOpen(t)
	if opened.api == nil {
		t.Fatal("missing client")
	}
}

func TestPutAndQueryMapMetadata(t *testing.T) {
	api := &fakeVectors{}
	bucket := &Bucket{api: api, bucket: "vectors", dims: map[string]int{}}
	loc := locator.Locator{Kind: locator.KindSlide, Slide: 2}
	vec := Vector{
		CourseID: "c1", ChunkID: "child", ParentID: "parent",
		Locators: []locator.Locator{loc}, Values: []float32{1, 0, 0, 0},
	}
	if err := bucket.Put(context.Background(), embed.DefaultModel, nil); err != nil {
		t.Fatal(err)
	}
	if err := bucket.Put(context.Background(), embed.DefaultModel, []Vector{vec}); err != nil {
		t.Fatal(err)
	}
	if err := bucket.Put(context.Background(), embed.DefaultModel, []Vector{vec}); err != nil {
		t.Fatal(err)
	}
	if api.creates != 1 {
		t.Fatalf("creates %d", api.creates)
	}
	if len(api.puts) != 2 || api.puts[0].IndexName == nil || *api.puts[0].IndexName != "amazon.titan-embed-text-v2-0" {
		t.Fatalf("puts %+v", api.puts)
	}
	if api.puts[0].Vectors[0].Key == nil || *api.puts[0].Vectors[0].Key != "child" {
		t.Fatal("key")
	}

	api.query = []types.QueryOutputVector{{
		Key:      aws.String("child"),
		Distance: aws.Float32(0.1),
		Metadata: api.puts[0].Vectors[0].Metadata,
	}}
	hits, err := bucket.Query(context.Background(), embed.DefaultModel, []float32{1, 0, 0, 0}, "c1", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ChunkID != "child" || hits[0].ParentID != "parent" || hits[0].CourseID != "c1" {
		t.Fatalf("hits %+v", hits)
	}
	if hits[0].Distance != 0.1 || len(hits[0].Locators) != 1 || hits[0].Locators[0].Slide != 2 {
		t.Fatalf("hit %+v", hits[0])
	}
	raw, err := api.lastQuery.Filter.MarshalSmithyDocument()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"course_id"`)) || !bytes.Contains(raw, []byte(`"$eq"`)) || !bytes.Contains(raw, []byte(`"c1"`)) {
		t.Fatalf("filter %s", raw)
	}
}

func TestPutRejects(t *testing.T) {
	bucket := &Bucket{api: &fakeVectors{}, bucket: "vectors", dims: map[string]int{}}
	ok := Vector{CourseID: "c1", ChunkID: "child", Values: []float32{1, 0}}
	cases := []struct {
		name    string
		modelID string
		vectors []Vector
	}{
		{name: "model", modelID: "", vectors: []Vector{ok}},
		{name: "identity", modelID: "model.beta", vectors: []Vector{{ChunkID: "child", Values: []float32{1}}}},
		{name: "nan", modelID: "model.beta", vectors: []Vector{{CourseID: "c1", ChunkID: "child", Values: []float32{float32(math.NaN())}}}},
		{name: "zero", modelID: "model.beta", vectors: []Vector{{CourseID: "c1", ChunkID: "child", Values: []float32{0, 0}}}},
		{name: "dimension", modelID: "model.beta", vectors: []Vector{ok, {CourseID: "c1", ChunkID: "other", Values: []float32{1}}}},
		{name: "locator", modelID: "model.beta", vectors: []Vector{{CourseID: "c1", ChunkID: "child", Values: []float32{1}, Locators: []locator.Locator{{Kind: locator.KindSlide}}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := bucket.Put(context.Background(), tc.modelID, tc.vectors); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestEnsureBucketAndIndexConflict(t *testing.T) {
	api := &fakeVectors{bucketErr: &types.ConflictException{}}
	bucket := &Bucket{api: api, bucket: "vectors", dims: map[string]int{}}
	if err := bucket.EnsureBucket(context.Background()); err != nil {
		t.Fatal(err)
	}
	api.bucketErr = errors.New("down")
	if err := bucket.EnsureBucket(context.Background()); err == nil {
		t.Fatal("expected error")
	}

	api.bucketErr = nil
	api.indexErr = &types.ConflictException{}
	api.indexDim = 4
	vec := Vector{CourseID: "c1", ChunkID: "child", Values: []float32{1, 0, 0, 0}}
	if err := bucket.Put(context.Background(), "model.beta", []Vector{vec}); err != nil {
		t.Fatal(err)
	}
	bucket.dims = map[string]int{}
	api.indexDim = 8
	if err := bucket.Put(context.Background(), "model.beta", []Vector{vec}); err == nil {
		t.Fatal("expected dimension mismatch")
	}
}

func TestQueryRejects(t *testing.T) {
	api := &fakeVectors{queryErr: errors.New("down")}
	bucket := &Bucket{api: api, bucket: "vectors", dims: map[string]int{}}
	if _, err := bucket.Query(context.Background(), "", []float32{1}, "c1", 1); err == nil {
		t.Fatal("expected model error")
	}
	if _, err := bucket.Query(context.Background(), "model.beta", []float32{1}, "", 1); err == nil {
		t.Fatal("expected course error")
	}
	if _, err := bucket.Query(context.Background(), "model.beta", []float32{0}, "c1", 1); err == nil {
		t.Fatal("expected zero vector error")
	}
	if _, err := bucket.Query(context.Background(), "model.beta", []float32{1}, "c1", 0); err == nil {
		t.Fatal("expected limit error")
	}
	if _, err := bucket.Query(context.Background(), "model.beta", []float32{1}, "c1", 1); err == nil {
		t.Fatal("expected query error")
	}
	api.queryErr = nil
	api.query = []types.QueryOutputVector{{Key: aws.String("child")}}
	if _, err := bucket.Query(context.Background(), "model.beta", []float32{1}, "c1", 1); err == nil {
		t.Fatal("expected missing metadata")
	}
	api.query = []types.QueryOutputVector{{
		Key: aws.String("child"),
		Metadata: document.NewLazyDocument(map[string]string{
			"course_id": "c1", "chunk_id": "other", "parent_id": "", "locator": "[]",
		}),
	}}
	if _, err := bucket.Query(context.Background(), "model.beta", []float32{1}, "c1", 1); err == nil {
		t.Fatal("expected key mismatch")
	}
}

func TestPutBatches(t *testing.T) {
	api := &fakeVectors{}
	bucket := &Bucket{api: api, bucket: "vectors", dims: map[string]int{}}
	vectors := make([]Vector, maxPutBatch+1)
	for i := range vectors {
		vectors[i] = Vector{CourseID: "c1", ChunkID: strings.Repeat("a", 3) + strings.Repeat("b", i), Values: []float32{1}}
	}
	if err := bucket.Put(context.Background(), "model.beta", vectors); err != nil {
		t.Fatal(err)
	}
	if len(api.puts) != 2 || len(api.puts[0].Vectors) != maxPutBatch || len(api.puts[1].Vectors) != 1 {
		t.Fatalf("batches %d", len(api.puts))
	}
}

type fakeVectors struct {
	bucketErr error
	indexErr  error
	indexDim  int32
	getErrs   []error // returned by GetIndex in turn before it answers from indexDim
	putErr    error
	queryErr  error
	creates   int
	puts      []s3vectors.PutVectorsInput
	query     []types.QueryOutputVector
	lastQuery *s3vectors.QueryVectorsInput
}

func (f *fakeVectors) CreateVectorBucket(context.Context, *s3vectors.CreateVectorBucketInput, ...func(*s3vectors.Options)) (*s3vectors.CreateVectorBucketOutput, error) {
	if f.bucketErr != nil {
		return nil, f.bucketErr
	}
	return &s3vectors.CreateVectorBucketOutput{}, nil
}

func (f *fakeVectors) CreateIndex(_ context.Context, in *s3vectors.CreateIndexInput, _ ...func(*s3vectors.Options)) (*s3vectors.CreateIndexOutput, error) {
	f.creates++
	if f.indexErr != nil {
		return nil, f.indexErr
	}
	if in.Dimension != nil {
		f.indexDim = *in.Dimension
	}
	return &s3vectors.CreateIndexOutput{}, nil
}

func (f *fakeVectors) GetIndex(context.Context, *s3vectors.GetIndexInput, ...func(*s3vectors.Options)) (*s3vectors.GetIndexOutput, error) {
	if len(f.getErrs) > 0 {
		err := f.getErrs[0]
		f.getErrs = f.getErrs[1:]
		if err != nil {
			return nil, err
		}
	}
	if f.indexDim == 0 {
		return nil, &types.NotFoundException{}
	}
	dim := f.indexDim
	return &s3vectors.GetIndexOutput{Index: &types.Index{Dimension: &dim}}, nil
}

func (f *fakeVectors) PutVectors(_ context.Context, in *s3vectors.PutVectorsInput, _ ...func(*s3vectors.Options)) (*s3vectors.PutVectorsOutput, error) {
	if f.putErr != nil {
		return nil, f.putErr
	}
	f.puts = append(f.puts, *in)
	return &s3vectors.PutVectorsOutput{}, nil
}

func (f *fakeVectors) QueryVectors(_ context.Context, in *s3vectors.QueryVectorsInput, _ ...func(*s3vectors.Options)) (*s3vectors.QueryVectorsOutput, error) {
	f.lastQuery = in
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return &s3vectors.QueryVectorsOutput{Vectors: f.query}, nil
}

func mustOpen(t *testing.T) *Bucket {
	t.Helper()
	withEndpoint, err := Open(aws.Config{Region: "us-east-1"}, "http://127.0.0.1:9", "vectors")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Open(aws.Config{Region: "us-east-1"}, "", "vectors")
	if err != nil {
		t.Fatal(err)
	}
	if plain.bucket != "vectors" {
		t.Fatal(plain.bucket)
	}
	return withEndpoint
}

func TestEnsureIndex(t *testing.T) {
	vec := Vector{CourseID: "c1", ChunkID: "child", Values: []float32{1, 0, 0, 0}}
	tests := []struct {
		name        string
		api         *fakeVectors
		wantErr     bool
		wantCreates int
	}{
		// Prod: Terraform made the index and the worker may not create one.
		{"existing index is not created", &fakeVectors{indexDim: 4, indexErr: &types.AccessDeniedException{}}, false, 0},
		{"missing index is created", &fakeVectors{}, false, 1},
		{"another writer creates it first", &fakeVectors{getErrs: []error{&types.NotFoundException{}}, indexDim: 4, indexErr: &types.ConflictException{}}, false, 1},
		{"read fails", &fakeVectors{getErrs: []error{errors.New("down")}, indexDim: 4}, true, 0},
		{"create fails", &fakeVectors{indexErr: &types.AccessDeniedException{}}, true, 1},
		{"existing index has another width", &fakeVectors{indexDim: 8}, true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket := &Bucket{api: tt.api, bucket: "vectors", dims: map[string]int{}}
			err := bucket.Put(context.Background(), "model.beta", []Vector{vec})
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error %v", err, tt.wantErr)
			}
			if tt.api.creates != tt.wantCreates {
				t.Fatalf("creates = %d, want %d", tt.api.creates, tt.wantCreates)
			}
		})
	}
}
