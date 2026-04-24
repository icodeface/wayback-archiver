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

type headResponseWriter struct {
	gin.ResponseWriter
}

func (w *headResponseWriter) Write(data []byte) (int, error) {
	if !w.ResponseWriter.Written() {
		w.ResponseWriter.WriteHeaderNow()
	}
	return len(data), nil
}

func (w *headResponseWriter) WriteString(s string) (int, error) {
	if !w.ResponseWriter.Written() {
		w.ResponseWriter.WriteHeaderNow()
	}
	return len(s), nil
}

func suppressHeadBody() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer = &headResponseWriter{ResponseWriter: c.Writer}
		c.Next()
	}
}

func registerGETAndHEAD(routes gin.IRoutes, relativePath string, handlers ...gin.HandlerFunc) {
	routes.GET(relativePath, handlers...)
	routes.HEAD(relativePath, append([]gin.HandlerFunc{suppressHeadBody()}, handlers...)...)
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
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, PUT, GET, HEAD, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})

	// robots.txt（在认证之前，确保爬虫和工具可以访问）
	webFS, _ := fs.Sub(web.StaticFiles, ".")
	registerGETAndHEAD(r, "/robots.txt", serveEmbeddedFile(webFS, "robots.txt"))

	// Basic Auth 中间件（如果启用）
	if authCfg.Enabled() {
		accounts := gin.Accounts{
			config.AuthUsername: authCfg.Password,
		}
		r.Use(gin.BasicAuth(accounts))
	}

	// Web UI (embedded)
	registerGETAndHEAD(r, "/", serveEmbeddedFile(webFS, "index.html"))
	registerGETAndHEAD(r, "/index.html", serveEmbeddedFile(webFS, "index.html"))
	registerGETAndHEAD(r, "/timeline", serveEmbeddedFile(webFS, "timeline.html"))
	registerGETAndHEAD(r, "/timeline.html", serveEmbeddedFile(webFS, "timeline.html"))
	registerGETAndHEAD(r, "/logs", serveEmbeddedFile(webFS, "logs.html"))
	registerGETAndHEAD(r, "/logs.html", serveEmbeddedFile(webFS, "logs.html"))
	registerGETAndHEAD(r, "/favicon.ico", serveEmbeddedFile(webFS, "favicon.ico"))

	api := r.Group("/api")
	{
		registerGETAndHEAD(api, "/version", func(c *gin.Context) {
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
		registerGETAndHEAD(api, "/pages", handler.ListPages)
		registerGETAndHEAD(api, "/pages/timeline", handler.GetPageTimeline)
		registerGETAndHEAD(api, "/pages/:id", handler.GetPage)
		registerGETAndHEAD(api, "/pages/:id/content", handler.GetPageContent)
		api.DELETE("/pages/:id", handler.DeletePage)
		registerGETAndHEAD(api, "/search", handler.SearchPages)
		registerGETAndHEAD(api, "/logs", handler.ListLogs)
		registerGETAndHEAD(api, "/logs/latest", handler.GetLatestLog)
		registerGETAndHEAD(api, "/logs/:filename", handler.GetLog)
	}

	// 查看归档页面
	registerGETAndHEAD(r, "/view/:id", handler.ViewPage)
	registerGETAndHEAD(r, "/archive/:page_id/:timestamp/*resource_path", handler.ProxyResource)

	// 直接资源访问（CSS 中引用的资源路径格式: /archive/resources/xx/yy/hash.ext）
	registerGETAndHEAD(r, "/archive/resources/*filepath", handler.ServeLocalResource)
}
