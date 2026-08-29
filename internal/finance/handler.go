// Package finance HTTP 层:F10 经费管理(契约 docs/api-contract.md §F10)。
package finance

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"labnexus/internal/middleware"
	"labnexus/internal/user"
)

// Handler 经费 HTTP handler
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes 注册 F10 路由(全部仅 admin/supervisor,service 层校验角色)
func (h *Handler) RegisterRoutes(r *gin.Engine, secret string) {
	authed := r.Group("/api")
	authed.Use(middleware.AuthRequired(secret))

	authed.POST("/finance/batches", h.CreateBatch)
	authed.GET("/finance/batches", h.ListBatches)
	authed.GET("/finance/batches/:id", h.GetBatch)
	authed.DELETE("/finance/batches/:id", h.DeleteBatch)
	authed.POST("/finance/batches/:id/complete", h.CompleteBatch)
	authed.POST("/finance/batches/:id/items", h.CreateItem)
	authed.POST("/finance/batches/:id/items/import-preview", h.ImportPreview)
	authed.POST("/finance/imports/:preview_id/confirm", h.ConfirmImport)
	authed.POST("/finance/items/:id/submit", h.Submit)
	authed.GET("/finance/ledger", h.Ledger)
	authed.POST("/finance/ledger/income", h.AddIncome)
	authed.POST("/finance/ledger/expense", h.AddExpense)
	authed.GET("/finance/participants", h.ListParticipants)
	authed.GET("/finance/participants/:id/bills", h.ParticipantBills)
}

func (h *Handler) userID(c *gin.Context) string {
	return c.GetString(middleware.ContextUserID)
}

// ---- 批次 ----

func (h *Handler) CreateBatch(c *gin.Context) {
	var req CreateBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	view, err := h.svc.CreateBatch(c.Request.Context(), h.userID(c), req)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"batch": view})
}

func (h *Handler) ListBatches(c *gin.Context) {
	views, err := h.svc.ListBatches(c.Request.Context(), h.userID(c))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"batches": views})
}

func (h *Handler) GetBatch(c *gin.Context) {
	view, err := h.svc.GetBatch(c.Request.Context(), h.userID(c), c.Param("id"))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"batch": view})
}

func (h *Handler) DeleteBatch(c *gin.Context) {
	if err := h.svc.DeleteBatch(c.Request.Context(), h.userID(c), c.Param("id")); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) CompleteBatch(c *gin.Context) {
	view, err := h.svc.CompleteBatch(c.Request.Context(), h.userID(c), c.Param("id"))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"batch": view})
}

// ---- 明细 ----

func (h *Handler) CreateItem(c *gin.Context) {
	var req CreateItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	view, err := h.svc.CreateItem(c.Request.Context(), h.userID(c), c.Param("id"), req)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"item": view})
}

func (h *Handler) ImportPreview(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "file field is required")
		return
	}
	f, err := fileHeader.Open()
	if err != nil {
		respondServiceError(c, err)
		return
	}
	defer f.Close()

	previewID, valid, errs, err := h.svc.ImportPreview(c.Request.Context(), h.userID(c), c.Param("id"), f)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	if valid == nil {
		valid = []ImportRow{}
	}
	if errs == nil {
		errs = []string{}
	}
	c.JSON(http.StatusOK, gin.H{
		"preview_id":  previewID,
		"valid_rows":  valid,
		"error_rows":  errs,
		"valid_count": len(valid),
		"error_count": len(errs),
	})
}

func (h *Handler) ConfirmImport(c *gin.Context) {
	batchID := c.Query("batch_id")
	if batchID == "" {
		respondError(c, http.StatusBadRequest, "VALIDATION", "batch_id query required")
		return
	}
	imported, skipped, err := h.svc.ConfirmImport(c.Request.Context(), h.userID(c), c.Param("preview_id"), batchID)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"imported_count": imported, "skipped_count": skipped})
}

// ---- 上交 ----

func (h *Handler) Submit(c *gin.Context) {
	var req SubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	sub, err := h.svc.Submit(c.Request.Context(), h.userID(c), c.Param("id"), req)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"submission": sub})
}

// ---- 资金池 ----

func (h *Handler) Ledger(c *gin.Context) {
	balance, views, err := h.svc.Ledger(c.Request.Context(), h.userID(c))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"balance": balance, "transactions": views})
}

func (h *Handler) AddIncome(c *gin.Context) {
	var req LedgerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	view, err := h.svc.AddIncome(c.Request.Context(), h.userID(c), req)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"transaction": view})
}

func (h *Handler) AddExpense(c *gin.Context) {
	var req LedgerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	view, err := h.svc.AddExpense(c.Request.Context(), h.userID(c), req)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"transaction": view})
}

// ---- 参与同学 ----

func (h *Handler) ListParticipants(c *gin.Context) {
	stats, err := h.svc.ListParticipants(c.Request.Context(), h.userID(c))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"participants": stats})
}

func (h *Handler) ParticipantBills(c *gin.Context) {
	p, bills, err := h.svc.ParticipantBills(c.Request.Context(), h.userID(c), c.Param("id"))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"participant": p, "bills": bills})
}

// ---- 错误映射 ----

func respondServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrBatchNotFound), errors.Is(err, ErrItemNotFound),
		errors.Is(err, ErrParticipantNotFound), errors.Is(err, ErrPreviewNotFound):
		respondError(c, http.StatusNotFound, "NOT_FOUND", err.Error())
	case errors.Is(err, ErrForbidden), errors.Is(err, user.ErrNotFound):
		respondError(c, http.StatusForbidden, "FORBIDDEN", err.Error())
	case errors.Is(err, ErrBatchNameEmpty), errors.Is(err, ErrItemFields),
		errors.Is(err, ErrInvalidDate), errors.Is(err, ErrInvalidAmount),
		errors.Is(err, ErrOverSubmit), errors.Is(err, ErrBatchDone),
		errors.Is(err, ErrBatchNotDone), errors.Is(err, ErrCannotDelete),
		errors.Is(err, ErrInvalidExcel), errors.Is(err, ErrTooManyRows):
		respondError(c, http.StatusBadRequest, "VALIDATION", err.Error())
	default:
		respondError(c, http.StatusInternalServerError, "INTERNAL", "internal error")
	}
}

func respondError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{"code": code, "message": message},
	})
}
