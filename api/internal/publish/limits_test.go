package publish

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// mapGetter serves canned settings values keyed by settings key; an absent key
// returns pgx.ErrNoRows, exactly like the real GetSetting query.
type mapGetter map[string][]byte

func (m mapGetter) GetSetting(_ context.Context, key string) ([]byte, error) {
	if v, ok := m[key]; ok {
		return v, nil
	}
	return nil, pgx.ErrNoRows
}

// AC-10.4 baseline: with nothing configured, the safe defaults from 00-overview §6.
func TestLoadMetadataLimits_Defaults(t *testing.T) {
	got, err := LoadMetadataLimits(context.Background(), mapGetter{})
	require.NoError(t, err)
	require.Equal(t, DefaultMetadataLimits, got)
}

// AC-10.4: a stored title/description limit overrides the default and would be
// honored on the next publish (read fresh per request).
func TestLoadMetadataLimits_Override(t *testing.T) {
	got, err := LoadMetadataLimits(context.Background(), mapGetter{
		TitleLimitsSettingKey:       []byte(`{"min":5,"max":90}`),
		DescriptionLimitsSettingKey: []byte(`{"min":20,"max":200}`),
	})
	require.NoError(t, err)
	require.Equal(t, Bounds{Min: 5, Max: 90}, got.Title)
	require.Equal(t, Bounds{Min: 20, Max: 200}, got.Description)
}

// A partial config overrides only the field present; the other keeps its default.
func TestLoadMetadataLimits_PartialOverride(t *testing.T) {
	got, err := LoadMetadataLimits(context.Background(), mapGetter{
		TitleLimitsSettingKey: []byte(`{"min":5,"max":90}`),
	})
	require.NoError(t, err)
	require.Equal(t, Bounds{Min: 5, Max: 90}, got.Title)
	require.Equal(t, DefaultMetadataLimits.Description, got.Description)
}

// Malformed settings JSON surfaces an error and returns the safe defaults rather
// than a half-parsed limit set that could weaken the gate.
func TestLoadMetadataLimits_BadJSON(t *testing.T) {
	got, err := LoadMetadataLimits(context.Background(), mapGetter{
		TitleLimitsSettingKey: []byte(`not json`),
	})
	require.Error(t, err)
	require.Equal(t, DefaultMetadataLimits, got)
}
