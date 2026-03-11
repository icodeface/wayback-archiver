package storage

import (
	"strings"
	"testing"
)

// BenchmarkRewriteHTML_Fast 测试快速版本的性能
func BenchmarkRewriteHTML_Fast(b *testing.B) {
	// 模拟真实场景：100 个资源，1MB HTML
	r := NewURLRewriter()
	r.SetPageID(123)
	r.SetTimestamp("20260311")

	// 添加 100 个资源映射
	for i := 0; i < 100; i++ {
		url := "https://example.com/resource" + string(rune(i)) + ".jpg"
		r.AddMapping(url, "resources/hash"+string(rune(i))+".img")
	}

	// 生成 1MB HTML（包含所有资源引用）
	var htmlBuilder strings.Builder
	htmlBuilder.WriteString("<html><head>")
	for i := 0; i < 100; i++ {
		url := "https://example.com/resource" + string(rune(i)) + ".jpg"
		htmlBuilder.WriteString(`<link href="` + url + `">`)
	}
	htmlBuilder.WriteString("</head><body>")
	// 填充到 1MB
	for htmlBuilder.Len() < 1024*1024 {
		htmlBuilder.WriteString("<p>Lorem ipsum dolor sit amet, consectetur adipiscing elit.</p>")
	}
	htmlBuilder.WriteString("</body></html>")
	html := htmlBuilder.String()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.RewriteHTML(html)
	}
}

// BenchmarkRewriteHTML_Regex 测试正则版本的性能（用于对比）
func BenchmarkRewriteHTML_Regex(b *testing.B) {
	r := NewURLRewriter()
	r.SetPageID(123)
	r.SetTimestamp("20260311")

	for i := 0; i < 100; i++ {
		url := "https://example.com/resource" + string(rune(i)) + ".jpg"
		r.AddMapping(url, "resources/hash"+string(rune(i))+".img")
	}

	var htmlBuilder strings.Builder
	htmlBuilder.WriteString("<html><head>")
	for i := 0; i < 100; i++ {
		url := "https://example.com/resource" + string(rune(i)) + ".jpg"
		htmlBuilder.WriteString(`<link href="` + url + `">`)
	}
	htmlBuilder.WriteString("</head><body>")
	for htmlBuilder.Len() < 1024*1024 {
		htmlBuilder.WriteString("<p>Lorem ipsum dolor sit amet, consectetur adipiscing elit.</p>")
	}
	htmlBuilder.WriteString("</body></html>")
	html := htmlBuilder.String()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.rewriteHTMLRegex(html)
	}
}
