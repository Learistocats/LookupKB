package tags

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
)

// LimitsSettingKey is the settings-table key holding the tag-limit overrides, e.g.
// {"min_tags":1,"max_tags":8,"min_len":2,"max_len":32}.
const LimitsSettingKey = "tags.limits"

// SettingsReader reads settings rows by key; *dbgen.Queries satisfies it.
type SettingsReader interface {
	GetSettings(ctx context.Context, keys []string) ([]dbgen.GetSettingsRow, error)
}

// LoadLimits returns DefaultLimits with any overrides stored under
// LimitsSettingKey applied. Absent keys keep their default, so a partial row is
// safe; on error the defaults are returned alongside it.
func LoadLimits(ctx context.Context, r SettingsReader) (Limits, error) {
	rows, err := r.GetSettings(ctx, []string{LimitsSettingKey})
	if err != nil {
		return DefaultLimits, fmt.Errorf("load tag limits: %w", err)
	}
	limits := DefaultLimits
	for _, row := range rows {
		if err := json.Unmarshal(row.Value, &limits); err != nil {
			return DefaultLimits, fmt.Errorf("parse tag limits setting: %w", err)
		}
	}
	return limits, nil
}
