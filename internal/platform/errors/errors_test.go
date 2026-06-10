package errors

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKindOf(t *testing.T) {
	e := New(KindNotFound, "missing")
	require.Equal(t, KindNotFound, KindOf(e))
	require.Equal(t, "missing", e.Error())

	wrapped := Wrap(KindConflict, "dup", errors.New("pg: unique"))
	require.Equal(t, KindConflict, KindOf(wrapped))
	require.Contains(t, wrapped.Error(), "pg: unique")

	require.Equal(t, KindUnknown, KindOf(errors.New("plain")))
}
