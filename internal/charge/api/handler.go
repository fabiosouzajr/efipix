package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	chargeapp "github.com/efipix/pix/internal/charge/app"
	"github.com/efipix/pix/internal/charge/domain"
	"github.com/efipix/pix/internal/platform/httpx"
	"github.com/efipix/pix/internal/platform/idempotency"
	"github.com/efipix/pix/internal/platform/money"
	"github.com/efipix/pix/internal/platform/tenantctx"
)

type Handler struct {
	uc   *chargeapp.CreateImmediateCharge
	repo chargeapp.ChargeRepository
}

func NewHandler(uc *chargeapp.CreateImmediateCharge, repo chargeapp.ChargeRepository) *Handler {
	return &Handler{uc: uc, repo: repo}
}

// RegisterRoutes wires the charge endpoints. POST /charges runs behind
// idempotency.Middleware (File 04): required Idempotency-Key, replay on
// retry. GET /charges/:id is a plain read with no idempotency protocol.
func RegisterRoutes(rg gin.IRoutes, h *Handler, idem idempotency.Store) {
	rg.POST("/charges", idempotency.Middleware(idem), h.create)
	rg.GET("/charges/:id", h.get)
}

func (h *Handler) create(c *gin.Context) {
	ctx := c.Request.Context()
	res, _ := tenantctx.From(ctx)

	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	status, body := h.process(ctx, res, raw)
	c.Data(status, "application/json; charset=utf-8", body)
}

// process runs validation + use case, returning the final status and JSON body.
func (h *Handler) process(ctx context.Context, res *tenantctx.Resolved, raw []byte) (int, []byte) {
	var req createChargeRequest
	if err := json.Unmarshal(raw, &req); err != nil || req.Amount == "" {
		return http.StatusUnprocessableEntity, mustJSON(gin.H{"error": "invalid request body"})
	}
	amount, err := money.ParseString(req.Amount)
	if err != nil || amount <= 0 {
		return http.StatusUnprocessableEntity, mustJSON(gin.H{"error": "invalid amount"})
	}
	charge, err := h.uc.Execute(ctx, chargeapp.CreateImmediateChargeCmd{
		TenantID: res.TenantID, PaymentProviderID: res.ProviderID, PixKey: res.PixKey,
		Amount: amount, Description: req.Description, ExpirationSeconds: req.ExpirationSeconds,
		Payer: domain.Payer{
			Doc: req.Payer.Doc, DocType: req.Payer.DocType, Name: req.Payer.Name,
			Email: req.Payer.Email, Phone: req.Payer.Phone,
		},
		ExternalReference: req.ExternalReference,
	})
	if err != nil {
		return httpx.StatusFor(err), mustJSON(gin.H{"error": err.Error()})
	}
	return http.StatusCreated, mustJSON(toResponse(charge))
}

func (h *Handler) get(c *gin.Context) {
	ctx := c.Request.Context()
	res, _ := tenantctx.From(ctx)
	charge, err := h.repo.FindByID(ctx, res.TenantID, c.Param("id"))
	if err != nil {
		c.JSON(httpx.StatusFor(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toResponse(charge))
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
