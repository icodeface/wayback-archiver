package api

import (
	"os"
	"path/filepath"
	"strings"
)

// escapeHTML 转义 HTML 特殊字符
func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

// detectFontType 检测字体类型
func detectFontType(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".otf":
		return "font/otf"
	case ".eot":
		return "application/vnd.ms-fontobject"
	default:
		return "font/woff2"
	}
}

// detectArchivedFontType inspects stored .font files to recover the original font MIME type.
func detectArchivedFontType(filePath string) string {
	f, err := os.Open(filePath)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()

	header := make([]byte, 36)
	n, err := f.Read(header)
	if err != nil || n == 0 {
		return "application/octet-stream"
	}

	return detectFontTypeFromHeader(header[:n])
}

func detectFontTypeFromHeader(header []byte) string {
	if len(header) >= 4 {
		switch string(header[:4]) {
		case "wOFF":
			return "font/woff"
		case "wOF2":
			return "font/woff2"
		case "OTTO":
			return "font/otf"
		case "true":
			return "font/ttf"
		}

		if header[0] == 0x00 && header[1] == 0x01 && header[2] == 0x00 && header[3] == 0x00 {
			return "font/ttf"
		}
	}

	if len(header) >= 36 && header[34] == 'L' && header[35] == 'P' {
		return "application/vnd.ms-fontobject"
	}

	return "application/octet-stream"
}
