package idempotency

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/efipix/pix/internal/platform/tenantctx"
)

// bufferingWriter tees everything written to the real ResponseWriter into an
// in-memory buffer, so Middleware can persist the handler's response via
// Store.SaveResult after c.Next() returns.
type bufferingWriter struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *bufferingWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *bufferingWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

// Middleware enforces the idempotent-replay protocol on the routes it's
// registered for: requires "Idempotency-Key" (400 if absent), fingerprints
// the raw request body (sha256), and reserves/replays/persists via store.
//
// Behavior by Reservation.State:
//   - "replay":   writes back the stored status/body verbatim, handler does not run.
//   - "conflict": 422 {"error": "idempotency key reused with different body"}.
//   - "inflight": 409 {"error": "request in progress"}.
//   - "new":      handler runs; its response is captured and persisted via SaveResult.
//
// Must run after tenant resolution (tenantctx.With must already be in ctx) and
// before any handler reading c.Request.Body — the body is restored via
// io.NopCloser before c.Next() so the handler can read it normally.
func Middleware(store Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		key := c.GetHeader("Idempotency-Key")
		if key == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Idempotency-Key header required"})
			return
		}

		res, ok := tenantctx.From(ctx)
		if !ok {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "idempotency"})
			return
		}

		raw, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
		sum := sha256.Sum256(raw)
		fp := hex.EncodeToString(sum[:])

		rv, err := store.Reserve(ctx, res.TenantID, key, fp)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "idempotency"})
			return
		}

		switch rv.State {
		case "replay":
			c.Data(rv.StoredStatus, "application/json; charset=utf-8", rv.StoredBody)
			c.Abort()
			return
		case "conflict":
			c.AbortWithStatusJSON(http.StatusUnprocessableEntity, gin.H{"error": "idempotency key reused with different body"})
			return
		case "inflight":
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "request in progress"})
			return
		}

		bw := &bufferingWriter{ResponseWriter: c.Writer}
		c.Writer = bw
		c.Next()

		if err := store.SaveResult(ctx, res.TenantID, key, c.Writer.Status(), bw.body.Bytes()); err != nil {
			slog.ErrorContext(ctx, "idempotency: save result failed",
				"tenant_id", res.TenantID, "key", key, "error", err)
		}
	}
}
