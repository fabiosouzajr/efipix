package httpx

import (
	"net/http"

	apperrs "github.com/efipix/pix/internal/platform/errors"
)

func StatusFor(err error) int {
	switch apperrs.KindOf(err) {
	case apperrs.KindNotFound:
		return http.StatusNotFound
	case apperrs.KindConflict:
		return http.StatusConflict
	case apperrs.KindValidation:
		return http.StatusUnprocessableEntity
	case apperrs.KindUnauthorized:
		return http.StatusUnauthorized
	case apperrs.KindProvider:
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}
