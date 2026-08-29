// Package app 应用装配:连接数据库、迁移、依赖注入、注册全部路由。
// 生产入口(main)与集成测试共用本装配,保证"测试即生产"(test/integration)。
package app

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"labnexus/internal/auth"
	"labnexus/internal/cache"
	"labnexus/internal/config"
	"labnexus/internal/database"
	"labnexus/internal/document"
	"labnexus/internal/finance"
	"labnexus/internal/project"
	"labnexus/internal/resource"
	"labnexus/internal/space"
	"labnexus/internal/tag"
	"labnexus/internal/user"
)

// Build 完成全部装配并返回 gin 路由。
func Build(cfg *config.Config) (*gin.Engine, error) {
	db, err := database.New(cfg)
	if err != nil {
		return nil, err
	}

	// 开发期 AutoMigrate;正式部署前切换 goose 版本化迁移(schema 权威定义 docs/schema.sql)
	if err := db.AutoMigrate(
		&user.User{}, &user.InviteCode{},
		&space.Space{}, &space.Folder{},
		&document.Document{}, &document.DocumentTag{}, &document.Comment{}, &document.Reaction{},
		&tag.Tag{},
		&resource.Resource{}, &resource.ResourceTag{},
		&project.Project{}, &project.ProjectMember{}, &project.Milestone{}, &project.Task{}, &project.TaskLink{},
		&finance.TurnoverBatch{}, &finance.Participant{}, &finance.TurnoverItem{},
		&finance.TurnoverSubmission{}, &finance.Account{}, &finance.Transaction{},
	); err != nil {
		return nil, err
	}
	slog.Info("database migrated")

	// 依赖装配(规范 §3:构造函数注入)
	store := cache.NewRedisStore(cfg.RedisAddr)
	users := user.NewGormRepository(db)
	invites := user.NewGormInviteRepository(db)
	spaces := space.NewGormRepository(db)
	folders := space.NewGormFolderRepository(db)
	authSvc := auth.NewService(users, invites, spaces, store, cfg).
		WithTxRunner(database.GormTxRunner(db))
	authHandler := auth.NewHandler(authSvc)
	spaceSvc := space.NewService(spaces, folders).
		WithDocCounter(document.NewGormRepository(db).CountByFolder)
	spaceHandler := space.NewHandler(spaceSvc)
	tagRepo := tag.NewGormRepository(db)
	tagSvc := tag.NewService(tagRepo)
	tagHandler := tag.NewHandler(tagSvc)
	docSvc := document.NewService(
		document.NewGormRepository(db),
		document.NewGormCommentRepository(db),
		document.NewGormReactionRepository(db),
		tagRepo, users, spaces, folders,
	).WithTxRunner(database.GormTxRunner(db)).
		WithSearchProviders(
			func(ctx context.Context, q string, limit int) ([]*resource.Resource, error) {
				list, _, err := resource.NewGormRepository(db).List(ctx, resource.ListFilter{
					Keyword: q, Page: 1, PageSize: limit,
				})
				return list, err
			},
			func(ctx context.Context, q string, limit int) ([]*project.Task, error) {
				return project.NewGormRepository(db).SearchTasks(ctx, q, limit)
			},
		)
	docHandler := document.NewHandler(docSvc)

	// 阶段 2:F7 资源库 + F8 文献元数据
	fileStore, err := resource.NewLocalFileStore("data/uploads")
	if err != nil {
		return nil, err
	}
	resSvc := resource.NewService(
		resource.NewGormRepository(db),
		tagRepo, fileStore, users,
	).WithTxRunner(database.GormTxRunner(db))
	resHandler := resource.NewHandler(resSvc)
	docSvc.WithResourceByTag(resSvc.ListByTag)

	// 阶段 2:F9 项目/任务
	projectSvc := project.NewService(project.NewGormRepository(db), users).
		WithTxRunner(database.GormTxRunner(db))
	projectHandler := project.NewHandler(projectSvc)

	// 阶段 3:F10 经费管理(仅 admin/supervisor)
	financeSvc := finance.NewService(finance.NewGormRepository(db), users).
		WithTxRunner(database.GormTxRunner(db)).
		WithPreviewStore(finance.NewCachePreviewStore(store))
	financeHandler := finance.NewHandler(financeSvc)

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	// 健康检查
	r.GET("/api/health", func(c *gin.Context) {
		sqlDB, err := db.DB()
		dbOK := err == nil && sqlDB.Ping() == nil
		status := "ok"
		if !dbOK {
			status = "degraded"
		}
		c.JSON(http.StatusOK, gin.H{
			"status":  status,
			"service": "labnexus",
			"db":      dbOK,
		})
	})

	// 路由注册(契约 docs/api-contract.md)
	authHandler.RegisterRoutes(r, cfg.JWTSecret)
	spaceHandler.RegisterRoutes(r, cfg.JWTSecret)
	docHandler.RegisterRoutes(r, cfg.JWTSecret)
	tagHandler.RegisterRoutes(r, cfg.JWTSecret)
	resHandler.RegisterRoutes(r, cfg.JWTSecret)
	projectHandler.RegisterRoutes(r, cfg.JWTSecret)
	financeHandler.RegisterRoutes(r, cfg.JWTSecret)

	// 前端外壳(阶段 1 验证:纯 HTML/JS,由后端托管)
	// 用 NoRoute 提供静态文件,避免 catch-all 与 /api 路由冲突;
	// 未注册的 /api/* 路径统一返回 JSON 404(契约 §通用约定)。
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"code": "NOT_FOUND", "message": "not found"},
			})
			return
		}
		http.FileServer(http.Dir(cfg.WebDir)).ServeHTTP(c.Writer, c.Request)
	})

	return r, nil
}
