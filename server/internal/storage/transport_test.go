package storage

import (
	"net/http"
	"testing"
	"time"
)

func TestNewFileStorage_TransportLimits(t *testing.T) {
	fs := NewFileStorage(t.TempDir())

	transport, ok := fs.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected *http.Transport")
	}

	if transport.MaxIdleConns != 100 {
		t.Errorf("MaxIdleConns = %d, want 100", transport.MaxIdleConns)
	}
	if transport.MaxIdleConnsPerHost != 10 {
		t.Errorf("MaxIdleConnsPerHost = %d, want 10", transport.MaxIdleConnsPerHost)
	}
	if transport.MaxConnsPerHost != 20 {
		t.Errorf("MaxConnsPerHost = %d, want 20", transport.MaxConnsPerHost)
	}
	if transport.IdleConnTimeout != 90*time.Second {
		t.Errorf("IdleConnTimeout = %v, want 90s", transport.IdleConnTimeout)
	}
}

func TestNewFileStorage_TransportLimitsWithProxy(t *testing.T) {
	// 设置代理环境变量，验证连接池限制仍然生效
	t.Setenv("https_proxy", "http://proxy.example.com:8080")

	fs := NewFileStorage(t.TempDir())

	transport, ok := fs.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected *http.Transport")
	}

	// 即使设置了代理，连接池限制也应该存在
	if transport.MaxIdleConns != 100 {
		t.Errorf("MaxIdleConns with proxy = %d, want 100", transport.MaxIdleConns)
	}
	if transport.MaxConnsPerHost != 20 {
		t.Errorf("MaxConnsPerHost with proxy = %d, want 20", transport.MaxConnsPerHost)
	}
	// 代理应被设置
	if transport.Proxy == nil {
		t.Error("Proxy should be set when https_proxy env is configured")
	}
}
