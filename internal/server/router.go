// Package server wires the routes. The URL space is the one the Next.js app
// exposed under /api, so the dashboard's client code does not change.
package server

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"cms-backend/internal/apierr"
	"cms-backend/internal/auth"
	"cms-backend/internal/config"
	"cms-backend/internal/db"
	"cms-backend/internal/handlers"
	"cms-backend/internal/media"
)

func New(cfg *config.Config, database *db.DB) *gin.Engine {
	if cfg.Production {
		gin.SetMode(gin.ReleaseMode)
	}

	authSvc := auth.NewService(cfg, database)
	h := &handlers.Handler{
		Cfg: cfg, DB: database, Auth: authSvc, Media: media.New(cfg), Throttle: auth.NewThrottle(),
	}

	r := gin.New()
	r.Use(gin.Recovery(), gin.Logger())
	// This service sits behind the Next.js server on loopback. Trust nothing
	// about forwarding headers beyond what auth.ClientIP reads explicitly.
	_ = r.SetTrustedProxies(nil)
	r.HandleMethodNotAllowed = true
	r.NoRoute(func(c *gin.Context) { apierr.Fail(c, apierr.NotFound("No such route.")) })
	r.NoMethod(func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusMethodNotAllowed, gin.H{"ok": false, "error": gin.H{
			"message": "Method not allowed.", "code": "method_not_allowed"}})
	})

	// Never cache an admin read, or two editors see different truths.
	r.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Next()
	})

	r.GET("/healthz", func(c *gin.Context) {
		if !database.Ping(c.Request.Context()) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"ok": false})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	api := r.Group("/api")

	// auth
	api.POST("/auth/login", handlersWrap(h.Login))
	api.POST("/auth/logout", handlersWrap(h.Logout))
	api.GET("/auth/me", handlersWrap(h.Me))

	// public, bearer-token protected
	pub := api.Group("/public", authSvc.RequirePublicToken())
	pub.GET("/pages/:slug", handlersWrap(h.PublicPage))
	pub.GET("/site", handlersWrap(h.PublicSite))

	// admin
	admin := api.Group("/admin", authSvc.RequireUser())

	admin.GET("/overview", handlersWrap(h.Overview))

	admin.GET("/sections", handlersWrap(h.Sections))
	admin.POST("/sections/reorder", handlersWrap(h.SectionsReorder))
	admin.PATCH("/sections/:id", handlersWrap(h.SectionSetActive))

	// One set of section and collection routes, mounted twice: under /home for
	// the home page (the page is implied) and under /pages/:page for every
	// page, About, Leadership and Sector included. pageOf() reads the path
	// parameter and falls back to "home", so the handlers are shared as they are.
	mountSections := func(g *gin.RouterGroup) {
		g.GET("/:section", handlersWrap(h.SectionGet))
		g.PUT("/:section", handlersWrap(h.SectionPut))
		g.GET("/:section/:collection", handlersWrap(h.CollectionList))
		g.POST("/:section/:collection", handlersWrap(h.CollectionCreate))
		g.POST("/:section/:collection/reorder", handlersWrap(h.CollectionReorder))
		g.GET("/:section/:collection/:id", handlersWrap(h.CollectionGet))
		g.PUT("/:section/:collection/:id", handlersWrap(h.CollectionUpdate))
		g.DELETE("/:section/:collection/:id", handlersWrap(h.CollectionDelete))
		g.PATCH("/:section/:collection/:id/active", handlersWrap(h.CollectionSetActive))
		g.POST("/:section/:collection/:id/lead", handlersWrap(h.CollectionSetLead))
	}
	mountSections(admin.Group("/home"))
	mountSections(admin.Group("/pages/:page"))

	media := admin.Group("/media")
	media.GET("", handlersWrap(h.MediaList))
	media.POST("", handlersWrap(h.MediaUpload))
	media.GET("/:id", handlersWrap(h.MediaGet))
	media.PUT("/:id", handlersWrap(h.MediaUpdate))
	media.DELETE("/:id", handlersWrap(h.MediaDelete))

	nav := admin.Group("/navigation")
	nav.GET("/menus", handlersWrap(h.NavMenus))
	nav.GET("/items/:id", handlersWrap(h.NavGet))
	nav.PUT("/items/:id", handlersWrap(h.NavUpdate))
	nav.DELETE("/items/:id", handlersWrap(h.NavDelete))
	nav.PATCH("/items/:id/active", handlersWrap(h.NavSetActive))
	nav.PATCH("/items/:id/parent", handlersWrap(h.NavMove))
	nav.GET("/:menuKey", handlersWrap(h.NavList))
	nav.POST("/:menuKey", handlersWrap(h.NavCreate))
	nav.POST("/:menuKey/reorder", handlersWrap(h.NavReorder))

	admin.GET("/seo/:slug", handlersWrap(h.SEOGet))
	admin.PUT("/seo/:slug", handlersWrap(h.SEOPut))

	admin.GET("/settings", handlersWrap(h.SettingsGet))
	admin.PUT("/settings", authSvc.RequireAdmin(), handlersWrap(h.SettingsPut))

	if cfg.DevBypassAuth {
		log.Println("WARNING: CMS_DEV_BYPASS_AUTH is on; every request is treated as an administrator.")
	}
	return r
}

func handlersWrap(fn func(c *gin.Context) error) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := fn(c); err != nil {
			apierr.Fail(c, err)
		}
	}
}
