package publish

import (
	"context"
	"testing"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/tags"
	"github.com/stretchr/testify/require"
)

// mapReader serves canned settings by key; absent keys are simply not returned,
// like the real GetSettings query.
type mapReader map[string][]byte

func (m mapReader) GetSettings(_ context.Context, keys []string) ([]dbgen.GetSettingsRow, error) {
	var rows []dbgen.GetSettingsRow
	for _, k := range keys {
		if v, ok := m[k]; ok {
			rows = append(rows, dbgen.GetSettingsRow{Key: k, Value: v})
		}
	}
	return rows, nil
}

func TestLoadLimits_Defaults(t *testing.T) {
	ml, tl, err := LoadLimits(context.Background(), mapReader{})
	require.NoError(t, err)
	require.Equal(t, DefaultMetadataLimits, ml)
	require.Equal(t, tags.DefaultLimits, tl)
}

func TestLoadLimits_Override(t *testing.T) {
	ml, tl, err := LoadLimits(context.Background(), mapReader{
		TitleLimitsSettingKey:       []byte(`{"min":5,"max":90}`),
		DescriptionLimitsSettingKey: []byte(`{"min":20,"max":200}`),
		tags.LimitsSettingKey:       []byte(`{"max_tags":3}`),
	})
	require.NoError(t, err)
	require.Equal(t, Bounds{Min: 5, Max: 90}, ml.Title)
	require.Equal(t, Bounds{Min: 20, Max: 200}, ml.Description)
	require.Equal(t, 3, tl.MaxTags)
	require.Equal(t, tags.DefaultLimits.MinLen, tl.MinLen)
}

func TestLoadLimits_PartialOverride(t *testing.T) {
	ml, _, err := LoadLimits(context.Background(), mapReader{
		TitleLimitsSettingKey: []byte(`{"min":5,"max":90}`),
	})
	require.NoError(t, err)
	require.Equal(t, Bounds{Min: 5, Max: 90}, ml.Title)
	require.Equal(t, DefaultMetadataLimits.Description, ml.Description)
}

func TestLoadLimits_BadJSONReturnsDefaults(t *testing.T) {
	ml, tl, err := LoadLimits(context.Background(), mapReader{
		TitleLimitsSettingKey: []byte(`not json`),
	})
	require.Error(t, err)
	require.Equal(t, DefaultMetadataLimits, ml)
	require.Equal(t, tags.DefaultLimits, tl)
}
