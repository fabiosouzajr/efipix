package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHashAPIKey(t *testing.T) {
	require.Len(t, HashAPIKey("pk_dev_secret"), 64) // sha256 hex = 64 chars
	require.Equal(t, HashAPIKey("a"), HashAPIKey("a"))
	require.NotEqual(t, HashAPIKey("a"), HashAPIKey("b"))
}
