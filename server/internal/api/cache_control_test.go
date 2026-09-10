package api

import (
	"testing"

	"wayback/internal/config"
)

func TestResourceCacheControl_NoAuth(t *testing.T) {
	handler := &Handler{
		authCfg: &config.AuthConfig{},
	}

	got := handler.resourceCacheControl()
	want := "public, max-age=31536000"

	if got != want {
		t.Errorf("resourceCacheControl() with no auth = %q, want %q", got, want)
	}
}

func TestResourceCacheControl_AuthEnabled(t *testing.T) {
	handler := &Handler{
		authCfg: &config.AuthConfig{Password: "secret"},
	}

	got := handler.resourceCacheControl()
	want := "private, max-age=31536000"

	if got != want {
		t.Errorf("resourceCacheControl() with auth enabled = %q, want %q", got, want)
	}
}

func TestResourceCacheControl_NilAuthConfig(t *testing.T) {
	handler := &Handler{
		authCfg: nil,
	}

	got := handler.resourceCacheControl()
	want := "public, max-age=31536000"

	if got != want {
		t.Errorf("resourceCacheControl() with nil authCfg = %q, want %q", got, want)
	}
}
