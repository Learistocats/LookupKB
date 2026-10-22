package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// IsUniqueViolation reports whether err is a Postgres unique_violation (23505).
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// FormatUUID renders a UUID as text; an invalid (NULL) value is "".
func FormatUUID(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}

// FormatTime renders a timestamp as UTC RFC 3339; an invalid (NULL) value is "".
func FormatTime(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}

// Deref returns *s, or "" for nil.
func Deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// NonNil returns s, or an empty slice for nil, so it marshals as [] not null.
func NonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// AuditWriter is the one query WriteAudit needs; *dbgen.Queries (and its WithTx
// form) satisfies it.
type AuditWriter interface {
	InsertAuditLog(ctx context.Context, arg dbgen.InsertAuditLogParams) error
}

// WriteAudit appends one audit_log row on the caller's (transaction-bound)
// queries. Unmarshalable meta falls back to {} rather than losing the fact.
func WriteAudit(ctx context.Context, q AuditWriter, actor pgtype.UUID, action, targetType, targetID string, meta map[string]any) error {
	raw, err := json.Marshal(meta)
	if err != nil {
		raw = []byte("{}")
	}
	if err := q.InsertAuditLog(ctx, dbgen.InsertAuditLogParams{
		ActorID:    actor,
		Action:     action,
		TargetType: targetType,
		TargetID:   &targetID,
		Metadata:   raw,
	}); err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}
	return nil
}
