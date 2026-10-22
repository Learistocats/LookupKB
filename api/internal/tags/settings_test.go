package tags

import (
	"context"
	"errors"
	"testing"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/stretchr/testify/require"
)

type fakeGetter struct {
	value []byte
	err   error
}

func (f fakeGetter) GetSettings(context.Context, []string) ([]dbgen.GetSettingsRow, error) {
	if f.err != nil || f.value == nil {
		return nil, f.err
	}
	return []dbgen.GetSettingsRow{{Key: LimitsSettingKey, Value: f.value}}, nil
}

// TestLoadLimits_DefaultsWhenUnset: a missing settings row yields DefaultLimits.
func TestLoadLimits_DefaultsWhenUnset(t *testing.T) {
	got, err := LoadLimits(context.Background(), fakeGetter{})
	require.NoError(t, err)
	require.Equal(t, DefaultLimits, got)
}

// TestLoadLimits_Override: stored JSON overrides the defaults.
func TestLoadLimits_Override(t *testing.T) {
	got, err := LoadLimits(context.Background(),
		fakeGetter{value: []byte(`{"min_tags":2,"max_tags":10,"min_len":3,"max_len":40}`)})
	require.NoError(t, err)
	require.Equal(t, Limits{MinTags: 2, MaxTags: 10, MinLen: 3, MaxLen: 40}, got)
}

// TestLoadLimits_PartialOverride: absent keys keep their default value.
func TestLoadLimits_PartialOverride(t *testing.T) {
	got, err := LoadLimits(context.Background(), fakeGetter{value: []byte(`{"max_tags":5}`)})
	require.NoError(t, err)
	require.Equal(t, Limits{MinTags: 1, MaxTags: 5, MinLen: 2, MaxLen: 32}, got)
}

// TestLoadLimits_BadJSONFallsBack: malformed settings return defaults + an error,
// never a half-parsed limit set.
func TestLoadLimits_BadJSON(t *testing.T) {
	got, err := LoadLimits(context.Background(), fakeGetter{value: []byte(`not json`)})
	require.Error(t, err)
	require.Equal(t, DefaultLimits, got)
}

// TestLoadLimits_QueryErrorFallsBack: a real DB error surfaces but still returns
// the safe defaults so a transient settings read can't disable validation.
func TestLoadLimits_QueryError(t *testing.T) {
	got, err := LoadLimits(context.Background(), fakeGetter{err: errors.New("boom")})
	require.Error(t, err)
	require.Equal(t, DefaultLimits, got)
}
