package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	apperrs "github.com/efipix/pix/internal/platform/errors"
	"github.com/efipix/pix/internal/platform/tenantctx"
	tapp "github.com/efipix/pix/internal/tenant/app"
)

func Middleware(resolver *tapp.Resolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader("X-Api-Key")
		if raw == "" {
			if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "ApiKey ") {
				raw = strings.TrimPrefix(h, "ApiKey ")
			}
		}
		if raw == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing api key"})
			return
		}
		res, err := resolver.Resolve(c.Request.Context(), raw, c.GetHeader("X-Provider-Id"))
		if err != nil {
			switch apperrs.KindOf(err) {
			case apperrs.KindUnauthorized:
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
			case apperrs.KindValidation:
				c.AbortWithStatusJSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "resolution failed"})
			}
			return
		}
		c.Request = c.Request.WithContext(tenantctx.With(c.Request.Context(), res))
		c.Next()
	}
}
