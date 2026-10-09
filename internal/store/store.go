// Package store is the DynamoDB repository for courses, sources, chunks, chats, and user settings.
//
// One table, keys pk and sk, named by TABLE_NAME. No second table, and no GSI:
// every access pattern below is a GetItem or a Query on the primary key.
//
//	course  pk = COURSE#<courseID>  sk = COURSE#<courseID>
//	list    pk = COURSES            sk = COURSE#<courseID>
//	source  pk = COURSE#<courseID>  sk = SOURCE#<sourceID>
//	chunk   pk = SOURCE#<sourceID>  sk = CHUNK#<chunkID>
//	job     pk = JOB#<jobName>      sk = JOB#<jobName>
//	key     pk = USER#<userID>       sk = KEY#<provider>
//	prefs   pk = USER#<userID>       sk = SETTINGS
//	chat    pk = USER#<userID>#CHATS sk = CHAT#<chatID>
//	message pk = CHAT#<chatID>       sk = MSG#<unix nanos, 20 digits>#<messageID>
//	attach  pk = CHAT#<chatID>       sk = ATT#<attachmentID>
//	usage   pk = USAGE#<subject>     sk = DAY#<yyyy-mm-dd>
//
// Chats sit under their owner, so a chat can only be read with the owner's id.
// Message keys sort by time, so a Query returns a chat in order. Attachment rows
// share the chat's partition under their own prefix, so a message query skips them.
//
// Usage rows count one subject's metered calls for a UTC day and carry a TTL.
// Guest records carry expires_at too, so DynamoDB TTL removes them.
//
// The list row exists so courses can be listed with a Query. There is still no GSI.
//
// GetCourse, ListCourses, ListSources, GetSource, ListChunks, and GetChunk use those keys.
// GetChunk needs the source id; chunk ids are unique within a source, and the id
// is stored on the item rather than parsed back out of the key.
// Delete removes that one item. It does not cascade, so deleting a course leaves
// its sources and chunks in place until a later issue defines cleanup.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/locator"
)

// ErrNotFound is returned when a get finds no item.
var ErrNotFound = errors.New("not found")

// Repository reads and writes the single application table.
type Repository struct {
	db       *dynamodb.Client
	table    string
	pageSize int32
}

// New returns a repository for table. client and table are required.
func New(client *dynamodb.Client, table string) (*Repository, error) {
	if client == nil {
		return nil, errors.New("dynamodb client is required")
	}
	if table == "" {
		return nil, errors.New("table name is required")
	}
	return &Repository{db: client, table: table}, nil
}

type courseItem struct {
	PK        string `dynamodbav:"pk"`
	SK        string `dynamodbav:"sk"`
	Entity    string `dynamodbav:"entity"`
	ID        string `dynamodbav:"id"`
	Title     string `dynamodbav:"title"`
	OwnerID   string `dynamodbav:"owner_id,omitempty"`
	Public    bool   `dynamodbav:"public,omitempty"`
	ExpiresAt int64  `dynamodbav:"expires_at,omitempty"`
}

type sourceItem struct {
	PK            string `dynamodbav:"pk"`
	SK            string `dynamodbav:"sk"`
	Entity        string `dynamodbav:"entity"`
	ID            string `dynamodbav:"id"`
	CourseID      string `dynamodbav:"course_id"`
	Name          string `dynamodbav:"name"`
	ContentType   string `dynamodbav:"content_type,omitempty"`
	Status        string `dynamodbav:"status"`
	FailureReason string `dynamodbav:"failure_reason,omitempty"`
	YouTubeID     string `dynamodbav:"youtube_id,omitempty"`
	ExpiresAt     int64  `dynamodbav:"expires_at,omitempty"`
}

type chunkItem struct {
	PK         string `dynamodbav:"pk"`
	SK         string `dynamodbav:"sk"`
	Entity     string `dynamodbav:"entity"`
	ID         string `dynamodbav:"id"`
	CourseID   string `dynamodbav:"course_id"`
	SourceID   string `dynamodbav:"source_id"`
	ParentID   string `dynamodbav:"parent_id,omitempty"`
	Text       string `dynamodbav:"text,omitempty"`
	Breadcrumb string `dynamodbav:"breadcrumb,omitempty"`
	// Locators is the JSON form from package locator, so zero offsets survive the round trip.
	Locators  string `dynamodbav:"locators"`
	ExpiresAt int64  `dynamodbav:"expires_at,omitempty"`
}

func courseKey(id string) (string, string) {
	return "COURSE#" + id, "COURSE#" + id
}

func courseListKey(id string) (string, string) {
	return "COURSES", "COURSE#" + id
}

func sourceKey(courseID, sourceID string) (string, string) {
	return "COURSE#" + courseID, "SOURCE#" + sourceID
}

func chunkKey(sourceID, chunkID string) (string, string) {
	return "SOURCE#" + sourceID, "CHUNK#" + chunkID
}

// PutCourse creates or replaces a course and its list row in one transaction.
func (r *Repository) PutCourse(ctx context.Context, course domain.Course) error {
	if err := course.Validate(); err != nil {
		return err
	}
	pk, sk := courseKey(course.ID)
	listPK, listSK := courseListKey(course.ID)
	item, err := attributevalue.MarshalMap(newCourseItem(pk, sk, course))
	if err != nil {
		return err
	}
	listed, err := attributevalue.MarshalMap(newCourseItem(listPK, listSK, course))
	if err != nil {
		return err
	}
	_, err = r.db.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{Put: &types.Put{TableName: &r.table, Item: item}},
			{Put: &types.Put{TableName: &r.table, Item: listed}},
		},
	})
	if err != nil {
		return fmt.Errorf("put course: %w", err)
	}
	return nil
}

// ListCourses returns every course, ordered by course id.
func (r *Repository) ListCourses(ctx context.Context) ([]domain.Course, error) {
	pk, _ := courseListKey("")
	items, err := r.query(ctx, pk, "COURSE#")
	if err != nil {
		return nil, fmt.Errorf("list courses: %w", err)
	}
	out := []domain.Course{}
	for _, raw := range items {
		var item courseItem
		if err := attributevalue.UnmarshalMap(raw, &item); err != nil {
			return nil, fmt.Errorf("list courses: %w", err)
		}
		out = append(out, item.course())
	}
	return out, nil
}

// GetCourse returns the course or ErrNotFound.
func (r *Repository) GetCourse(ctx context.Context, id string) (domain.Course, error) {
	pk, sk := courseKey(id)
	var item courseItem
	ok, err := r.get(ctx, pk, sk, &item)
	if err != nil {
		return domain.Course{}, fmt.Errorf("get course: %w", err)
	}
	if !ok {
		return domain.Course{}, fmt.Errorf("course %s: %w", id, ErrNotFound)
	}
	return item.course(), nil
}

func newCourseItem(pk, sk string, course domain.Course) courseItem {
	return courseItem{
		PK: pk, SK: sk, Entity: "course",
		ID: course.ID, Title: course.Title, OwnerID: course.OwnerID, Public: course.Public,
		ExpiresAt: course.ExpiresAt,
	}
}

func (item courseItem) course() domain.Course {
	return domain.Course{
		ID: item.ID, Title: item.Title, OwnerID: item.OwnerID, Public: item.Public,
		ExpiresAt: item.ExpiresAt,
	}
}

// DeleteCourse removes the course and its list row. Sources and chunks are left in place.
func (r *Repository) DeleteCourse(ctx context.Context, id string) error {
	pk, sk := courseKey(id)
	listPK, listSK := courseListKey(id)
	_, err := r.db.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{Delete: &types.Delete{TableName: &r.table, Key: itemKey(pk, sk)}},
			{Delete: &types.Delete{TableName: &r.table, Key: itemKey(listPK, listSK)}},
		},
	})
	if err != nil {
		return fmt.Errorf("delete course: %w", err)
	}
	return nil
}

func itemKey(pk, sk string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"pk": &types.AttributeValueMemberS{Value: pk},
		"sk": &types.AttributeValueMemberS{Value: sk},
	}
}

// PutSource creates or replaces a source.
func (r *Repository) PutSource(ctx context.Context, source domain.Source) error {
	if err := source.Validate(); err != nil {
		return err
	}
	pk, sk := sourceKey(source.CourseID, source.ID)
	return r.put(ctx, sourceItem{
		PK: pk, SK: sk, Entity: "source",
		ID: source.ID, CourseID: source.CourseID, Name: source.Name,
		ContentType: source.ContentType, Status: string(source.Status),
		FailureReason: source.FailureReason,
		YouTubeID:     source.YouTubeID, ExpiresAt: source.ExpiresAt,
	})
}

// GetSource returns the source or ErrNotFound.
func (r *Repository) GetSource(ctx context.Context, courseID, sourceID string) (domain.Source, error) {
	pk, sk := sourceKey(courseID, sourceID)
	var item sourceItem
	ok, err := r.get(ctx, pk, sk, &item)
	if err != nil {
		return domain.Source{}, fmt.Errorf("get source: %w", err)
	}
	if !ok {
		return domain.Source{}, fmt.Errorf("source %s: %w", sourceID, ErrNotFound)
	}
	return sourceFromItem(item), nil
}

// ListSources returns the sources in a course, ordered by source id.
// An unknown course yields an empty slice.
func (r *Repository) ListSources(ctx context.Context, courseID string) ([]domain.Source, error) {
	pk, _ := sourceKey(courseID, "")
	items, err := r.query(ctx, pk, "SOURCE#")
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	out := []domain.Source{}
	for _, raw := range items {
		var item sourceItem
		if err := attributevalue.UnmarshalMap(raw, &item); err != nil {
			return nil, fmt.Errorf("list sources: %w", err)
		}
		out = append(out, sourceFromItem(item))
	}
	return out, nil
}

// DeleteSource removes the source item. Chunks are left in place.
func (r *Repository) DeleteSource(ctx context.Context, courseID, sourceID string) error {
	pk, sk := sourceKey(courseID, sourceID)
	return r.delete(ctx, pk, sk)
}

// PutChunk creates or replaces a chunk.
func (r *Repository) PutChunk(ctx context.Context, chunk domain.Chunk) error {
	if err := chunk.Validate(); err != nil {
		return err
	}
	locs := chunk.Locators
	if locs == nil {
		locs = []locator.Locator{}
	}
	raw, err := json.Marshal(locs)
	if err != nil {
		return fmt.Errorf("put chunk: %w", err)
	}
	pk, sk := chunkKey(chunk.SourceID, chunk.ID)
	return r.put(ctx, chunkItem{
		PK: pk, SK: sk, Entity: "chunk",
		ID: chunk.ID, CourseID: chunk.CourseID, SourceID: chunk.SourceID,
		ParentID: chunk.ParentID, Text: chunk.Text, Breadcrumb: chunk.Breadcrumb, Locators: string(raw),
		ExpiresAt: chunk.ExpiresAt,
	})
}

// GetChunk returns the chunk or ErrNotFound. sourceID is required because the primary key is the source.
func (r *Repository) GetChunk(ctx context.Context, sourceID, chunkID string) (domain.Chunk, error) {
	pk, sk := chunkKey(sourceID, chunkID)
	var item chunkItem
	ok, err := r.get(ctx, pk, sk, &item)
	if err != nil {
		return domain.Chunk{}, fmt.Errorf("get chunk: %w", err)
	}
	if !ok {
		return domain.Chunk{}, fmt.Errorf("chunk %s: %w", chunkID, ErrNotFound)
	}
	return chunkFromItem(item)
}

// ListChunks returns the chunks in a source, ordered by chunk id.
func (r *Repository) ListChunks(ctx context.Context, sourceID string) ([]domain.Chunk, error) {
	pk, _ := chunkKey(sourceID, "")
	items, err := r.query(ctx, pk, "CHUNK#")
	if err != nil {
		return nil, fmt.Errorf("list chunks: %w", err)
	}
	out := []domain.Chunk{}
	for _, raw := range items {
		var item chunkItem
		if err := attributevalue.UnmarshalMap(raw, &item); err != nil {
			return nil, fmt.Errorf("list chunks: %w", err)
		}
		chunk, err := chunkFromItem(item)
		if err != nil {
			return nil, fmt.Errorf("list chunks: %w", err)
		}
		out = append(out, chunk)
	}
	return out, nil
}

// DeleteChunk removes the chunk item.
func (r *Repository) DeleteChunk(ctx context.Context, sourceID, chunkID string) error {
	pk, sk := chunkKey(sourceID, chunkID)
	return r.delete(ctx, pk, sk)
}

func sourceFromItem(item sourceItem) domain.Source {
	return domain.Source{
		ID: item.ID, CourseID: item.CourseID, Name: item.Name,
		ContentType: item.ContentType, Status: domain.SourceStatus(item.Status),
		FailureReason: item.FailureReason,
		YouTubeID:     item.YouTubeID, ExpiresAt: item.ExpiresAt,
	}
}

func chunkFromItem(item chunkItem) (domain.Chunk, error) {
	var locs []locator.Locator
	if item.Locators != "" {
		if err := json.Unmarshal([]byte(item.Locators), &locs); err != nil {
			return domain.Chunk{}, fmt.Errorf("chunk %s locators: %w", item.ID, err)
		}
	}
	return domain.Chunk{
		ID: item.ID, CourseID: item.CourseID, SourceID: item.SourceID,
		ParentID: item.ParentID, Text: item.Text, Breadcrumb: item.Breadcrumb, Locators: locs,
		ExpiresAt: item.ExpiresAt,
	}, nil
}

func (r *Repository) put(ctx context.Context, item any) error {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return err
	}
	_, err = r.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &r.table,
		Item:      av,
	})
	return err
}

func (r *Repository) get(ctx context.Context, pk, sk string, out any) (bool, error) {
	res, err := r.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &r.table,
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: pk},
			"sk": &types.AttributeValueMemberS{Value: sk},
		},
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return false, err
	}
	if len(res.Item) == 0 {
		return false, nil
	}
	if err := attributevalue.UnmarshalMap(res.Item, out); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repository) delete(ctx context.Context, pk, sk string) error {
	_, err := r.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: &r.table,
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: pk},
			"sk": &types.AttributeValueMemberS{Value: sk},
		},
	})
	return err
}

func (r *Repository) query(ctx context.Context, pk, prefix string) ([]map[string]types.AttributeValue, error) {
	expr, err := expression.NewBuilder().WithKeyCondition(
		expression.Key("pk").Equal(expression.Value(pk)).
			And(expression.Key("sk").BeginsWith(prefix)),
	).Build()
	if err != nil {
		return nil, err
	}
	var items []map[string]types.AttributeValue
	var start map[string]types.AttributeValue
	for {
		in := &dynamodb.QueryInput{
			TableName:                 &r.table,
			KeyConditionExpression:    expr.KeyCondition(),
			ExpressionAttributeNames:  expr.Names(),
			ExpressionAttributeValues: expr.Values(),
			ExclusiveStartKey:         start,
			ConsistentRead:            aws.Bool(true),
		}
		if r.pageSize > 0 {
			in.Limit = aws.Int32(r.pageSize)
		}
		page, err := r.db.Query(ctx, in)
		if err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if len(page.LastEvaluatedKey) == 0 {
			return items, nil
		}
		start = page.LastEvaluatedKey
	}
}
