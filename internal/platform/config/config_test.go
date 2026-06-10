package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadDefaultsAndRequired(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://app:pw@localhost:5432/pix")
	c, err := Load()
	require.NoError(t, err)
	require.Equal(t, "postgres://app:pw@localhost:5432/pix", c.DatabaseURL)
	require.Equal(t, "8080", c.HTTPPort) // default
	require.Equal(t, "info", c.LogLevel) // default
}

func TestLoadMissingRequired(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	_, err := Load()
	require.Error(t, err)
}
