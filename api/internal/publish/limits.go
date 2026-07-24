// Package publish is the metadata-gate publish orchestrator (F5, LOO-14): the one
// place that turns a private draft into a published KB entry. It composes the
// primitives the earlier milestones built — render.RenderAndSanitize (LOO-16),
// tags.Validate/ApplyDocumentTags (LOO-15), versions.CreateSnapshot (LOO-17) —
// behind a deliberate, metadata-validated, confirmed action (00-overview.md §5/§6,
// 01-architecture.md §3.5).
//
// Publish is never a side effect of saving a draft: the whole transition (status
// flip, first-publish published_at/slug, snapshot, tag apply, audit, index-outbox
// enqueue) happens atomically in one transaction, and metadata validation runs
// server-side regardless of what the client checked (06-security.md §6).
package publish

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Learistocats/LookupKB/api/internal/tags"
	"github.com/jackc/pgx/v5"
)

// Settings keys holding the admin-tunable title/description limits, alongside the
// tag-limit key (tags.LimitsSettingKey) LOO-15 already reads. Stored JSON matches
// the Bounds struct tags, e.g. {"min":3,"max":120}.
const (
	TitleLimitsSettingKey       = "title.limits"
	DescriptionLimitsSettingKey = "description.limits"
)

// Bounds is a min/max character bound for a single metadata field (00-overview.md
// §6). Lengths are measured in runes, not bytes, so multi-byte titles are not
// unfairly rejected (mirrors tags.Limits).
type Bounds struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// MetadataLimits are the admin-tunable title/description bounds. Tag limits live in
// tags.Limits (loaded via tags.LoadLimits) so the single tag rule stays in one
// place; this struct owns only the fields the publish gate itself validates.
type MetadataLimits struct {
	Title       Bounds
	Description Bounds
}

// DefaultMetadataLimits are the safe defaults from 00-overview.md §6: title 3–120,
// description 10–280. LoadMetadataLimits overrides these from the settings table
// when configured.
var DefaultMetadataLimits = MetadataLimits{
	Title:       Bounds{Min: 3, Max: 120},
	Description: Bounds{Min: 10, Max: 280},
}

// LoadMetadataLimits returns the title/description limits, starting from
// DefaultMetadataLimits and applying any overrides stored under the settings keys.
// A missing row (pgx.ErrNoRows) is the normal unconfigured case and keeps the
// default for that field. Read fresh per publish request so an admin limit change
// is honored on the next publish without a redeploy (AC-10.4). Reuses the same
// tags.SettingGetter seam as tags.LoadLimits, so *dbgen.Queries satisfies it.
func LoadMetadataLimits(ctx context.Context, getter tags.SettingGetter) (MetadataLimits, error) {
	limits := DefaultMetadataLimits

	if err := loadBounds(ctx, getter, TitleLimitsSettingKey, &limits.Title); err != nil {
		return DefaultMetadataLimits, err
	}
	if err := loadBounds(ctx, getter, DescriptionLimitsSettingKey, &limits.Description); err != nil {
		return DefaultMetadataLimits, err
	}
	return limits, nil
}

// loadBounds overlays a stored {"min","max"} JSON value onto dst, leaving dst
// unchanged when the key is unset (pgx.ErrNoRows).
func loadBounds(ctx context.Context, getter tags.SettingGetter, key string, dst *Bounds) error {
	raw, err := getter.GetSetting(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load %s: %w", key, err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("parse %s setting: %w", key, err)
	}
	return nil
}
