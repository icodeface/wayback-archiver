package api

import (
	"io/fs"
	"net/http"
	"net/http/pprof"
	"strings"

	"github.com/gin-gonic/gin"
	"wayback/internal/config"
	"wayback/web"
)

func embeddedFileContentType(name string) string {
	switch {
	case len(name) > 5 && name[len(name)-5:] == ".html":
		return "text/html; charset=utf-8"
	case len(name) > 4 && name[len(name)-4:] == ".ico":
		return "image/x-icon"
	case len(name) > 4 && name[len(name)-4:] == ".txt":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func serveEmbeddedFile(webFS fs.FS, name string) gin.HandlerFunc {
	data, err := fs.ReadFile(webFS, name)
	contentType := embeddedFileContentType(name)

	return func(c *gin.Context) {
		if err != nil {
			c.String(http.StatusInternalServerError, "Failed to load embedded file")
			return
		}
		c.Data(http.StatusOK, contentType, data)
	}
}

// SetupRoutes 设置路由
func SetupRoutes(r *gin.Engine, handler *Handler, authCfg *config.AuthConfig, serverCfg *config.ServerConfig, version, buildTime string) {
	origins := serverCfg.AllowedOrigins
	if len(origins) == 0 {
		origins = config.DefaultAllowedOrigins()
	}

	allowedOrigins := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		allowedOrigins[trimmed] = struct{}{}
	}

	// CORS 中间件 - 仅允许显式白名单中的 http(s) 来源；拒绝 Origin: null 这类 opaque origin
	r.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin != "" {
			c.Writer.Header().Add("Vary", "Origin")
		}
		if origin != "null" {
			if _, ok := allowedOrigins[origin]; ok {
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			}
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
	r.GET("/robots.txt", serveEmbeddedFile(webFS, "robots.txt"))

	// Basic Auth 中间件（如果启用）
	if authCfg.Enabled() {
		accounts := gin.Accounts{
			config.AuthUsername: authCfg.Password,
		}
		r.Use(gin.BasicAuth(accounts))
	}

	// Web UI (embedded)
	r.GET("/", serveEmbeddedFile(webFS, "index.html"))
	r.GET("/index.html", serveEmbeddedFile(webFS, "index.html"))
	r.GET("/timeline", serveEmbeddedFile(webFS, "timeline.html"))
	r.GET("/timeline.html", serveEmbeddedFile(webFS, "timeline.html"))
	r.GET("/logs", serveEmbeddedFile(webFS, "logs.html"))
	r.GET("/logs.html", serveEmbeddedFile(webFS, "logs.html"))
	r.GET("/favicon.ico", serveEmbeddedFile(webFS, "favicon.ico"))

	api := r.Group("/api")
	{
		api.GET("/version", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"version":    version,
				"build_time": buildTime,
				"repo":       "https://github.com/icodeface/wayback-archiver",
			})
		})
		if serverCfg.EnableDebugAPI {
			api.GET("/debug/memstats", handler.MemStats)
			api.POST("/debug/gc", handler.ForceGC)
			api.GET("/debug/pprof/", gin.WrapF(pprof.Index))
			api.GET("/debug/pprof/cmdline", gin.WrapF(pprof.Cmdline))
			api.GET("/debug/pprof/profile", gin.WrapF(pprof.Profile))
			api.POST("/debug/pprof/symbol", gin.WrapF(pprof.Symbol))
			api.GET("/debug/pprof/symbol", gin.WrapF(pprof.Symbol))
			api.GET("/debug/pprof/trace", gin.WrapF(pprof.Trace))
			api.GET("/debug/pprof/allocs", gin.WrapH(pprof.Handler("allocs")))
			api.GET("/debug/pprof/block", gin.WrapH(pprof.Handler("block")))
			api.GET("/debug/pprof/goroutine", gin.WrapH(pprof.Handler("goroutine")))
			api.GET("/debug/pprof/heap", gin.WrapH(pprof.Handler("heap")))
			api.GET("/debug/pprof/mutex", gin.WrapH(pprof.Handler("mutex")))
			api.GET("/debug/pprof/threadcreate", gin.WrapH(pprof.Handler("threadcreate")))
		}
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
