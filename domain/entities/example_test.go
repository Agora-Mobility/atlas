package entities_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
)

func TestNewExample(t *testing.T) {
	tests := []struct {
		name       string
		exName     string
		lat, lng   float64
		wantFields []string
	}{
		{name: "valid", exName: "Talatona", lat: -8.9, lng: 13.1},
		{name: "blank name", exName: "  ", lat: 0, lng: 0, wantFields: []string{"name"}},
		{name: "name too long", exName: strings.Repeat("a", 121), wantFields: []string{"name"}},
		{name: "bad coordinates", exName: "x", lat: -91, lng: 181, wantFields: []string{"latitude", "longitude"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := entities.NewExample(tt.exName, "", tt.lat, tt.lng)

			if len(tt.wantFields) == 0 {
				require.NoError(t, err)
				assert.NotEqual(t, [16]byte{}, [16]byte(e.ID))
				assert.Equal(t, strings.TrimSpace(tt.exName), e.Name)
				return
			}
			appErr := customerrors.As(err)
			require.Equal(t, customerrors.CodeValidation, appErr.Code)
			for _, f := range tt.wantFields {
				assert.Contains(t, appErr.Details, f)
			}
		})
	}
}

func TestExampleUpdate_KeepsEntityUnchangedOnError(t *testing.T) {
	e, err := entities.NewExample("Original", "desc", 0, 0)
	require.NoError(t, err)

	require.Error(t, e.Update("", "new"))
	assert.Equal(t, "Original", e.Name)

	require.NoError(t, e.Update("Renamed", "new"))
	assert.Equal(t, "Renamed", e.Name)
	assert.False(t, e.UpdatedAt.Before(e.CreatedAt))
}
