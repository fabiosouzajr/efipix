package httpx

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	apperrs "github.com/efipix/pix/internal/platform/errors"
)

func TestStatusForKind(t *testing.T) {
	require.Equal(t, http.StatusNotFound, StatusFor(apperrs.New(apperrs.KindNotFound, "x")))
	require.Equal(t, http.StatusConflict, StatusFor(apperrs.New(apperrs.KindConflict, "x")))
	require.Equal(t, http.StatusUnprocessableEntity, StatusFor(apperrs.New(apperrs.KindValidation, "x")))
	require.Equal(t, http.StatusUnauthorized, StatusFor(apperrs.New(apperrs.KindUnauthorized, "x")))
	require.Equal(t, http.StatusBadGateway, StatusFor(apperrs.New(apperrs.KindProvider, "x")))
	require.Equal(t, http.StatusInternalServerError, StatusFor(apperrs.New(apperrs.KindUnknown, "x")))
}
