package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/sumonmselim/scholia-aws/internal/domain"
)

// ErrQuota means the day's allowance for that kind is used up. Nothing was counted.
var ErrQuota = errors.New("daily allowance is used up")

// usageKeep is how long a day's row outlives the day, so a late read still sees it.
const usageKeep = 24 * time.Hour

type usageItem struct {
	Messages int `dynamodbav:"messages"`
	Uploads  int `dynamodbav:"uploads"`
	Web      int `dynamodbav:"web"`
}

func usageKey(subject string, day time.Time) (string, string) {
	return "USAGE#" + subject, "DAY#" + day.UTC().Format(time.DateOnly)
}

func usageKind(kind string) bool {
	switch kind {
	case domain.UsageMessages, domain.UsageUploads, domain.UsageWeb, domain.UsageGuests, domain.UsageCodes:
		return true
	}
	return false
}

// AddUsage counts one call of kind for subject on the UTC day of now, unless the
// count has reached limit. The check and the increment are one conditional
// update, so concurrent Lambdas cannot both take the last unit.
func (r *Repository) AddUsage(ctx context.Context, subject, kind string, now time.Time, limit int) (int, error) {
	if subject == "" || !usageKind(kind) || limit < 1 {
		return 0, errors.New("usage: subject, a known kind and a positive limit are required")
	}
	pk, sk := usageKey(subject, now)
	end := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day()+1, 0, 0, 0, 0, time.UTC)
	out, err := r.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:           &r.table,
		Key:                 itemKey(pk, sk),
		UpdateExpression:    aws.String("SET entity = :entity, expires_at = if_not_exists(expires_at, :exp) ADD #k :one"),
		ConditionExpression: aws.String("attribute_not_exists(#k) OR #k < :limit"),
		ExpressionAttributeNames: map[string]string{
			"#k": kind,
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":one":    &types.AttributeValueMemberN{Value: "1"},
			":limit":  &types.AttributeValueMemberN{Value: strconv.Itoa(limit)},
			":exp":    &types.AttributeValueMemberN{Value: strconv.FormatInt(end.Add(usageKeep).Unix(), 10)},
			":entity": &types.AttributeValueMemberS{Value: "usage"},
		},
		ReturnValues: types.ReturnValueUpdatedNew,
	})
	if err != nil {
		var failed *types.ConditionalCheckFailedException
		if errors.As(err, &failed) {
			return limit, ErrQuota
		}
		return 0, fmt.Errorf("add usage: %w", err)
	}
	n, ok := out.Attributes[kind].(*types.AttributeValueMemberN)
	if !ok {
		return 0, errors.New("add usage: count missing from the reply")
	}
	count, err := strconv.Atoi(n.Value)
	if err != nil {
		return 0, fmt.Errorf("add usage: %w", err)
	}
	return count, nil
}

// GetUsage returns subject's counts for the UTC day of now. No row reads as zero.
func (r *Repository) GetUsage(ctx context.Context, subject string, now time.Time) (domain.Usage, error) {
	pk, sk := usageKey(subject, now)
	var item usageItem
	if _, err := r.get(ctx, pk, sk, &item); err != nil {
		return domain.Usage{}, fmt.Errorf("get usage: %w", err)
	}
	return domain.Usage{Messages: item.Messages, Uploads: item.Uploads, Web: item.Web}, nil
}
