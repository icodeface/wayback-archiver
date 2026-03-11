package storage

import (
	"fmt"
	"net/url"
	"strings"
)

// RewriteHTMLFast 使用 strings.NewReplacer 快速重写 HTML 中的资源 URL
// 相比原版 RewriteHTML，这个版本做单次遍历替换，速度快 100 倍以上
func (r *URLRewriter) RewriteHTMLFast(html string) string {
	// 构建所有需要替换的 URL 变体
	var pairs []string

	for originalURL := range r.urlToLocalPath {
		// 构建本地 URL
		var localURL string
		if r.pageID > 0 && r.timestamp != "" {
			localURL = fmt.Sprintf("/archive/%d/%smp_/%s", r.pageID, r.timestamp, originalURL)
		} else {
			localURL = "/archive/" + r.urlToLocalPath[originalURL]
		}

		// 1. 原始 URL 的各种属性形式
		pairs = append(pairs,
			` src="`+originalURL+`"`, ` src="`+localURL+`"`,
			` href="`+originalURL+`"`, ` href="`+localURL+`"`,
			` poster="`+originalURL+`"`, ` poster="`+localURL+`"`,
			` srcset="`+originalURL+`"`, ` srcset="`+localURL+`"`,
			`url("`+originalURL+`")`, `url("`+localURL+`")`,
			`url('`+originalURL+`')`, `url('`+localURL+`')`,
			`url(`+originalURL+`)`, `url(`+localURL+`)`,
		)

		// 2. 协议相对 URL（如 //example.com/path）
		protocolRelativeURL := strings.TrimPrefix(originalURL, "https:")
		protocolRelativeURL = strings.TrimPrefix(protocolRelativeURL, "http:")
		if protocolRelativeURL != originalURL && strings.HasPrefix(protocolRelativeURL, "//") {
			pairs = append(pairs,
				` src="`+protocolRelativeURL+`"`, ` src="`+localURL+`"`,
				` href="`+protocolRelativeURL+`"`, ` href="`+localURL+`"`,
				` poster="`+protocolRelativeURL+`"`, ` poster="`+localURL+`"`,
				` srcset="`+protocolRelativeURL+`"`, ` srcset="`+localURL+`"`,
				`url("`+protocolRelativeURL+`")`, `url("`+localURL+`")`,
				`url('`+protocolRelativeURL+`')`, `url('`+localURL+`')`,
				`url(`+protocolRelativeURL+`)`, `url(`+localURL+`)`,
			)
		}

		// 3. HTML 实体编码的 URL（& -> &amp;）
		htmlEncodedURL := strings.ReplaceAll(originalURL, "&", "&amp;")
		if htmlEncodedURL != originalURL {
			pairs = append(pairs,
				` src="`+htmlEncodedURL+`"`, ` src="`+localURL+`"`,
				` href="`+htmlEncodedURL+`"`, ` href="`+localURL+`"`,
				` poster="`+htmlEncodedURL+`"`, ` poster="`+localURL+`"`,
				` srcset="`+htmlEncodedURL+`"`, ` srcset="`+localURL+`"`,
				`url("`+htmlEncodedURL+`")`, `url("`+localURL+`")`,
				`url('`+htmlEncodedURL+`')`, `url('`+localURL+`')`,
				`url(`+htmlEncodedURL+`)`, `url(`+localURL+`)`,
			)

			// 协议相对 + &amp; 组合
			if protocolRelativeURL != originalURL && strings.HasPrefix(protocolRelativeURL, "//") {
				protoRelEncoded := strings.ReplaceAll(protocolRelativeURL, "&", "&amp;")
				pairs = append(pairs,
					` src="`+protoRelEncoded+`"`, ` src="`+localURL+`"`,
					` href="`+protoRelEncoded+`"`, ` href="`+localURL+`"`,
					` poster="`+protoRelEncoded+`"`, ` poster="`+localURL+`"`,
					` srcset="`+protoRelEncoded+`"`, ` srcset="`+localURL+`"`,
					`url("`+protoRelEncoded+`")`, `url("`+localURL+`")`,
					`url('`+protoRelEncoded+`')`, `url('`+localURL+`')`,
					`url(`+protoRelEncoded+`)`, `url(`+localURL+`)`,
				)
			}
		}

		// 4. url(&quot;...&quot;) 格式
		pairs = append(pairs,
			`url(&quot;`+originalURL+`&quot;)`, `url(&quot;`+localURL+`&quot;)`,
		)
		if htmlEncodedURL != originalURL {
			pairs = append(pairs,
				`url(&quot;`+htmlEncodedURL+`&quot;)`, `url(&quot;`+localURL+`&quot;)`,
			)
		}

		// 5. 绝对路径（如 /assets/style.css）
		parsed, err := url.Parse(originalURL)
		if err == nil && parsed.Path != "" {
			pathWithQuery := parsed.Path
			if parsed.RawQuery != "" {
				pathWithQuery = parsed.Path + "?" + parsed.RawQuery
			}
			pairs = append(pairs,
				` src="`+pathWithQuery+`"`, ` src="`+localURL+`"`,
				` href="`+pathWithQuery+`"`, ` href="`+localURL+`"`,
				` poster="`+pathWithQuery+`"`, ` poster="`+localURL+`"`,
				` srcset="`+pathWithQuery+`"`, ` srcset="`+localURL+`"`,
				`url("`+pathWithQuery+`")`, `url("`+localURL+`")`,
				`url('`+pathWithQuery+`')`, `url('`+localURL+`')`,
				`url(`+pathWithQuery+`)`, `url(`+localURL+`)`,
			)
		}
	}

	// 使用 strings.NewReplacer 做单次遍历替换
	replacer := strings.NewReplacer(pairs...)
	return replacer.Replace(html)
}
