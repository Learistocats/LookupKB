package publish

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/tags"
)

// Settings keys holding the admin-tunable title/description limits. Stored JSON
// matches Bounds, e.g. {"min":3,"max":120}.
const (
	TitleLimitsSettingKey       = "title.limits"
	DescriptionLimitsSettingKey = "description.limits"
)

// Bounds is a min/max length for one metadata field, counted in runes so
// multi-byte titles are not unfairly rejected.
type Bounds struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// MetadataLimits are the title/description bounds. Tag limits live in tags.Limits
// so the tag rule stays in one place.
type MetadataLimits struct {
	Title       Bounds
	Description Bounds
}

// DefaultMetadataLimits apply when nothing is configured.
var DefaultMetadataLimits = MetadataLimits{
	Title:       Bounds{Min: 3, Max: 120},
	Description: Bounds{Min: 10, Max: 280},
}

// settingsReader is satisfied by *dbgen.Queries.
type settingsReader interface {
	GetSettings(ctx context.Context, keys []string) ([]dbgen.GetSettingsRow, error)
}

// LoadLimits reads the title, description and tag limits in one query, overlaying
// stored overrides on the defaults. It is read per publish so an admin change
// applies to the next publish. On error the defaults are returned alongside it
// rather than a half-parsed set that could weaken the gate.
func LoadLimits(ctx context.Context, r settingsReader) (MetadataLimits, tags.Limits, error) {
	rows, err := r.GetSettings(ctx, []string{TitleLimitsSettingKey, DescriptionLimitsSettingKey, tags.LimitsSettingKey})
	if err != nil {
		return DefaultMetadataLimits, tags.DefaultLimits, fmt.Errorf("load limits: %w", err)
	}
	ml, tl := DefaultMetadataLimits, tags.DefaultLimits
	for _, row := range rows {
		var dst any
		switch row.Key {
		case TitleLimitsSettingKey:
			dst = &ml.Title
		case DescriptionLimitsSettingKey:
			dst = &ml.Description
		case tags.LimitsSettingKey:
			dst = &tl
		default:
			continue
		}
		if err := json.Unmarshal(row.Value, dst); err != nil {
			return DefaultMetadataLimits, tags.DefaultLimits, fmt.Errorf("parse %s setting: %w", row.Key, err)
		}
	}
	return ml, tl, nil
}
