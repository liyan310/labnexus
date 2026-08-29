// Package tag HTTP 层 + 业务:F5 标签创建与列表(契约 docs/api-contract.md §F5)。
// 标签内容聚合端点 GET /tags/:id/contents 由 document 模块注册(依赖倒置,防循环)。
package tag

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"labnexus/internal/middleware"
)

// 哨兵错误
var (
	ErrTagNotFound  = errors.New("tag not found")
	ErrTagNameEmpty = errors.New("tag name is empty")
	ErrTagExists    = errors.New("tag already exists")
)

// Service 标签业务逻辑
type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// CreateTag 创建标签(name 唯一)。
func (s *Service) CreateTag(ctx context.Context, name, color string) (*Tag, error) {
	if strings.TrimSpace(name) == "" {
		return nil, ErrTagNameEmpty
	}
	if _, err := s.repo.GetByName(ctx, name); err == nil {
		return nil, ErrTagExists
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	t := NewTag(name, color)
	if err := s.repo.Create(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// ListTags 全部标签。
func (s *Service) ListTags(ctx context.Context) ([]*Tag, error) {
	return s.repo.List(ctx)
}

// Handler 标签 HTTP handler
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes 注册 F5 路由(contents 端点由 document 模块注册)
func (h *Handler) RegisterRoutes(r *gin.Engine, secret string) {
	authed := r.Group("/api")
	authed.Use(middleware.AuthRequired(secret))
	authed.GET("/tags", h.List)
	authed.POST("/tags", h.Create)
}

type createTagReq struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

func (h *Handler) Create(c *gin.Context) {
	var req createTagReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	t, err := h.svc.CreateTag(c.Request.Context(), req.Name, req.Color)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"tag": t})
}

func (h *Handler) List(c *gin.Context) {
	tags, err := h.svc.ListTags(c.Request.Context())
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tags": tags})
}

func respondServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrTagExists):
		respondError(c, http.StatusConflict, "CONFLICT", err.Error())
	case errors.Is(err, ErrTagNameEmpty), errors.Is(err, ErrTagNotFound):
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
