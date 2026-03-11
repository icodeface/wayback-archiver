package storage

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// NormalizeHTMLURLs 规范化 HTML 中所有包含 ../ 的绝对 URL
// 例如：https://example.com/path/../file.css -> https://example.com/file.css
func NormalizeHTMLURLs(html string) string {
	// 匹配 src/href/poster 属性中的绝对 URL（双引号和单引号分别匹配）
	urlRegexDQ := regexp.MustCompile(`((?:src|href|poster)=")(https?://[^"]+)"`)
	urlRegexSQ := regexp.MustCompile(`((?:src|href|poster)=')(https?://[^']+)'`)

	normalize := func(match string, re *regexp.Regexp, quote string) string {
		parts := re.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}

		attrPrefix := parts[1] // 如 href="
		rawURL := parts[2]

		if !strings.Contains(rawURL, "..") {
			return match
		}

		parsed, err := url.Parse(rawURL)
		if err != nil {
			return match
		}

		normalized := parsed.ResolveReference(&url.URL{})
		return attrPrefix + normalized.String() + quote
	}

	result := urlRegexDQ.ReplaceAllStringFunc(html, func(match string) string {
		return normalize(match, urlRegexDQ, `"`)
	})
	result = urlRegexSQ.ReplaceAllStringFunc(result, func(match string) string {
		return normalize(match, urlRegexSQ, `'`)
	})

	return result
}


// URLRewriter 负责重写 HTML 中的资源 URL
type URLRewriter struct {
	urlToLocalPath map[string]string
	baseURL        string
	pageID         int64
	timestamp      string
}

func NewURLRewriter() *URLRewriter {
	return &URLRewriter{
		urlToLocalPath: make(map[string]string),
	}
}

// SetBaseURL 设置基础 URL（用于解析相对路径）
func (r *URLRewriter) SetBaseURL(baseURL string) {
	r.baseURL = baseURL
}

// SetPageID 设置页面 ID
func (r *URLRewriter) SetPageID(pageID int64) {
	r.pageID = pageID
}

// SetTimestamp 设置时间戳
func (r *URLRewriter) SetTimestamp(timestamp string) {
	r.timestamp = timestamp
}

// AddMapping 添加 URL 到本地路径的映射
func (r *URLRewriter) AddMapping(originalURL, localPath string) {
	r.urlToLocalPath[originalURL] = localPath
}

// getRelativePath 从完整 URL 中提取相对路径
func (r *URLRewriter) getRelativePath(fullURL string) string {
	parsed, err := url.Parse(fullURL)
	if err != nil {
		return ""
	}
	// 返回路径的最后一部分（文件名）
	return path.Base(parsed.Path)
}

// replaceURLInHTML 替换 HTML 中匹配 escapedURL 的 src/href/poster/url() 引用
// 使用 \s 前缀匹配 src=/href=/poster= 以避免误匹配 data-src= 等属性
func replaceURLInHTML(html, escapedURL, localURL string) string {
	patterns := []string{
		`(\s)src=["']` + escapedURL + `["']`,
		`(\s)href=["']` + escapedURL + `["']`,
		`(\s)poster=["']` + escapedURL + `["']`,
		`(\s)srcset=["']` + escapedURL + `["']`,
		`url\(["']?` + escapedURL + `["']?\)`,
	}
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		html = re.ReplaceAllStringFunc(html, func(match string) string {
			if strings.Contains(match, `url(`) {
				return `url("` + localURL + `")`
			}
			// 保留前导空白字符
			ws := match[:1]
			if strings.Contains(match, `src=`) && !strings.Contains(match, `srcset=`) {
				return ws + `src="` + localURL + `"`
			}
			if strings.Contains(match, `srcset=`) {
				return ws + `srcset="` + localURL + `"`
			}
			if strings.Contains(match, `poster=`) {
				return ws + `poster="` + localURL + `"`
			}
			return ws + `href="` + localURL + `"`
		})
	}
	return html
}

// RewriteHTML 重写 HTML 中的资源 URL
func (r *URLRewriter) RewriteHTML(html string) string {
	return r.RewriteHTMLFast(html)
}

// rewriteHTMLRegex 使用正则逐个替换（旧版本，保留用于对比测试）
func (r *URLRewriter) rewriteHTMLRegex(html string) string {
	result := html

	// 替换所有已知的资源 URL
	for originalURL, localPath := range r.urlToLocalPath {
		// 构建本地 URL：/archive/{pageID}/{timestamp}mp_/{originalURL}
		var localURL string
		if r.pageID > 0 && r.timestamp != "" {
			localURL = fmt.Sprintf("/archive/%d/%smp_/%s", r.pageID, r.timestamp, originalURL)
		} else {
			localURL = "/archive/" + localPath
		}

		escapedURL := regexp.QuoteMeta(originalURL)

		// 1. 替换完整的绝对 URL
		result = replaceURLInHTML(result, escapedURL, localURL)

		// 2. 处理协议相对 URL（如 //example.com/path）
		protocolRelativeURL := strings.TrimPrefix(originalURL, "https:")
		protocolRelativeURL = strings.TrimPrefix(protocolRelativeURL, "http:")

		if protocolRelativeURL != originalURL && strings.HasPrefix(protocolRelativeURL, "//") {
			result = replaceURLInHTML(result, regexp.QuoteMeta(protocolRelativeURL), localURL)
		}

		// 3. 替换相对路径（只匹配文件名）
		relativePath := r.getRelativePath(originalURL)
		if relativePath != "" && relativePath != "." && relativePath != "/" {
			escapedRelative := regexp.QuoteMeta(relativePath)
			relPatterns := []string{
				`(\s)src=["']\.?/?` + escapedRelative + `["']`,
				`(\s)href=["']\.?/?` + escapedRelative + `["']`,
			}
			for _, pattern := range relPatterns {
				re := regexp.MustCompile(pattern)
				result = re.ReplaceAllStringFunc(result, func(match string) string {
					ws := match[:1]
					if strings.Contains(match, `src=`) {
						return ws + `src="` + localURL + `"`
					}
					return ws + `href="` + localURL + `"`
				})
			}
		}

		// 4. 处理以 / 开头的绝对路径（如 /assets/style.css）
		parsed, err := url.Parse(originalURL)
		if err == nil && parsed.Path != "" {
			pathWithQuery := parsed.Path
			if parsed.RawQuery != "" {
				pathWithQuery = parsed.Path + "?" + parsed.RawQuery
			}
			result = replaceURLInHTML(result, regexp.QuoteMeta(pathWithQuery), localURL)
		}

		// 5. 处理 HTML 实体编码的 URL（& -> &amp;）
		htmlEncodedURL := strings.ReplaceAll(originalURL, "&", "&amp;")
		if htmlEncodedURL != originalURL {
			result = replaceURLInHTML(result, regexp.QuoteMeta(htmlEncodedURL), localURL)

			// 5b. 协议相对 + &amp; 组合（如 //host/path?a=1&amp;b=2）
			if protocolRelativeURL != originalURL && strings.HasPrefix(protocolRelativeURL, "//") {
				protoRelEncoded := strings.ReplaceAll(protocolRelativeURL, "&", "&amp;")
				result = replaceURLInHTML(result, regexp.QuoteMeta(protoRelEncoded), localURL)
			}
		}

		// 6. 处理 url(&quot;...&quot;) 格式
		quotPatterns := []string{
			`url\(&quot;` + escapedURL + `&quot;\)`,
		}
		if htmlEncodedURL != originalURL {
			quotPatterns = append(quotPatterns,
				`url\(&quot;`+regexp.QuoteMeta(htmlEncodedURL)+`&quot;\)`,
			)
		}
		for _, pattern := range quotPatterns {
			re := regexp.MustCompile(pattern)
			result = re.ReplaceAllString(result, `url(&quot;`+localURL+`&quot;)`)
		}
	}

	return result
}
