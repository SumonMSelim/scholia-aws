package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/sumonmselim/scholia-aws/internal/domain"
)

type keyItem struct {
	PK         string `dynamodbav:"pk"`
	SK         string `dynamodbav:"sk"`
	Entity     string `dynamodbav:"entity"`
	UserID     string `dynamodbav:"user_id"`
	Provider   string `dynamodbav:"provider"`
	Ciphertext []byte `dynamodbav:"ciphertext"`
}

type settingsItem struct {
	PK           string `dynamodbav:"pk"`
	SK           string `dynamodbav:"sk"`
	Entity       string `dynamodbav:"entity"`
	UserID       string `dynamodbav:"user_id"`
	DefaultModel string `dynamodbav:"default_model,omitempty"`
	ExpiresAt    int64  `dynamodbav:"expires_at,omitempty"`
}

type chatItem struct {
	PK        string `dynamodbav:"pk"`
	SK        string `dynamodbav:"sk"`
	Entity    string `dynamodbav:"entity"`
	ID        string `dynamodbav:"id"`
	OwnerID   string `dynamodbav:"owner_id"`
	CourseID  string `dynamodbav:"course_id"`
	Title     string `dynamodbav:"title"`
	Model     string `dynamodbav:"model,omitempty"`
	CreatedAt string `dynamodbav:"created_at"`
	UpdatedAt string `dynamodbav:"updated_at"`
	ExpiresAt int64  `dynamodbav:"expires_at,omitempty"`
}

type messageItem struct {
	PK     string `dynamodbav:"pk"`
	SK     string `dynamodbav:"sk"`
	Entity string `dynamodbav:"entity"`
	ID     string `dynamodbav:"id"`
	ChatID string `dynamodbav:"chat_id"`
	Role   string `dynamodbav:"role"`
	Text   string `dynamodbav:"text,omitempty"`
	Mode   string `dynamodbav:"mode"`
	// Citations is JSON so locator zero offsets survive, as on chunks.
	Citations string `dynamodbav:"citations,omitempty"`
	// Web and Attachments are JSON for the same reason.
	Web           string `dynamodbav:"web,omitempty"`
	Attachments   string `dynamodbav:"attachments,omitempty"`
	Refusal       string `dynamodbav:"refusal,omitempty"`
	RefusalReason string `dynamodbav:"refusal_reason,omitempty"`
	CreatedAt     string `dynamodbav:"created_at"`
	ExpiresAt     int64  `dynamodbav:"expires_at,omitempty"`
}

type attachmentItem struct {
	PK          string `dynamodbav:"pk"`
	SK          string `dynamodbav:"sk"`
	Entity      string `dynamodbav:"entity"`
	ID          string `dynamodbav:"id"`
	ChatID      string `dynamodbav:"chat_id"`
	Name        string `dynamodbav:"name"`
	ContentType string `dynamodbav:"content_type"`
	ByteSize    int64  `dynamodbav:"byte_size"`
	Key         string `dynamodbav:"object_key"`
	CreatedAt   string `dynamodbav:"created_at"`
	ExpiresAt   int64  `dynamodbav:"expires_at,omitempty"`
}

func keyKey(userID, provider string) (string, string) {
	return "USER#" + userID, "KEY#" + provider
}

func settingsKey(userID string) (string, string) {
	return "USER#" + userID, "SETTINGS"
}

func chatKey(userID, chatID string) (string, string) {
	return "USER#" + userID + "#CHATS", "CHAT#" + chatID
}

func messageKey(chatID string, at time.Time, id string) (string, string) {
	return "CHAT#" + chatID, fmt.Sprintf("MSG#%020d#%s", at.UnixNano(), id)
}

func attachmentKey(chatID, id string) (string, string) {
	return "CHAT#" + chatID, "ATT#" + id
}

// PutProviderKey stores ciphertext for one user and provider. The plaintext key is never stored.
func (r *Repository) PutProviderKey(ctx context.Context, userID, provider string, ciphertext []byte) error {
	if userID == "" || provider == "" || len(ciphertext) == 0 {
		return errors.New("provider key is incomplete")
	}
	pk, sk := keyKey(userID, provider)
	if err := r.put(ctx, keyItem{
		PK: pk, SK: sk, Entity: "provider_key",
		UserID: userID, Provider: provider, Ciphertext: ciphertext,
	}); err != nil {
		return fmt.Errorf("put provider key: %w", err)
	}
	return nil
}

// GetProviderKey returns the stored ciphertext for provider or ErrNotFound.
func (r *Repository) GetProviderKey(ctx context.Context, userID, provider string) ([]byte, error) {
	pk, sk := keyKey(userID, provider)
	var item keyItem
	ok, err := r.get(ctx, pk, sk, &item)
	if err != nil {
		return nil, fmt.Errorf("get provider key: %w", err)
	}
	if !ok || len(item.Ciphertext) == 0 {
		return nil, fmt.Errorf("provider key %s: %w", provider, ErrNotFound)
	}
	return item.Ciphertext, nil
}

// ListProviderKeys returns the providers that have a stored key.
func (r *Repository) ListProviderKeys(ctx context.Context, userID string) ([]string, error) {
	pk, _ := keyKey(userID, "")
	items, err := r.query(ctx, pk, "KEY#")
	if err != nil {
		return nil, fmt.Errorf("list provider keys: %w", err)
	}
	out := make([]string, 0, len(items))
	for _, raw := range items {
		var item keyItem
		if err := attributevalue.UnmarshalMap(raw, &item); err != nil {
			return nil, err
		}
		if len(item.Ciphertext) > 0 {
			out = append(out, item.Provider)
		}
	}
	return out, nil
}

// DeleteProviderKey removes one provider's key. A missing key is not an error.
func (r *Repository) DeleteProviderKey(ctx context.Context, userID, provider string) error {
	pk, sk := keyKey(userID, provider)
	return r.delete(ctx, pk, sk)
}

// GetSettings returns the user's settings. A user with none gets the zero value.
func (r *Repository) GetSettings(ctx context.Context, userID string) (domain.Settings, error) {
	pk, sk := settingsKey(userID)
	var item settingsItem
	if _, err := r.get(ctx, pk, sk, &item); err != nil {
		return domain.Settings{}, fmt.Errorf("get settings: %w", err)
	}
	return domain.Settings{DefaultModel: item.DefaultModel, ExpiresAt: item.ExpiresAt}, nil
}

// PutSettings replaces the user's settings.
func (r *Repository) PutSettings(ctx context.Context, userID string, settings domain.Settings) error {
	if userID == "" {
		return errors.New("settings: user id is required")
	}
	pk, sk := settingsKey(userID)
	return r.put(ctx, settingsItem{PK: pk, SK: sk, Entity: "settings", UserID: userID, DefaultModel: settings.DefaultModel, ExpiresAt: settings.ExpiresAt})
}

// PutChat creates or replaces a chat under its owner.
func (r *Repository) PutChat(ctx context.Context, chat domain.Chat) error {
	if err := chat.Validate(); err != nil {
		return err
	}
	pk, sk := chatKey(chat.OwnerID, chat.ID)
	return r.put(ctx, chatItem{
		PK: pk, SK: sk, Entity: "chat",
		ID: chat.ID, OwnerID: chat.OwnerID, CourseID: chat.CourseID, Title: chat.Title, Model: chat.Model,
		CreatedAt: chat.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: chat.UpdatedAt.UTC().Format(time.RFC3339Nano),
		ExpiresAt: chat.ExpiresAt,
	})
}

// GetChat returns the owner's chat or ErrNotFound. Another user's chat id is not found.
func (r *Repository) GetChat(ctx context.Context, userID, chatID string) (domain.Chat, error) {
	pk, sk := chatKey(userID, chatID)
	var item chatItem
	ok, err := r.get(ctx, pk, sk, &item)
	if err != nil {
		return domain.Chat{}, fmt.Errorf("get chat: %w", err)
	}
	if !ok {
		return domain.Chat{}, fmt.Errorf("chat %s: %w", chatID, ErrNotFound)
	}
	return item.chat()
}

// ListChats returns the owner's chats, most recently updated first.
func (r *Repository) ListChats(ctx context.Context, userID string) ([]domain.Chat, error) {
	pk, _ := chatKey(userID, "")
	items, err := r.query(ctx, pk, "CHAT#")
	if err != nil {
		return nil, fmt.Errorf("list chats: %w", err)
	}
	out := make([]domain.Chat, 0, len(items))
	for _, raw := range items {
		var item chatItem
		if err := attributevalue.UnmarshalMap(raw, &item); err != nil {
			return nil, err
		}
		chat, err := item.chat()
		if err != nil {
			return nil, err
		}
		out = append(out, chat)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

// DeleteChat removes the chat, its messages and its attachment rows.
// The caller removes attachment objects from the bucket.
func (r *Repository) DeleteChat(ctx context.Context, userID, chatID string) error {
	childPK := "CHAT#" + chatID
	for _, prefix := range []string{"MSG#", "ATT#"} {
		items, err := r.query(ctx, childPK, prefix)
		if err != nil {
			return fmt.Errorf("list chat items: %w", err)
		}
		for _, raw := range items {
			sk, ok := raw["sk"].(*types.AttributeValueMemberS)
			if !ok {
				continue
			}
			if err := r.delete(ctx, childPK, sk.Value); err != nil {
				return fmt.Errorf("delete chat item: %w", err)
			}
		}
	}
	pk, sk := chatKey(userID, chatID)
	return r.delete(ctx, pk, sk)
}

// PutAttachment records one chat attachment.
func (r *Repository) PutAttachment(ctx context.Context, att domain.Attachment) error {
	if err := att.Validate(); err != nil {
		return err
	}
	pk, sk := attachmentKey(att.ChatID, att.ID)
	return r.put(ctx, attachmentItem{
		PK: pk, SK: sk, Entity: "attachment",
		ID: att.ID, ChatID: att.ChatID, Name: att.Name, ContentType: att.ContentType,
		ByteSize: att.ByteSize, Key: att.Key, CreatedAt: att.CreatedAt.UTC().Format(time.RFC3339Nano),
		ExpiresAt: att.ExpiresAt,
	})
}

// GetAttachment returns one attachment of a chat or ErrNotFound.
func (r *Repository) GetAttachment(ctx context.Context, chatID, id string) (domain.Attachment, error) {
	pk, sk := attachmentKey(chatID, id)
	var item attachmentItem
	ok, err := r.get(ctx, pk, sk, &item)
	if err != nil {
		return domain.Attachment{}, fmt.Errorf("get attachment: %w", err)
	}
	if !ok {
		return domain.Attachment{}, fmt.Errorf("attachment %s: %w", id, ErrNotFound)
	}
	return item.attachment()
}

// ListAttachments returns every attachment of a chat.
func (r *Repository) ListAttachments(ctx context.Context, chatID string) ([]domain.Attachment, error) {
	items, err := r.query(ctx, "CHAT#"+chatID, "ATT#")
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	out := make([]domain.Attachment, 0, len(items))
	for _, raw := range items {
		var item attachmentItem
		if err := attributevalue.UnmarshalMap(raw, &item); err != nil {
			return nil, err
		}
		att, err := item.attachment()
		if err != nil {
			return nil, err
		}
		out = append(out, att)
	}
	return out, nil
}

// DeleteAttachment removes one attachment row.
func (r *Repository) DeleteAttachment(ctx context.Context, chatID, id string) error {
	pk, sk := attachmentKey(chatID, id)
	return r.delete(ctx, pk, sk)
}

func (item attachmentItem) attachment() (domain.Attachment, error) {
	created, err := time.Parse(time.RFC3339Nano, item.CreatedAt)
	if err != nil {
		return domain.Attachment{}, fmt.Errorf("attachment %s created_at: %w", item.ID, err)
	}
	return domain.Attachment{
		ID: item.ID, ChatID: item.ChatID, Name: item.Name, ContentType: item.ContentType,
		ByteSize: item.ByteSize, Key: item.Key, CreatedAt: created, ExpiresAt: item.ExpiresAt,
	}, nil
}

// PutMessage appends one message to a chat.
func (r *Repository) PutMessage(ctx context.Context, msg domain.Message) error {
	if err := msg.Validate(); err != nil {
		return err
	}
	citations, err := jsonList(msg.Citations)
	if err != nil {
		return err
	}
	web, err := jsonList(msg.Web)
	if err != nil {
		return err
	}
	attachments, err := jsonList(msg.Attachments)
	if err != nil {
		return err
	}
	pk, sk := messageKey(msg.ChatID, msg.CreatedAt, msg.ID)
	return r.put(ctx, messageItem{
		PK: pk, SK: sk, Entity: "message",
		ID: msg.ID, ChatID: msg.ChatID, Role: msg.Role, Text: msg.Text, Mode: msg.Mode,
		Citations: citations, Web: web, Attachments: attachments,
		Refusal: msg.Refusal, RefusalReason: msg.RefusalReason,
		CreatedAt: msg.CreatedAt.UTC().Format(time.RFC3339Nano),
		ExpiresAt: msg.ExpiresAt,
	})
}

// ListMessages returns a chat's messages in the order they were written.
func (r *Repository) ListMessages(ctx context.Context, chatID string) ([]domain.Message, error) {
	items, err := r.query(ctx, "CHAT#"+chatID, "MSG#")
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	out := make([]domain.Message, 0, len(items))
	for _, raw := range items {
		var item messageItem
		if err := attributevalue.UnmarshalMap(raw, &item); err != nil {
			return nil, err
		}
		msg, err := item.message()
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, nil
}

func (item chatItem) chat() (domain.Chat, error) {
	created, err := time.Parse(time.RFC3339Nano, item.CreatedAt)
	if err != nil {
		return domain.Chat{}, fmt.Errorf("chat %s created_at: %w", item.ID, err)
	}
	updated, err := time.Parse(time.RFC3339Nano, item.UpdatedAt)
	if err != nil {
		return domain.Chat{}, fmt.Errorf("chat %s updated_at: %w", item.ID, err)
	}
	return domain.Chat{
		ID: item.ID, OwnerID: item.OwnerID, CourseID: item.CourseID, Title: item.Title, Model: item.Model,
		CreatedAt: created, UpdatedAt: updated, ExpiresAt: item.ExpiresAt,
	}, nil
}

func (item messageItem) message() (domain.Message, error) {
	created, err := time.Parse(time.RFC3339Nano, item.CreatedAt)
	if err != nil {
		return domain.Message{}, fmt.Errorf("message %s created_at: %w", item.ID, err)
	}
	var msg domain.Message
	for name, field := range map[string]struct {
		raw  string
		dest any
	}{
		"citations":   {item.Citations, &msg.Citations},
		"web":         {item.Web, &msg.Web},
		"attachments": {item.Attachments, &msg.Attachments},
	} {
		if strings.TrimSpace(field.raw) == "" {
			continue
		}
		if err := json.Unmarshal([]byte(field.raw), field.dest); err != nil {
			return domain.Message{}, fmt.Errorf("message %s %s: %w", item.ID, name, err)
		}
	}
	msg.ID, msg.ChatID, msg.Role, msg.Text, msg.Mode = item.ID, item.ChatID, item.Role, item.Text, item.Mode
	msg.Refusal, msg.RefusalReason, msg.CreatedAt = item.Refusal, item.RefusalReason, created
	msg.ExpiresAt = item.ExpiresAt
	return msg, nil
}

// jsonList stores a list as JSON, or as nothing when it is empty.
func jsonList[T any](rows []T) (string, error) {
	if len(rows) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
