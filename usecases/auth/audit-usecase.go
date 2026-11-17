package usecases

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

const (
	auditActorSystem = "system"
	auditRedacted    = "***"
)

var (
	ErrAuditFilter = errors.New("invalid audit filter")

	sensitiveAuditKeys = []string{"password", "hash", "token", "secret", "otp", "api_key"}
)

type AuditUsecase struct {
	reader repository.IAuditLogReader
}

func NewAuditUsecase(reader repository.IAuditLogReader) *AuditUsecase {
	return &AuditUsecase{reader: reader}
}

func (uc *AuditUsecase) List(ctx context.Context, query dtos.AuditListQuery) (*dtos.AuditPage, error) {
	filter := repository.AuditFilter{EntityType: query.Entity, EntityID: query.EntityID, Action: query.Action}
	if query.ActorID != "" {
		id, err := uuid.Parse(query.ActorID)
		if err != nil {
			return nil, ErrAuditFilter
		}
		filter.ActorID = &id
	}
	var err error
	if filter.From, err = auditTime(query.From); err != nil {
		return nil, err
	}
	if filter.To, err = auditTime(query.To); err != nil {
		return nil, err
	}
	params := pagination.Params{Page: query.Page, Size: query.Size}.Normalize()
	filter.Limit, filter.Offset = int(params.Limit()), int(params.Offset())
	records, total, err := uc.reader.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	views := make([]dtos.AuditEntryView, 0, len(records))
	for i := range records {
		views = append(views, toAuditView(&records[i]))
	}
	page := pagination.NewPage(views, params, total)
	return &dtos.AuditPage{Items: page.Items, Page: page.Page, Size: page.Size, Total: page.Total, TotalPages: page.TotalPages}, nil
}

func auditTime(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, time.DateOnly} {
		if at, err := time.Parse(layout, raw); err == nil {
			return &at, nil
		}
	}
	return nil, ErrAuditFilter
}

func toAuditView(record *repository.AuditRecord) dtos.AuditEntryView {
	actor := dtos.AuditActorView{ID: record.ActorID, Name: record.ActorName, Type: record.ActorRole}
	if actor.ID == nil {
		actor.Type = auditActorSystem
	}
	return dtos.AuditEntryView{
		ID: record.ID, OccurredAt: record.CreatedAt, Actor: actor, OrganizationID: record.OrganizationID, Action: record.Action, Entity: record.EntityType,
		EntityID: record.EntityID, Before: redacted(record.Before), After: redacted(record.After), Reason: record.Reason, CorrelationID: record.CorrelationID,
	}
}

func redacted(snapshot json.RawMessage) json.RawMessage {
	if len(snapshot) == 0 {
		return nil
	}
	var value any
	if json.Unmarshal(snapshot, &value) != nil {
		return nil
	}
	clean, err := json.Marshal(redactValue(value))
	if err != nil {
		return nil
	}
	return clean
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, inner := range typed {
			if isSensitiveAuditKey(key) {
				typed[key] = auditRedacted
				continue
			}
			typed[key] = redactValue(inner)
		}
	case []any:
		for i := range typed {
			typed[i] = redactValue(typed[i])
		}
	}
	return value
}

func isSensitiveAuditKey(key string) bool {
	lower := strings.ToLower(key)
	for _, sensitive := range sensitiveAuditKeys {
		if strings.Contains(lower, sensitive) {
			return true
		}
	}
	return false
}
