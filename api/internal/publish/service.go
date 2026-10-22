// Package publish turns a draft into a published entry. The whole transition
// (status flip, first-publish slug/published_at, tag apply, audit, index enqueue)
// is one transaction, and metadata is validated server-side regardless of what the
// client checked. Publish is never a side effect of saving a draft.
package publish

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/Learistocats/LookupKB/api/internal/db"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/render"
	"github.com/Learistocats/LookupKB/api/internal/tags"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound means the document does not exist, is not the caller's, is
// soft-deleted, or (for unpublish) is not currently published. The handler maps it
// to a 404 with an identical body for every case, so a probing user can never
// distinguish "not yours" from "doesn't exist".
var ErrNotFound = errors.New("document not found")

// Service orchestrates the publish/unpublish transitions. It holds the pool to run
// each transition in one transaction, and a shared Renderer it invokes before
// opening that transaction.
type Service struct {
	pool     *pgxpool.Pool
	q        *dbgen.Queries
	renderer *render.Renderer
	log      *slog.Logger
}

// NewService builds a publish Service over the pool + shared renderer.
func NewService(pool *pgxpool.Pool, renderer *render.Renderer, log *slog.Logger) *Service {
	return &Service{pool: pool, q: dbgen.New(pool), renderer: renderer, log: log}
}

// Input is a publish attempt. source_md is not here: it is the server-held working
// copy read from the document row, never trusted from the client.
type Input struct {
	DocID    pgtype.UUID
	AuthorID pgtype.UUID
	// IsSuperAdmin may tag an entry with any protected tag.
	IsSuperAdmin bool
	Title        string
	Description  string
	Tags         []string
}

// Result is the outcome of a successful publish/unpublish: the updated document row.
type Result struct {
	Document dbgen.Document
}

// Publish validates the metadata, renders the working copy, and — only if both
// succeed — atomically transitions the document to published.
//
// On a metadata violation it returns the per-field errors with error==nil and no
// state change. A too-large source is surfaced the same way (a "content" field
// error), and rendering happens before the transaction opens so a render failure
// can never leave a half-published document.
func (s *Service) Publish(ctx context.Context, in Input) (*Result, []FieldError, error) {
	ml, tl, err := LoadLimits(ctx, s.q)
	if err != nil {
		return nil, nil, err
	}

	// Owner-scoped: a cross-author or missing id is ErrNotFound.
	doc, err := s.q.GetOwnedDocument(ctx, dbgen.GetOwnedDocumentParams{ID: in.DocID, AuthorID: in.AuthorID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get document: %w", err)
	}

	normTags, fieldErrs := ValidateMetadata(ml, tl, MetadataInput{
		Title:       in.Title,
		Description: in.Description,
		Tags:        in.Tags,
	})

	// The author must be granted every protected tag they put on the entry.
	caller := access.Caller{UserID: in.AuthorID, IsSuperAdmin: in.IsSuperAdmin}
	protErrs, err := ProtectedTagErrors(ctx, s.q, caller, normalizedSet(in.Tags))
	if err != nil {
		return nil, nil, fmt.Errorf("check protected tags: %w", err)
	}
	fieldErrs = append(fieldErrs, protErrs...)

	// A too-large source is a per-field error; any other render error is a real fault.
	source := db.Deref(doc.SourceMd)
	html, rerr := s.renderer.RenderAndSanitize(ctx, source)
	switch {
	case errors.Is(rerr, render.ErrSourceTooLarge):
		fieldErrs = append(fieldErrs, FieldError{Field: FieldContent, Message: "document is too large to publish"})
	case rerr != nil:
		return nil, nil, fmt.Errorf("render document: %w", rerr)
	}

	if len(fieldErrs) > 0 {
		return nil, fieldErrs, nil
	}

	title := strings.TrimSpace(in.Title)
	description := strings.TrimSpace(in.Description)

	// Title-derived on first publish only, then stable (the SQL CASE also guards it).
	firstPublish := !doc.PublishedAt.Valid
	slug := doc.Slug
	if firstPublish {
		slug, err = newPublishedSlug(title)
		if err != nil {
			return nil, nil, fmt.Errorf("generate slug: %w", err)
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin publish tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after Commit; safe rollback on any early return
	qtx := s.q.WithTx(tx)

	published, err := qtx.PublishDocument(ctx, dbgen.PublishDocumentParams{
		Title:        &title,
		Description:  &description,
		RenderedHtml: &html,
		// The exact source rendered above, decoupled from later working-copy saves.
		PublishedMd: &source,
		Slug:        slug,
		ID:          in.DocID,
		AuthorID:    in.AuthorID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("publish document: %w", err)
	}

	// Re-check inside the transaction: an admin may have protected one of these tags
	// since the first check. A refusal rolls the whole publish back.
	protErrs, err = ProtectedTagErrors(ctx, qtx, caller, normTags)
	if err != nil {
		return nil, nil, fmt.Errorf("re-check protected tags: %w", err)
	}
	if len(protErrs) > 0 {
		return nil, protErrs, nil
	}

	if err := tags.ApplyDocumentTags(ctx, qtx, in.DocID, normTags); err != nil {
		return nil, nil, fmt.Errorf("apply tags: %w", err)
	}

	if err := db.WriteAudit(ctx, qtx, in.AuthorID, "document.publish", "document", db.FormatUUID(in.DocID), map[string]any{
		"first_publish": firstPublish,
	}); err != nil {
		return nil, nil, err
	}

	// Enqueued in-tx so a rolled-back publish leaves no stray index op.
	if err := qtx.EnqueueIndexOp(ctx, dbgen.EnqueueIndexOpParams{DocumentID: in.DocID, Op: "upsert"}); err != nil {
		return nil, nil, fmt.Errorf("enqueue index op: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit publish tx: %w", err)
	}

	return &Result{Document: published}, nil, nil
}

// Unpublish reverts a published document to author-only 'unpublished' visibility,
// enqueues its removal from the index, and drops its live tag counts, all in one
// transaction. published_at and the rendered HTML are retained.
func (s *Service) Unpublish(ctx context.Context, docID, authorID pgtype.UUID) (*Result, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin unpublish tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after Commit; safe rollback on any early return
	qtx := s.q.WithTx(tx)

	// Owner-scoped and status-guarded: anything else is ErrNotFound.
	doc, err := qtx.UnpublishDocument(ctx, dbgen.UnpublishDocumentParams{ID: docID, AuthorID: authorID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("unpublish document: %w", err)
	}

	if err := tags.ApplyDocumentTags(ctx, qtx, docID, nil); err != nil {
		return nil, fmt.Errorf("clear tags: %w", err)
	}

	if err := db.WriteAudit(ctx, qtx, authorID, "document.unpublish", "document", db.FormatUUID(docID), map[string]any{}); err != nil {
		return nil, err
	}

	if err := qtx.EnqueueIndexOp(ctx, dbgen.EnqueueIndexOpParams{DocumentID: docID, Op: "delete"}); err != nil {
		return nil, fmt.Errorf("enqueue index op: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit unpublish tx: %w", err)
	}

	return &Result{Document: doc}, nil
}

// maxSlugRunes bounds the human-readable part of a published slug so a very long
// title doesn't produce an unwieldy URL; the random suffix still guarantees uniqueness.
const maxSlugRunes = 80

// newPublishedSlug derives a URL-safe slug from the title: the kebab-cased,
// truncated title plus a short random suffix for uniqueness. A title that
// normalizes to nothing falls back to "doc".
func newPublishedSlug(title string) (string, error) {
	base := truncateRunes(tags.Normalize(title), maxSlugRunes)
	base = strings.Trim(base, "-")
	if base == "" {
		base = "doc"
	}
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base + "-" + hex.EncodeToString(b[:]), nil
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// normalizedSet normalizes raw tags (dropping empties) for the protected-tag check,
// independent of whether the tag-limit validation passed.
func normalizedSet(raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		if slug := tags.Normalize(r); slug != "" {
			out = append(out, slug)
		}
	}
	return out
}
