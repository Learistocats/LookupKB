package publish

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/render"
	"github.com/Learistocats/LookupKB/api/internal/tags"
	"github.com/Learistocats/LookupKB/api/internal/versions"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound means the document does not exist, is not the caller's, is
// soft-deleted, or (for unpublish) is not currently published. The handler maps it
// to a 404 with an identical body for every case, so a probing user can never
// distinguish "not yours" from "doesn't exist" (03-data-model.md §2).
var ErrNotFound = errors.New("document not found")

// Service orchestrates the publish/unpublish transitions. It holds the pool so it
// can run the whole transition in ONE transaction (unlike the per-primitive stores
// that each own a tx), and a shared Renderer (concurrency-safe) it invokes before
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

// Input is a publish attempt: the target document, the caller (author), and
// the metadata entered in the confirm flow. source_md is not here — it is the
// server-held working copy read from the document row, never trusted from the client.
type Input struct {
	DocID       pgtype.UUID
	AuthorID    pgtype.UUID
	Title       string
	Description string
	Tags        []string
}

// Result is the outcome of a successful publish/unpublish: the updated document row
// and (for publish) the version number of the snapshot just created.
type Result struct {
	Document  dbgen.Document
	VersionNo int32
}

// Publish validates the metadata, renders the working copy, and — only if both
// succeed — atomically transitions the document to published: snapshot, tag apply,
// audit and index-enqueue all commit together or not at all.
//
// On a metadata violation it returns the per-field errors with error==nil and NO
// state change (the document stays a draft, AC-5.2). A render failure is surfaced
// the same way (a "content" field error) rather than as a 500, and crucially
// happens BEFORE the transaction opens, so a Kroki/render failure can never leave a
// half-published document (ticket "things not to miss").
func (s *Service) Publish(ctx context.Context, in Input) (*Result, []FieldError, error) {
	// Limits are read fresh per request so an admin change is honored on the next
	// publish without a redeploy (AC-10.4).
	ml, err := LoadMetadataLimits(ctx, s.q)
	if err != nil {
		return nil, nil, fmt.Errorf("load metadata limits: %w", err)
	}
	tl, err := tags.LoadLimits(ctx, s.q)
	if err != nil {
		return nil, nil, fmt.Errorf("load tag limits: %w", err)
	}

	// The working copy in any status (draft on first publish, published on
	// re-publish). Owner-scoped: a cross-author or missing id is ErrNotFound.
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

	// Render before the transaction: goldmark+bluemonday now, Kroki later (M4). A
	// too-large source is a per-field error; any other render error is a real fault.
	source := deref(doc.SourceMd)
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

	// Slug is title-derived on FIRST publish only, then stable. The SQL CASE also
	// guards this, but computing it only when needed avoids burning randomness.
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
		Slug:         slug,
		ID:           in.DocID,
		AuthorID:     in.AuthorID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("publish document: %w", err)
	}

	// Immutable snapshot of exactly what was published; also repoints
	// current_version_id. Access is captured from the row (public this milestone).
	ver, err := versions.CreateSnapshot(ctx, qtx, versions.Snapshot{
		DocumentID:   in.DocID,
		Title:        &title,
		Description:  &description,
		SourceMD:     doc.SourceMd,
		RenderedHTML: &html,
		Tags:         normTags,
		Access:       published.Access,
		ACLGroupIDs:  nil, // restricted-group ACLs are M3.
		PublishedBy:  in.AuthorID,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("create snapshot: %w", err)
	}

	// Live tag membership + usage_count, composed into this same transaction so a
	// later failure rolls the counts back too.
	if err := tags.ApplyDocumentTags(ctx, qtx, in.DocID, normTags); err != nil {
		return nil, nil, fmt.Errorf("apply tags: %w", err)
	}

	if err := s.audit(ctx, qtx, in.AuthorID, "document.publish", in.DocID, map[string]any{
		"version_no":    ver.VersionNo,
		"first_publish": firstPublish,
	}); err != nil {
		return nil, nil, err
	}

	// Enqueue the Meili upsert; the M2 consumer processes it. Enqueued in-tx so a
	// rolled-back publish leaves no stray index op.
	if err := qtx.EnqueueIndexOp(ctx, dbgen.EnqueueIndexOpParams{DocumentID: in.DocID, Op: "upsert"}); err != nil {
		return nil, nil, fmt.Errorf("enqueue index op: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit publish tx: %w", err)
	}

	return &Result{Document: published, VersionNo: ver.VersionNo}, nil, nil
}

// Unpublish reverts a published document to author-only 'unpublished' visibility,
// enqueues its removal from the index, and drops its live tag counts — all in one
// transaction. published_at and the version history are retained (AC-5.6).
func (s *Service) Unpublish(ctx context.Context, docID, authorID pgtype.UUID) (*Result, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin unpublish tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after Commit; safe rollback on any early return
	qtx := s.q.WithTx(tx)

	// Owner-scoped and status='published'-guarded, so only a currently-published
	// doc of the caller's transitions; anything else is ErrNotFound.
	doc, err := qtx.UnpublishDocument(ctx, dbgen.UnpublishDocumentParams{ID: docID, AuthorID: authorID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("unpublish document: %w", err)
	}

	// Detach live tags so an unpublished doc stops counting toward tag popularity;
	// the version snapshots keep their own immutable tag copies.
	if err := tags.ApplyDocumentTags(ctx, qtx, docID, nil); err != nil {
		return nil, fmt.Errorf("clear tags: %w", err)
	}

	if err := s.audit(ctx, qtx, authorID, "document.unpublish", docID, map[string]any{}); err != nil {
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

// audit writes one audit_log row inside the caller's transaction (AC-10.5). It is
// the sole audit path for publish/unpublish; the metadata jsonb carries per-action
// context. A marshal failure falls back to an empty object rather than losing the
// audited fact.
func (s *Service) audit(ctx context.Context, q *dbgen.Queries, actor pgtype.UUID, action string, target pgtype.UUID, meta map[string]any) error {
	raw, err := json.Marshal(meta)
	if err != nil {
		raw = []byte("{}")
	}
	targetID := uuid.UUID(target.Bytes).String()
	if err := q.InsertAuditLog(ctx, dbgen.InsertAuditLogParams{
		ActorID:    actor,
		Action:     action,
		TargetType: "document",
		TargetID:   &targetID,
		Metadata:   raw,
	}); err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}
	return nil
}

// maxSlugRunes bounds the human-readable part of a published slug so a very long
// title doesn't produce an unwieldy URL; the random suffix still guarantees uniqueness.
const maxSlugRunes = 80

// newPublishedSlug derives a stable, URL-safe slug from the title on first publish:
// the normalized (lowercase-kebab) title, truncated, plus a short random suffix for
// uniqueness (03-data-model.md §1 `slug`: "title + short id, stable across edits").
// A title that normalizes to nothing (all punctuation) falls back to "doc".
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

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
