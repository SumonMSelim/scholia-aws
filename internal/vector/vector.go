// Package vector stores child-chunk embeddings in S3 Vectors.
// The index name is derived from the embedding model id, so a query reads only
// the index written by that same model.
package vector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors/document"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors/types"

	"github.com/sumonmselim/scholia-aws/internal/locator"
)

const (
	maxDimension = 4096
	maxPutBatch  = 500
	maxQuery     = 100
	maxKeyBytes  = 1024
)

// Vector is one child chunk and the embedding that should be searchable for it.
// Locators are the positions the chunk covers. ParentID is empty only when the chunk is itself a parent.
type Vector struct {
	CourseID string
	ChunkID  string
	ParentID string
	Locators []locator.Locator
	Values   []float32
}

// Hit is one neighbour returned by a query, nearest first.
type Hit struct {
	ChunkID  string
	CourseID string
	ParentID string
	Locators []locator.Locator
	Distance float32
}

// Store writes and queries vectors for one embedding model at a time.
type Store interface {
	Put(ctx context.Context, modelID string, vectors []Vector) error
	Query(ctx context.Context, modelID string, query []float32, courseID string, limit int) ([]Hit, error)
}

type vectorsAPI interface {
	CreateVectorBucket(ctx context.Context, params *s3vectors.CreateVectorBucketInput, optFns ...func(*s3vectors.Options)) (*s3vectors.CreateVectorBucketOutput, error)
	CreateIndex(ctx context.Context, params *s3vectors.CreateIndexInput, optFns ...func(*s3vectors.Options)) (*s3vectors.CreateIndexOutput, error)
	GetIndex(ctx context.Context, params *s3vectors.GetIndexInput, optFns ...func(*s3vectors.Options)) (*s3vectors.GetIndexOutput, error)
	PutVectors(ctx context.Context, params *s3vectors.PutVectorsInput, optFns ...func(*s3vectors.Options)) (*s3vectors.PutVectorsOutput, error)
	QueryVectors(ctx context.Context, params *s3vectors.QueryVectorsInput, optFns ...func(*s3vectors.Options)) (*s3vectors.QueryVectorsOutput, error)
}

// Bucket is the S3 Vectors client for one vector bucket.
// endpoint is AWS_ENDPOINT_URL: empty in AWS, floci locally.
type Bucket struct {
	api    vectorsAPI
	bucket string
	dims   map[string]int
	mu     sync.Mutex
}

// Open builds a client. A non-empty endpoint overrides the service URL the same way the other AWS clients do.
func Open(cfg aws.Config, endpoint, bucket string) (*Bucket, error) {
	if strings.TrimSpace(bucket) == "" {
		return nil, errors.New("vector bucket is required")
	}
	api := s3vectors.NewFromConfig(cfg, func(o *s3vectors.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})
	return &Bucket{api: api, bucket: bucket, dims: map[string]int{}}, nil
}

// EnsureBucket creates the vector bucket. An existing bucket is success, so local init and tests can both call it.
func (b *Bucket) EnsureBucket(ctx context.Context) error {
	_, err := b.api.CreateVectorBucket(ctx, &s3vectors.CreateVectorBucketInput{
		VectorBucketName: &b.bucket,
	})
	var conflict *types.ConflictException
	if errors.As(err, &conflict) {
		return nil
	}
	return err
}

// IndexName is the vector index for modelID.
// S3 index names are 3 to 63 characters of lowercase letters, digits, hyphens, and dots.
// Characters outside that alphabet become hyphens, so the name includes the model id
// without being a lossless encoding of it.
func IndexName(modelID string) (string, error) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return "", errors.New("embedding model id is required")
	}
	var raw strings.Builder
	for _, r := range strings.ToLower(modelID) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			raw.WriteRune(r)
			continue
		}
		raw.WriteByte('-')
	}
	name := strings.Trim(raw.String(), "-.")
	if len(name) > 63 {
		name = strings.TrimRight(name[:63], "-.")
	}
	if len(name) < 3 {
		return "", fmt.Errorf("embedding model id %q cannot name an index", modelID)
	}
	return name, nil
}

// Put writes vectors into the index for modelID, creating that index on first use.
// Every vector in the call must share a dimension. Cosine distance rejects an all-zero vector.
func (b *Bucket) Put(ctx context.Context, modelID string, vectors []Vector) error {
	if len(vectors) == 0 {
		return nil
	}
	name, err := IndexName(modelID)
	if err != nil {
		return err
	}
	dim := len(vectors[0].Values)
	for i, v := range vectors {
		if err := validateVector(v); err != nil {
			return fmt.Errorf("vector %d: %w", i, err)
		}
		if len(v.Values) != dim {
			return errors.New("vectors in one write must share a dimension")
		}
	}
	if err := b.ensureIndex(ctx, name, dim); err != nil {
		return err
	}
	for start := 0; start < len(vectors); start += maxPutBatch {
		end := start + maxPutBatch
		if end > len(vectors) {
			end = len(vectors)
		}
		batch := make([]types.PutInputVector, 0, end-start)
		for _, v := range vectors[start:end] {
			item, err := putVector(v)
			if err != nil {
				return err
			}
			batch = append(batch, item)
		}
		if _, err := b.api.PutVectors(ctx, &s3vectors.PutVectorsInput{
			VectorBucketName: &b.bucket,
			IndexName:        &name,
			Vectors:          batch,
		}); err != nil {
			return err
		}
	}
	return nil
}

// Query returns the nearest child chunks in courseID from the index written by modelID.
// A different model id addresses a different index and cannot return these vectors.
func (b *Bucket) Query(ctx context.Context, modelID string, query []float32, courseID string, limit int) ([]Hit, error) {
	name, err := IndexName(modelID)
	if err != nil {
		return nil, err
	}
	if courseID == "" {
		return nil, errors.New("course id is required")
	}
	if err := finiteValues(query); err != nil {
		return nil, err
	}
	if limit < 1 || limit > maxQuery {
		return nil, fmt.Errorf("query limit must be from 1 to %d", maxQuery)
	}
	topK := int32(limit) // limit is bounded by maxQuery, which fits in int32
	out, err := b.api.QueryVectors(ctx, &s3vectors.QueryVectorsInput{
		VectorBucketName: &b.bucket,
		IndexName:        &name,
		QueryVector:      &types.VectorDataMemberFloat32{Value: query},
		TopK:             &topK,
		ReturnDistance:   true,
		ReturnMetadata:   true,
		Filter: document.NewLazyDocument(map[string]any{
			"course_id": map[string]string{"$eq": courseID},
		}),
	})
	if err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(out.Vectors))
	for _, v := range out.Vectors {
		hit, err := hitFrom(v)
		if err != nil {
			return nil, err
		}
		hits = append(hits, hit)
	}
	return hits, nil
}

// ensureIndex checks the index before creating it. In AWS, Terraform creates the
// index and the worker may only read and write it, so CreateIndex is called only
// when the index is missing, as it is on a fresh local stack.
func (b *Bucket) ensureIndex(ctx context.Context, name string, dim int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if got, ok := b.dims[name]; ok {
		if got != dim {
			return fmt.Errorf("index %s has dimension %d, vector has %d", name, got, dim)
		}
		return nil
	}
	if dim < 1 || dim > math.MaxInt32 {
		return fmt.Errorf("dimension %d is out of range", dim)
	}
	have, err := b.indexDimension(ctx, name)
	var missing *types.NotFoundException
	if errors.As(err, &missing) {
		have, err = b.createIndex(ctx, name, int32(dim))
	}
	if err != nil {
		return err
	}
	if have != dim {
		return fmt.Errorf("index %s has dimension %d, vector has %d", name, have, dim)
	}
	b.dims[name] = dim
	return nil
}

// createIndex creates the index and returns its dimension. A concurrent writer
// may create it first, so a conflict reads the existing index instead.
func (b *Bucket) createIndex(ctx context.Context, name string, width int32) (int, error) {
	_, err := b.api.CreateIndex(ctx, &s3vectors.CreateIndexInput{
		VectorBucketName: &b.bucket,
		IndexName:        &name,
		DataType:         types.DataTypeFloat32,
		Dimension:        &width,
		DistanceMetric:   types.DistanceMetricCosine,
		// locator is retrieved with the hit and is not a query filter.
		MetadataConfiguration: &types.MetadataConfiguration{
			NonFilterableMetadataKeys: []string{"locator"},
		},
	})
	var conflict *types.ConflictException
	if errors.As(err, &conflict) {
		return b.indexDimension(ctx, name)
	}
	if err != nil {
		return 0, err
	}
	return int(width), nil
}

func (b *Bucket) indexDimension(ctx context.Context, name string) (int, error) {
	got, err := b.api.GetIndex(ctx, &s3vectors.GetIndexInput{
		VectorBucketName: &b.bucket,
		IndexName:        &name,
	})
	if err != nil {
		return 0, err
	}
	if got.Index == nil || got.Index.Dimension == nil {
		return 0, nil
	}
	return int(*got.Index.Dimension), nil
}

func validateVector(v Vector) error {
	if v.CourseID == "" || v.ChunkID == "" {
		return errors.New("course id and chunk id are required")
	}
	if len(v.ChunkID) > maxKeyBytes {
		return errors.New("chunk id is too long")
	}
	if err := finiteValues(v.Values); err != nil {
		return err
	}
	for i, loc := range v.Locators {
		if err := loc.Validate(); err != nil {
			return fmt.Errorf("locator %d: %w", i, err)
		}
	}
	return nil
}

func finiteValues(values []float32) error {
	if len(values) < 1 || len(values) > maxDimension {
		return fmt.Errorf("dimension %d is out of range", len(values))
	}
	zero := true
	for _, x := range values {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return errors.New("values must be finite")
		}
		if x != 0 {
			zero = false
		}
	}
	if zero {
		return errors.New("values must not all be zero")
	}
	return nil
}

func putVector(v Vector) (types.PutInputVector, error) {
	locs := v.Locators
	if locs == nil {
		locs = []locator.Locator{}
	}
	raw, err := json.Marshal(locs)
	if err != nil {
		return types.PutInputVector{}, err
	}
	return types.PutInputVector{
		Key:  &v.ChunkID,
		Data: &types.VectorDataMemberFloat32{Value: v.Values},
		Metadata: document.NewLazyDocument(map[string]string{
			"course_id": v.CourseID,
			"chunk_id":  v.ChunkID,
			"parent_id": v.ParentID,
			"locator":   string(raw),
		}),
	}, nil
}

func hitFrom(v types.QueryOutputVector) (Hit, error) {
	if v.Key == nil || *v.Key == "" {
		return Hit{}, errors.New("query result missing chunk id")
	}
	if v.Metadata == nil {
		return Hit{}, errors.New("query result missing metadata")
	}
	// Marshal then decode. The SDK's outbound document cannot be unmarshaled
	// back through UnmarshalSmithyDocument, and the service returns the same JSON.
	raw, err := v.Metadata.MarshalSmithyDocument()
	if err != nil {
		return Hit{}, err
	}
	var meta struct {
		CourseID string `json:"course_id"`
		ChunkID  string `json:"chunk_id"`
		ParentID string `json:"parent_id"`
		Locator  string `json:"locator"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return Hit{}, err
	}
	if meta.ChunkID == "" || meta.CourseID == "" || meta.Locator == "" {
		return Hit{}, errors.New("vector metadata is incomplete")
	}
	if meta.ChunkID != *v.Key {
		return Hit{}, errors.New("chunk id metadata does not match the vector key")
	}
	chunkID, courseID, parentID, locRaw := meta.ChunkID, meta.CourseID, meta.ParentID, meta.Locator
	var locs []locator.Locator
	if err := json.Unmarshal([]byte(locRaw), &locs); err != nil {
		return Hit{}, fmt.Errorf("locator metadata: %w", err)
	}
	for i, loc := range locs {
		if err := loc.Validate(); err != nil {
			return Hit{}, fmt.Errorf("locator %d: %w", i, err)
		}
	}
	var distance float32
	if v.Distance != nil {
		distance = *v.Distance
	}
	return Hit{
		ChunkID: chunkID, CourseID: courseID, ParentID: parentID,
		Locators: locs, Distance: distance,
	}, nil
}
