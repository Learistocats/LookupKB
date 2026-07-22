package tags

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// LimitsSettingKey is the settings-table key holding the tag-limit overrides.
// The stored JSON matches the Limits struct tags, e.g.
// {"min_tags":1,"max_tags":8,"min_len":2,"max_len":32}.
const LimitsSettingKey = "tags.limits"

// SettingGetter reads a raw jsonb settings value by key. *dbgen.Queries satisfies
// it via the generated GetSetting query. Kept as a narrow interface so LoadLimits
// is unit-testable without a database (mirrors the DocumentStore/TagStore seams).
type SettingGetter interface {
	GetSetting(ctx context.Context, key string) ([]byte, error)
}

// LoadLimits returns the tag limits, starting from DefaultLimits and applying any
// overrides stored under LimitsSettingKey. A missing row (pgx.ErrNoRows) is the
// normal unconfigured case and yields the defaults. This is the settings-read
// seam the metadata-gate ticket (LOO-14) extends for title/description limits.
//
// Only keys present in the stored JSON override defaults (absent fields keep their
// default), so a partial settings row is safe.
func LoadLimits(ctx context.Context, getter SettingGetter) (Limits, error) {
	limits := DefaultLimits

	raw, err := getter.GetSetting(ctx, LimitsSettingKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return limits, nil
	}
	if err != nil {
		return DefaultLimits, fmt.Errorf("load tag limits: %w", err)
	}

	if err := json.Unmarshal(raw, &limits); err != nil {
		return DefaultLimits, fmt.Errorf("parse tag limits setting: %w", err)
	}
	return limits, nil
}
