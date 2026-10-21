package auth

import (
	"context"
	"errors"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// PATQueries is the subset of the generated queries the PAT verifier needs;
// *dbgen.Queries satisfies it.
type PATQueries interface {
	GetActiveAPITokenByPrefix(ctx context.Context, prefix string) (dbgen.GetActiveAPITokenByPrefixRow, error)
	TouchAPITokenLastUsed(ctx context.Context, id pgtype.UUID) error
}

// DBPATStore adapts the generated api_tokens queries to PATVerifier.
type DBPATStore struct {
	q PATQueries
}

// NewDBPATStore wraps the generated queries as a PATVerifier.
func NewDBPATStore(q PATQueries) *DBPATStore {
	return &DBPATStore{q: q}
}

// LookupPAT returns the active token with the prefix, or ErrPATNotFound.
func (s *DBPATStore) LookupPAT(ctx context.Context, prefix string) (PATRecord, error) {
	row, err := s.q.GetActiveAPITokenByPrefix(ctx, prefix)
	if errors.Is(err, pgx.ErrNoRows) {
		return PATRecord{}, ErrPATNotFound
	}
	if err != nil {
		return PATRecord{}, err
	}
	rec := PATRecord{
		ID:      uuid.UUID(row.ID.Bytes),
		UserID:  uuid.UUID(row.UserID.Bytes),
		Email:   row.Email,
		Hash:    row.TokenHash,
		Revoked: row.RevokedAt.Valid,
	}
	if row.ExpiresAt.Valid {
		rec.ExpiresAt = row.ExpiresAt.Time
	}
	if row.LastUsedAt.Valid {
		rec.LastUsedAt = row.LastUsedAt.Time
	}
	return rec, nil
}

// TouchPAT stamps last_used_at (throttled in SQL).
func (s *DBPATStore) TouchPAT(ctx context.Context, id uuid.UUID) error {
	return s.q.TouchAPITokenLastUsed(ctx, pgtype.UUID{Bytes: id, Valid: true})
}
