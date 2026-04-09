package config

import (
	"os"
	"runtime"
	"strconv"
	"testing"
)

func TestDetectResourceConfig_AutoDetect(t *testing.T) {
	// 确保没有环境变量干扰
	os.Unsetenv("RESOURCE_WORKERS")
	os.Unsetenv("RESOURCE_CACHE_MB")
	os.Unsetenv("RESOURCE_METADATA_CACHE_MB")
	os.Unsetenv("RESOURCE_DOWNLOAD_TIMEOUT")

	cfg := detectResourceConfig()

	// workers 应该与 CPU 核心数 × 4 相关（I/O 密集型任务）
	cpus := runtime.NumCPU()
	expectedWorkers := cpus * 4
	if expectedWorkers < 2 {
		expectedWorkers = 2
	}
	if cfg.Workers != expectedWorkers {
		t.Errorf("Workers = %d, want %d (cpus=%d, multiplier=4)", cfg.Workers, expectedWorkers, cpus)
	}

	// cache 应该 > 0
	if cfg.MetadataCacheMB <= 0 {
		t.Errorf("MetadataCacheMB = %d, want > 0", cfg.MetadataCacheMB)
	}

	// download timeout 默认 30
	if cfg.DownloadTimeout != 30 {
		t.Errorf("DownloadTimeout = %d, want 30", cfg.DownloadTimeout)
	}
}

func TestDetectResourceConfig_EnvOverride(t *testing.T) {
	t.Setenv("RESOURCE_WORKERS", "3")
	t.Setenv("RESOURCE_METADATA_CACHE_MB", "200")
	t.Setenv("RESOURCE_DOWNLOAD_TIMEOUT", "60")

	cfg := detectResourceConfig()

	if cfg.Workers != 3 {
		t.Errorf("Workers = %d, want 3", cfg.Workers)
	}
	if cfg.MetadataCacheMB != 200 {
		t.Errorf("MetadataCacheMB = %d, want 200", cfg.MetadataCacheMB)
	}
	if cfg.DownloadTimeout != 60 {
		t.Errorf("DownloadTimeout = %d, want 60", cfg.DownloadTimeout)
	}
}

func TestDetectResourceConfig_EnvZeroUsesDefault(t *testing.T) {
	// 0 表示不覆盖，使用自动检测值
	t.Setenv("RESOURCE_WORKERS", "0")
	t.Setenv("RESOURCE_METADATA_CACHE_MB", "0")

	cfg := detectResourceConfig()

	// 应该使用自动检测值，而不是 0
	if cfg.Workers < 1 {
		t.Errorf("Workers = %d, want >= 1 (should use auto-detected default)", cfg.Workers)
	}
	// cache 为 0 时被安全边界拦截为 1
	if cfg.MetadataCacheMB < 1 {
		t.Errorf("MetadataCacheMB = %d, want >= 1", cfg.MetadataCacheMB)
	}
}

func TestDetectResourceConfig_SafetyBounds(t *testing.T) {
	// 负值不满足 v > 0，workers 使用自动检测值
	t.Setenv("RESOURCE_WORKERS", "-5")
	// downloadTimeout 直接赋值，安全边界将 1 提升到 5
	t.Setenv("RESOURCE_DOWNLOAD_TIMEOUT", "1")

	cfg := detectResourceConfig()

	if cfg.Workers < 1 {
		t.Errorf("Workers = %d, want >= 1 (safety bound)", cfg.Workers)
	}
	if cfg.DownloadTimeout < 5 {
		t.Errorf("DownloadTimeout = %d, want >= 5 (safety bound)", cfg.DownloadTimeout)
	}
}

func TestDetectResourceConfig_InvalidEnvIgnored(t *testing.T) {
	t.Setenv("RESOURCE_WORKERS", "not_a_number")
	t.Setenv("RESOURCE_METADATA_CACHE_MB", "abc")

	cfg := detectResourceConfig()

	// 无效值应被忽略，使用自动检测默认值
	if cfg.Workers < 1 {
		t.Errorf("Workers = %d, want >= 1 (invalid env should use default)", cfg.Workers)
	}
	if cfg.MetadataCacheMB < 1 {
		t.Errorf("MetadataCacheMB = %d, want >= 1 (invalid env should use default)", cfg.MetadataCacheMB)
	}
}

func TestGetTotalMemoryMB(t *testing.T) {
	mem := getTotalMemoryMB()

	// 应该返回一个合理的值（至少大于 0）
	if mem <= 0 {
		t.Errorf("getTotalMemoryMB() = %d, want > 0", mem)
	}

	// 现代机器至少 256MB 内存
	if mem < 256 {
		t.Logf("Warning: detected memory %dMB seems very low", mem)
	}

	t.Logf("Detected total memory: %dMB", mem)
}

func TestDetectResourceConfig_StreamThresholdZero(t *testing.T) {
	// 设置为 0 意味着"所有文件都流式落盘"，不应被覆盖
	t.Setenv("RESOURCE_STREAM_THRESHOLD_KB", "0")

	cfg := detectResourceConfig()

	if cfg.StreamThresholdKB != 0 {
		t.Errorf("StreamThresholdKB = %d, want 0 (should allow zero for 'stream everything')", cfg.StreamThresholdKB)
	}
}

func TestDetectResourceConfig_StreamThresholdDefault(t *testing.T) {
	os.Unsetenv("RESOURCE_STREAM_THRESHOLD_KB")

	cfg := detectResourceConfig()

	if cfg.StreamThresholdKB != 2048 {
		t.Errorf("StreamThresholdKB = %d, want 2048 (default)", cfg.StreamThresholdKB)
	}
}

func TestDetectResourceConfig_CacheNotExceedMemory(t *testing.T) {
	totalMem := getTotalMemoryMB()

	// 设置缓存为总内存的两倍
	t.Setenv("RESOURCE_METADATA_CACHE_MB", strconv.Itoa(totalMem*2))

	cfg := detectResourceConfig()

	if cfg.MetadataCacheMB > totalMem {
		t.Errorf("MetadataCacheMB = %d, exceeds total memory %dMB", cfg.MetadataCacheMB, totalMem)
	}
}

func TestDetectResourceConfig_LegacyCacheEnvFallback(t *testing.T) {
	t.Setenv("RESOURCE_CACHE_MB", "123")
	t.Setenv("RESOURCE_METADATA_CACHE_MB", "")

	cfg := detectResourceConfig()

	if cfg.MetadataCacheMB != 123 {
		t.Errorf("MetadataCacheMB = %d, want 123 from legacy env", cfg.MetadataCacheMB)
	}
}

func TestDetectResourceConfig_NewCacheEnvTakesPrecedence(t *testing.T) {
	t.Setenv("RESOURCE_CACHE_MB", "123")
	t.Setenv("RESOURCE_METADATA_CACHE_MB", "456")

	cfg := detectResourceConfig()

	if cfg.MetadataCacheMB != 456 {
		t.Errorf("MetadataCacheMB = %d, want 456 from new env", cfg.MetadataCacheMB)
	}
}

func TestDetectResourceConfig_NegativeStreamThreshold(t *testing.T) {
	// 负值应被安全边界修正为 0
	t.Setenv("RESOURCE_STREAM_THRESHOLD_KB", "-100")

	cfg := detectResourceConfig()

	if cfg.StreamThresholdKB != 0 {
		t.Errorf("StreamThresholdKB = %d, want 0 (negative should be clamped to 0)", cfg.StreamThresholdKB)
	}
}

func TestDetectResourceConfig_LargeWorkers(t *testing.T) {
	t.Setenv("RESOURCE_WORKERS", "1000")

	cfg := detectResourceConfig()

	// 应该接受大值（不设上限，由用户决定）
	if cfg.Workers != 1000 {
		t.Errorf("Workers = %d, want 1000", cfg.Workers)
	}
}

func TestDetectResourceConfig_StreamThresholdCustom(t *testing.T) {
	t.Setenv("RESOURCE_STREAM_THRESHOLD_KB", "512")

	cfg := detectResourceConfig()

	if cfg.StreamThresholdKB != 512 {
		t.Errorf("StreamThresholdKB = %d, want 512", cfg.StreamThresholdKB)
	}
}

func TestLoadFromEnv_DebugAPIDefaultDisabled(t *testing.T) {
	t.Setenv("ENABLE_DEBUG_API", "")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}
	if cfg.Server.EnableDebugAPI {
		t.Fatal("EnableDebugAPI = true, want false by default")
	}
}

func TestLoadFromEnv_DebugAPIEnabled(t *testing.T) {
	t.Setenv("ENABLE_DEBUG_API", "true")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}
	if !cfg.Server.EnableDebugAPI {
		t.Fatal("EnableDebugAPI = false, want true")
	}
}
