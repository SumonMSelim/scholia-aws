package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/locator"
)

func TestNewRequiresClientAndTable(t *testing.T) {
	if _, err := New(nil, "scholia"); err == nil {
		t.Fatal("nil client was accepted")
	}
	client := dynamodb.NewFromConfig(aws.Config{Region: "us-east-1"})
	if _, err := New(client, ""); err == nil {
		t.Fatal("empty table was accepted")
	}
}

func TestNewClientRequiresRegion(t *testing.T) {
	if _, err := NewClient(t.Context(), "", ""); err == nil {
		t.Fatal("empty region was accepted")
	}
}

func TestRepositoryCRUD(t *testing.T) {
	ctx := t.Context()
	endpoint := startFloci(t)
	client, err := NewClient(ctx, "us-east-1", endpoint)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	const table = "scholia-test"
	createTable(t, client, table)
	repo, err := New(client, table)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	repo.pageSize = 1

	if _, err := repo.GetCourse(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing course: got %v", err)
	}
	if listed, err := repo.ListCourses(ctx); err != nil || len(listed) != 0 {
		t.Fatalf("empty course list: %+v %v", listed, err)
	}

	course := domain.Course{ID: "c1", Title: "Networks"}
	if err := repo.PutCourse(ctx, course); err != nil {
		t.Fatalf("put course: %v", err)
	}
	course.Title = "Security of Systems and Networks"
	course.OwnerID = "user-1"
	course.Public = true
	if err := repo.PutCourse(ctx, course); err != nil {
		t.Fatalf("update course: %v", err)
	}
	gotCourse, err := repo.GetCourse(ctx, course.ID)
	if err != nil {
		t.Fatalf("get course: %v", err)
	}
	if gotCourse != course {
		t.Fatalf("course = %+v, want %+v", gotCourse, course)
	}
	if _, err := repo.GetProviderKey(ctx, "user-1", "openai"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing key: %v", err)
	}
	secret := []byte("ciphertext-not-the-key")
	if err := repo.PutProviderKey(ctx, "user-1", "openai", secret); err != nil {
		t.Fatalf("put key: %v", err)
	}
	if err := repo.PutProviderKey(ctx, "user-1", "bedrock", []byte("other")); err != nil {
		t.Fatalf("put second key: %v", err)
	}
	gotSecret, err := repo.GetProviderKey(ctx, "user-1", "openai")
	if err != nil || string(gotSecret) != string(secret) {
		t.Fatalf("key = %q %v", gotSecret, err)
	}
	if providers, err := repo.ListProviderKeys(ctx, "user-1"); err != nil || len(providers) != 2 || providers[0] != "bedrock" || providers[1] != "openai" {
		t.Fatalf("providers = %v %v", providers, err)
	}
	if err := repo.DeleteProviderKey(ctx, "user-1", "bedrock"); err != nil {
		t.Fatalf("delete key: %v", err)
	}
	if providers, err := repo.ListProviderKeys(ctx, "user-1"); err != nil || len(providers) != 1 || providers[0] != "openai" {
		t.Fatalf("providers after delete = %v %v", providers, err)
	}
	other := domain.Course{ID: "c2", Title: "Signals"}
	if err := repo.PutCourse(ctx, other); err != nil {
		t.Fatalf("put second course: %v", err)
	}
	listed, err := repo.ListCourses(ctx)
	if err != nil {
		t.Fatalf("list courses: %v", err)
	}
	if len(listed) != 2 || listed[0] != course || listed[1] != other {
		t.Fatalf("courses = %+v", listed)
	}
	if err := repo.DeleteCourse(ctx, other.ID); err != nil {
		t.Fatalf("delete second course: %v", err)
	}

	queued := domain.Source{ID: "s1", CourseID: course.ID, Name: "lecture.vtt", ContentType: "text/vtt", Status: domain.SourceQueued}
	failed := domain.Source{ID: "s2", CourseID: course.ID, Name: "notes.pdf", ContentType: "application/pdf", Status: domain.SourceFailed, FailureReason: "bad magic"}
	for _, src := range []domain.Source{queued, failed} {
		if err := repo.PutSource(ctx, src); err != nil {
			t.Fatalf("put source %s: %v", src.ID, err)
		}
	}
	sources, err := repo.ListSources(ctx, course.ID)
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	if len(sources) != 2 || sources[0].ID != "s1" || sources[1].FailureReason != "bad magic" {
		t.Fatalf("sources = %+v", sources)
	}
	if _, err := repo.GetSource(ctx, course.ID, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing source: got %v", err)
	}
	queued.Status = domain.SourceProcessing
	queued.YouTubeID = "ZA-tUyM_y7s"
	if err := repo.PutSource(ctx, queued); err != nil {
		t.Fatalf("put video id: %v", err)
	}
	gotSource, err := repo.GetSource(ctx, course.ID, queued.ID)
	if err != nil || gotSource.YouTubeID != "ZA-tUyM_y7s" {
		t.Fatalf("source video %+v err %v", gotSource, err)
	}
	empty, err := repo.ListSources(ctx, "no-such-course")
	if err != nil || len(empty) != 0 {
		t.Fatalf("unknown course sources = %+v, err %v", empty, err)
	}

	box := locator.BBox{X0: 10, Y0: 20, X1: 30, Y1: 40}
	locs := []locator.Locator{
		{Kind: locator.KindPage, Page: 2, BBox: &box},
		{Kind: locator.KindSlide, Slide: 5},
		{Kind: locator.KindTime, StartMS: 0, EndMS: 1500},
		{Kind: locator.KindText, Start: 0, End: 8},
	}
	parent := domain.Chunk{ID: "p", CourseID: course.ID, SourceID: queued.ID, Text: "intro", Breadcrumb: "Nets", Locators: locs[:1]}
	child := domain.Chunk{ID: "k", CourseID: course.ID, SourceID: queued.ID, ParentID: parent.ID, Text: "span", Locators: locs}
	for _, chunk := range []domain.Chunk{parent, child} {
		if err := repo.PutChunk(ctx, chunk); err != nil {
			t.Fatalf("put chunk %s: %v", chunk.ID, err)
		}
	}
	gotChild, err := repo.GetChunk(ctx, queued.ID, child.ID)
	if err != nil {
		t.Fatalf("get chunk: %v", err)
	}
	if gotChild.ParentID != parent.ID || !reflect.DeepEqual(gotChild.Locators, locs) {
		t.Fatalf("chunk = %+v", gotChild)
	}
	gotParent, err := repo.GetChunk(ctx, queued.ID, parent.ID)
	if err != nil || gotParent.Breadcrumb != "Nets" {
		t.Fatalf("parent = %+v err %v", gotParent, err)
	}
	chunks, err := repo.ListChunks(ctx, queued.ID)
	if err != nil {
		t.Fatalf("list chunks: %v", err)
	}
	if len(chunks) != 2 || chunks[0].ID != "k" || chunks[1].ID != "p" {
		t.Fatalf("chunks = %+v", chunks)
	}

	if err := repo.PutCourse(ctx, domain.Course{}); err == nil {
		t.Fatal("invalid course was stored")
	}
	if err := repo.DeleteChunk(ctx, queued.ID, child.ID); err != nil {
		t.Fatalf("delete chunk: %v", err)
	}
	if _, err := repo.GetChunk(ctx, queued.ID, child.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted chunk: got %v", err)
	}
	if err := repo.DeleteSource(ctx, course.ID, queued.ID); err != nil {
		t.Fatalf("delete source: %v", err)
	}
	if err := repo.DeleteCourse(ctx, course.ID); err != nil {
		t.Fatalf("delete course: %v", err)
	}
	if _, err := repo.GetCourse(ctx, course.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted course: got %v", err)
	}
	if listed, err := repo.ListCourses(ctx); err != nil || len(listed) != 0 {
		t.Fatalf("course list after delete: %+v %v", listed, err)
	}
	left, err := repo.ListChunks(ctx, queued.ID)
	if err != nil || len(left) != 1 || left[0].ID != parent.ID {
		t.Fatalf("chunks after source delete = %+v, err %v", left, err)
	}
}

func startFloci(t *testing.T) string {
	t.Helper()
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
		t.Fatalf("host: %v", err)
	}
	port, err := ctr.MappedPort(ctx, "4566/tcp")
	if err != nil {
		t.Fatalf("port: %v", err)
	}
	return fmt.Sprintf("http://%s:%s", host, port.Port())
}

func createTable(t *testing.T, client *dynamodb.Client, table string) {
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
		t.Fatalf("create table: %v", err)
	}
}

func TestPutChunkRejectsBadLocator(t *testing.T) {
	client := dynamodb.NewFromConfig(aws.Config{Region: "us-east-1"})
	repo, err := New(client, "scholia-test")
	if err != nil {
		t.Fatal(err)
	}
	err = repo.PutChunk(context.Background(), domain.Chunk{
		ID: "k", CourseID: "c", SourceID: "s",
		Locators: []locator.Locator{{Kind: locator.KindPage, Page: 1}},
	})
	if err == nil {
		t.Fatal("missing bbox was stored")
	}
}

func TestRepositoryChatsAndSettings(t *testing.T) {
	ctx := t.Context()
	endpoint := startFloci(t)
	client, err := NewClient(ctx, "us-east-1", endpoint)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	const table = "scholia-chats"
	createTable(t, client, table)
	repo, err := New(client, table)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	repo.pageSize = 1

	if got, err := repo.GetSettings(ctx, "u1"); err != nil || got.DefaultModel != "" {
		t.Fatalf("empty settings = %+v %v", got, err)
	}
	if err := repo.PutSettings(ctx, "u1", domain.Settings{DefaultModel: "gpt-4.1-mini"}); err != nil {
		t.Fatalf("put settings: %v", err)
	}
	if got, err := repo.GetSettings(ctx, "u1"); err != nil || got.DefaultModel != "gpt-4.1-mini" {
		t.Fatalf("settings = %+v %v", got, err)
	}
	if err := repo.PutSettings(ctx, "", domain.Settings{}); err == nil {
		t.Fatal("settings without a user were stored")
	}

	t0 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	older := domain.Chat{ID: "h1", OwnerID: "u1", CourseID: "c1", Title: "New chat", CreatedAt: t0, UpdatedAt: t0}
	newer := domain.Chat{ID: "h2", OwnerID: "u1", CourseID: "c1", Title: "Routing", Model: "gpt-4.1", CreatedAt: t0, UpdatedAt: t0.Add(time.Hour)}
	for _, chat := range []domain.Chat{older, newer, {ID: "h3", OwnerID: "u2", CourseID: "c1", Title: "x", CreatedAt: t0, UpdatedAt: t0}} {
		if err := repo.PutChat(ctx, chat); err != nil {
			t.Fatalf("put chat: %v", err)
		}
	}
	if err := repo.PutChat(ctx, domain.Chat{ID: "bad"}); err == nil {
		t.Fatal("invalid chat was stored")
	}
	listed, err := repo.ListChats(ctx, "u1")
	if err != nil || len(listed) != 2 || listed[0].ID != "h2" || listed[1].ID != "h1" {
		t.Fatalf("chats = %+v %v", listed, err)
	}
	if got, err := repo.GetChat(ctx, "u1", "h2"); err != nil || got != newer {
		t.Fatalf("chat = %+v %v", got, err)
	}
	if _, err := repo.GetChat(ctx, "u2", "h1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user's chat: %v", err)
	}

	cite := []domain.Citation{{ChunkID: "k1", SourceID: "s1", Locators: []locator.Locator{{Kind: locator.KindText, Start: 0, End: 4}}}}
	msgs := []domain.Message{
		{ID: "m1", ChatID: "h1", Role: domain.RoleUser, Text: "next hop?", Mode: domain.ModeAnswer, CreatedAt: t0},
		{ID: "m2", ChatID: "h1", Role: domain.RoleAssistant, Text: "Routing.", Mode: domain.ModeAnswer, Citations: cite, CreatedAt: t0.Add(time.Second)},
		{ID: "m3", ChatID: "h1", Role: domain.RoleAssistant, Mode: domain.ModeExplain, Refusal: "nothing", RefusalReason: domain.RefusalOffTopic, CreatedAt: t0.Add(2 * time.Second)},
	}
	msgs[0].Attachments = []domain.AttachmentRef{{ID: "a1", Name: "hw.pdf", ContentType: "application/pdf"}}
	msgs[1].Web = []domain.WebResult{{Title: "TCP", URL: "https://example.test/tcp"}}
	for _, msg := range []domain.Message{msgs[2], msgs[0], msgs[1]} {
		if err := repo.PutMessage(ctx, msg); err != nil {
			t.Fatalf("put message: %v", err)
		}
	}
	if err := repo.PutMessage(ctx, domain.Message{ID: "x", ChatID: "h1", Role: "system", Mode: domain.ModeAnswer}); err == nil {
		t.Fatal("invalid message was stored")
	}
	got, err := repo.ListMessages(ctx, "h1")
	if err != nil || len(got) != 3 {
		t.Fatalf("messages = %+v %v", got, err)
	}
	for i := range msgs {
		if got[i].ID != msgs[i].ID || got[i].Refusal != msgs[i].Refusal || !got[i].CreatedAt.Equal(msgs[i].CreatedAt) {
			t.Fatalf("message %d = %+v, want %+v", i, got[i], msgs[i])
		}
	}
	if len(got[1].Citations) != 1 || got[1].Citations[0].Locators[0].End != 4 {
		t.Fatalf("citations = %+v", got[1].Citations)
	}
	if len(got[0].Attachments) != 1 || got[0].Attachments[0].Name != "hw.pdf" || len(got[1].Web) != 1 || got[1].Web[0].URL != "https://example.test/tcp" ||
		got[2].RefusalReason != domain.RefusalOffTopic || got[0].Web != nil {
		t.Fatalf("message extras = %+v", got)
	}

	att := domain.Attachment{ID: "a1", ChatID: "h1", Name: "hw.pdf", ContentType: "application/pdf", ByteSize: 12, Key: "chats/h1/attachments/a1/hw.pdf", CreatedAt: t0}
	for _, a := range []domain.Attachment{att, {ID: "a2", ChatID: "h1", Name: "b.md", ContentType: "text/markdown", ByteSize: 3, Key: "k", CreatedAt: t0}} {
		if err := repo.PutAttachment(ctx, a); err != nil {
			t.Fatalf("put attachment: %v", err)
		}
	}
	if err := repo.PutAttachment(ctx, domain.Attachment{ID: "bad", ChatID: "h1"}); err == nil {
		t.Fatal("invalid attachment was stored")
	}
	if gotAtt, err := repo.GetAttachment(ctx, "h1", "a1"); err != nil || gotAtt != att {
		t.Fatalf("attachment = %+v %v", gotAtt, err)
	}
	if _, err := repo.GetAttachment(ctx, "h2", "a1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("attachment of another chat: %v", err)
	}
	if listedAtt, err := repo.ListAttachments(ctx, "h1"); err != nil || len(listedAtt) != 2 {
		t.Fatalf("attachments = %+v %v", listedAtt, err)
	}
	if again, err := repo.ListMessages(ctx, "h1"); err != nil || len(again) != 3 {
		t.Fatalf("attachment rows leaked into messages: %+v %v", again, err)
	}
	if err := repo.DeleteAttachment(ctx, "h1", "a2"); err != nil {
		t.Fatalf("delete attachment: %v", err)
	}
	if err := repo.DeleteChat(ctx, "u1", "h1"); err != nil {
		t.Fatalf("delete chat: %v", err)
	}
	if _, err := repo.GetChat(ctx, "u1", "h1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted chat: %v", err)
	}
	if left, err := repo.ListMessages(ctx, "h1"); err != nil || len(left) != 0 {
		t.Fatalf("messages after delete = %+v %v", left, err)
	}
	if left, err := repo.ListAttachments(ctx, "h1"); err != nil || len(left) != 0 {
		t.Fatalf("attachments after delete = %+v %v", left, err)
	}
}
