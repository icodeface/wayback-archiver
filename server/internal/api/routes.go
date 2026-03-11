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

	// favicon（避免 404）
	r.GET("/favicon.ico", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	api := r.Group("/api")
	{
		api.POST("/archive", handler.ArchivePage)
		api.PUT("/archive/:id", handler.UpdatePage)
		api.GET("/pages", handler.ListPages)
		api.GET("/pages/:id", handler.GetPage)
		api.DELETE("/pages/:id", handler.DeletePage)
		api.GET("/search", handler.SearchPages)
	}

	// 查看归档页面
	r.GET("/view/:id", handler.ViewPage)
	r.GET("/archive/:page_id/:timestamp/*resource_path", handler.ProxyResource)

	// 直接资源访问（CSS 中引用的资源路径格式: /archive/resources/xx/yy/hash.ext）
	r.GET("/archive/resources/*filepath", handler.ServeLocalResource)
}
