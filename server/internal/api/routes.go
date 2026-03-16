package api

import (
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
	"wayback/internal/config"
	"wayback/web"
)

// SetupRoutes 设置路由
func SetupRoutes(r *gin.Engine, handler *Handler, authCfg *config.AuthConfig) {
	// CORS 中间件 - 仅允许本地来源，防止 CSRF 攻击
	r.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		// 仅允许本地来源（localhost, 127.0.0.1, file://)
		if origin == "http://localhost:8080" || origin == "http://127.0.0.1:8080" ||
		   origin == "null" || origin == "" { // null = file:// protocol
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, PUT, GET, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})

	// robots.txt（在认证之前，确保爬虫和工具可以访问）
	webFS, _ := fs.Sub(web.StaticFiles, ".")
	serveFile := func(name string) gin.HandlerFunc {
		return func(c *gin.Context) {
			c.FileFromFS(name, http.FS(webFS))
		}
	}
	r.GET("/robots.txt", serveFile("robots.txt"))

	// Basic Auth 中间件（如果启用）
	if authCfg.Enabled() {
		accounts := gin.Accounts{
			config.AuthUsername: authCfg.Password,
		}
		r.Use(gin.BasicAuth(accounts))
	}

	// Web UI (embedded)
	r.GET("/", serveFile("index.html"))
	r.GET("/index.html", serveFile("index.html"))
	r.GET("/timeline", serveFile("timeline.html"))
	r.GET("/timeline.html", serveFile("timeline.html"))
	r.GET("/logs", serveFile("logs.html"))
	r.GET("/logs.html", serveFile("logs.html"))
	r.GET("/favicon.ico", serveFile("favicon.ico"))

	api := r.Group("/api")
	{
		api.POST("/archive", handler.ArchivePage)
		api.PUT("/archive/:id", handler.UpdatePage)
		api.GET("/pages", handler.ListPages)
		api.GET("/pages/timeline", handler.GetPageTimeline)
		api.GET("/pages/:id", handler.GetPage)
		api.GET("/pages/:id/content", handler.GetPageContent)
		api.DELETE("/pages/:id", handler.DeletePage)
		api.GET("/search", handler.SearchPages)
		api.GET("/logs", handler.ListLogs)
		api.GET("/logs/latest", handler.GetLatestLog)
		api.GET("/logs/:filename", handler.GetLog)
	}

	// 查看归档页面
	r.GET("/view/:id", handler.ViewPage)
	r.GET("/archive/:page_id/:timestamp/*resource_path", handler.ProxyResource)
	r.HEAD("/archive/:page_id/:timestamp/*resource_path", handler.ProxyResource)

	// 直接资源访问（CSS 中引用的资源路径格式: /archive/resources/xx/yy/hash.ext）
	r.GET("/archive/resources/*filepath", handler.ServeLocalResource)
	r.HEAD("/archive/resources/*filepath", handler.ServeLocalResource)
}
