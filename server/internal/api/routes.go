package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"wayback/internal/config"
)

// SetupRoutes 设置路由
func SetupRoutes(r *gin.Engine, handler *Handler, authCfg *config.AuthConfig) {
	// CORS 中间件
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, PUT, GET, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})

	// robots.txt（在认证之前，确保爬虫和工具可以访问）
	r.GET("/robots.txt", func(c *gin.Context) {
		robotsTxt := `User-agent: Googlebot
Disallow: /

User-agent: Bingbot
Disallow: /

User-agent: Slurp
Disallow: /

User-agent: DuckDuckBot
Disallow: /

User-agent: Baiduspider
Disallow: /

User-agent: YandexBot
Disallow: /

User-agent: Sogou
Disallow: /

User-agent: Bytespider
Disallow: /

User-agent: GPTBot
Disallow: /

User-agent: CCBot
Disallow: /

User-agent: anthropic-ai
Disallow: /

User-agent: ClaudeBot
Disallow: /

User-agent: *
Allow: /
`
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(robotsTxt))
	})

	// Basic Auth 中间件（如果启用）
	if authCfg.Enabled() {
		accounts := gin.Accounts{
			config.AuthUsername: authCfg.Password,
		}
		r.Use(gin.BasicAuth(accounts))
	}

	// Web UI
	r.StaticFile("/", "./web/index.html")
	r.StaticFile("/index.html", "./web/index.html")
	r.StaticFile("/test.html", "./web/test.html")
	r.StaticFile("/timeline", "./web/timeline.html")
	r.StaticFile("/timeline.html", "./web/timeline.html")
	r.StaticFile("/logs", "./web/logs.html")
	r.StaticFile("/logs.html", "./web/logs.html")

	// favicon（避免 404）
	r.GET("/favicon.ico", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

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
