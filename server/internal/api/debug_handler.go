package api

import (
	"net/http"
	"runtime"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

// MemStats 返回当前内存使用统计（用于调试和监控内存泄漏）
func (h *Handler) MemStats(c *gin.Context) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	c.JSON(http.StatusOK, gin.H{
		"alloc_mb":        float64(m.Alloc) / 1024 / 1024,
		"total_alloc_mb":  float64(m.TotalAlloc) / 1024 / 1024,
		"sys_mb":          float64(m.Sys) / 1024 / 1024,
		"heap_alloc_mb":   float64(m.HeapAlloc) / 1024 / 1024,
		"heap_inuse_mb":   float64(m.HeapInuse) / 1024 / 1024,
		"heap_idle_mb":    float64(m.HeapIdle) / 1024 / 1024,
		"heap_released_mb": float64(m.HeapReleased) / 1024 / 1024,
		"heap_objects":    m.HeapObjects,
		"goroutines":      runtime.NumGoroutine(),
		"gc_cycles":       m.NumGC,
		"last_gc_ns":      m.LastGC,
	})
}

// ForceGC 手动触发垃圾回收（用于测试内存释放）
func (h *Handler) ForceGC(c *gin.Context) {
	runtime.GC()
	debug.FreeOSMemory()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	c.JSON(http.StatusOK, gin.H{
		"status":        "gc_completed",
		"heap_alloc_mb": float64(m.HeapAlloc) / 1024 / 1024,
		"heap_inuse_mb": float64(m.HeapInuse) / 1024 / 1024,
		"gc_cycles":     m.NumGC,
		"goroutines":    runtime.NumGoroutine(),
	})
}
