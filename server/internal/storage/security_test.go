package storage

import (
	"net"
	"testing"
)

func TestValidateResourceURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantError bool
	}{
		// Valid URLs
		{
			name:      "valid https URL",
			url:       "https://example.com/image.png",
			wantError: false,
		},
		{
			name:      "valid http URL",
			url:       "http://example.com/style.css",
			wantError: false,
		},
		// Invalid schemes
		{
			name:      "file scheme not allowed",
			url:       "file:///etc/passwd",
			wantError: true,
		},
		{
			name:      "ftp scheme not allowed",
			url:       "ftp://example.com/file.txt",
			wantError: true,
		},
		// Private IPs (these will be blocked by DNS lookup + IP check)
		{
			name:      "localhost not allowed",
			url:       "http://localhost/api",
			wantError: true, // DNS resolves to 127.0.0.1, which is private
		},
		{
			name:      "127.0.0.1 not allowed",
			url:       "http://127.0.0.1/api",
			wantError: true, // Private IP
		},
		// Invalid URLs
		{
			name:      "invalid URL",
			url:       "not a url",
			wantError: true,
		},
		{
			name:      "missing hostname",
			url:       "http:///path",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateResourceURL(tt.url)
			if (err != nil) != tt.wantError {
				t.Errorf("validateResourceURL() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		isPriv  bool
	}{
		// Private IPv4
		{"10.0.0.1", "10.0.0.1", true},
		{"172.16.0.1", "172.16.0.1", true},
		{"192.168.1.1", "192.168.1.1", true},
		{"127.0.0.1", "127.0.0.1", true},
		{"169.254.1.1", "169.254.1.1", true},
		// Public IPv4
		{"8.8.8.8", "8.8.8.8", false},
		{"1.1.1.1", "1.1.1.1", false},
		// Private IPv6
		{"::1", "::1", true},
		{"fc00::1", "fc00::1", true},
		{"fe80::1", "fe80::1", true},
		// Public IPv6
		{"2001:4860:4860::8888", "2001:4860:4860::8888", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP: %s", tt.ip)
			}
			got := isPrivateIP(ip)
			if got != tt.isPriv {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.isPriv)
			}
		})
	}
}

func TestIsCloudMetadataIP(t *testing.T) {
	tests := []struct {
		name       string
		ip         string
		isMetadata bool
	}{
		// Cloud metadata IPs
		{"AWS metadata", "169.254.169.254", true},
		{"AWS IPv6 metadata", "fd00:ec2::254", true},
		// Non-metadata IPs
		{"regular private IP", "192.168.1.1", false},
		{"public IP", "8.8.8.8", false},
		{"other link-local", "169.254.1.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP: %s", tt.ip)
			}
			got := isCloudMetadataIP(ip)
			if got != tt.isMetadata {
				t.Errorf("isCloudMetadataIP(%s) = %v, want %v", tt.ip, got, tt.isMetadata)
			}
		})
	}
}

func TestGetRootDomain(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		want     string
	}{
		// Standard TLDs
		{"simple domain", "example.com", "example.com"},
		{"subdomain", "www.example.com", "example.com"},
		{"deep subdomain", "api.v2.example.com", "example.com"},
		// Multi-segment TLDs
		{"co.uk domain", "example.co.uk", "example.co.uk"},
		{"co.uk subdomain", "www.example.co.uk", "example.co.uk"},
		{"com.au domain", "example.com.au", "example.com.au"},
		{"co.jp domain", "example.co.jp", "example.co.jp"},
		// Edge cases
		{"single segment", "localhost", "localhost"},
		{"two segments", "example.com", "example.com"},
		// Real-world examples from the issue
		{"m-team subdomain", "kp.m-team.cc", "m-team.cc"},
		{"m-team img subdomain", "img.m-team.cc", "m-team.cc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getRootDomain(tt.hostname)
			if got != tt.want {
				t.Errorf("getRootDomain(%s) = %s, want %s", tt.hostname, got, tt.want)
			}
		})
	}
}
