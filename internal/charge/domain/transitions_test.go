package domain

import (
	"testing"

	"github.com/stretchr/testify/require"

	apperrs "github.com/efipix/pix/internal/platform/errors"
)

func TestMarkActiveFromCreated(t *testing.T) {
	c, _ := NewImmediate(validParams())
	err := c.MarkActive("loc1", "imgb64", "000201...")
	require.NoError(t, err)
	require.Equal(t, StatusActive, c.Status)
	require.Equal(t, "loc1", c.LocationID)
	require.Equal(t, "000201...", c.PixPayload)
	require.Equal(t, "activated", c.Events[len(c.Events)-1].EventType)
}

func TestMarkActiveIllegalFromFailed(t *testing.T) {
	c, _ := NewImmediate(validParams())
	require.NoError(t, c.MarkFailed("boom"))
	err := c.MarkActive("l", "i", "p")
	require.Equal(t, apperrs.KindConflict, apperrs.KindOf(err))
}

func TestMarkFailedFromCreated(t *testing.T) {
	c, _ := NewImmediate(validParams())
	require.NoError(t, c.MarkFailed("provider down"))
	require.Equal(t, StatusFailed, c.Status)
	require.Equal(t, "failed", c.Events[len(c.Events)-1].EventType)
}
