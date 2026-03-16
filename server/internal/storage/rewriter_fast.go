package storage

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// appendURLPairs 为给定的 URL 字符串生成所有属性形式的替换对
// 覆盖 src/href/poster/srcset 属性（双引号）和 url() 的三种引号形式
func appendURLPairs(pairs []string, urlStr, localURL string) []string {
	return append(pairs,
		` src="`+urlStr+`"`, ` src="`+localURL+`"`,
		` href="`+urlStr+`"`, ` href="`+localURL+`"`,
		` poster="`+urlStr+`"`, ` poster="`+localURL+`"`,
		` srcset="`+urlStr+`"`, ` srcset="`+localURL+`"`,
		`url("`+urlStr+`")`, `url("`+localURL+`")`,
		`url('`+urlStr+`')`, `url('`+localURL+`')`,
		`url(`+urlStr+`)`, `url(`+localURL+`)`,
	)
}

// ResolveRelativeURLs 将 HTML 中的相对路径解析为绝对 URL
// 这是 RewriteHTMLFast 的预处理步骤，确保所有 URL 都是绝对形式，
// 从而让 rewriter 只需匹配绝对 URL 变体即可覆盖所有情况。
// 处理 ./path、../path、bare/path 等所有相对路径形式。
func ResolveRelativeURLs(html, baseURL string) string {
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" {
		return html
	}

	resolve := func(val string) string {
		// 跳过已经是绝对的、特殊协议的、空的
		if val == "" || strings.HasPrefix(val, "http://") || strings.HasPrefix(val, "https://") ||
			strings.HasPrefix(val, "//") || strings.HasPrefix(val, "/") ||
			strings.HasPrefix(val, "data:") || strings.HasPrefix(val, "javascript:") ||
			strings.HasPrefix(val, "#") || strings.HasPrefix(val, "mailto:") ||
			strings.HasPrefix(val, "tel:") || strings.HasPrefix(val, "blob:") {
			return ""
		}
		// 解码 &amp; 以便正确解析 URL
		decoded := strings.ReplaceAll(val, "&amp;", "&")
		ref, err := url.Parse(decoded)
		if err != nil {
			return ""
		}
		return base.ResolveReference(ref).String()
	}

	// 1. src/href/poster 属性（双引号）
	attrDQ := regexp.MustCompile(`(\s(?:src|href|poster))="([^"]*)"`)
	html = attrDQ.ReplaceAllStringFunc(html, func(match string) string {
		sub := attrDQ.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		resolved := resolve(sub[2])
		if resolved == "" {
			return match
		}
		return sub[1] + `="` + resolved + `"`
	})

	// 2. src/href/poster 属性（单引号）
	attrSQ := regexp.MustCompile(`(\s(?:src|href|poster))='([^']*)'`)
	html = attrSQ.ReplaceAllStringFunc(html, func(match string) string {
		sub := attrSQ.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		resolved := resolve(sub[2])
		if resolved == "" {
			return match
		}
		return sub[1] + `='` + resolved + `'`
	})

	// 3. url() 中的相对路径
	urlRe := regexp.MustCompile(`url\(["']?([^"')]+)["']?\)`)
	html = urlRe.ReplaceAllStringFunc(html, func(match string) string {
		sub := urlRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		resolved := resolve(sub[1])
		if resolved == "" {
			return match
		}
		return `url("` + resolved + `")`
	})

	// 4. srcset 中的相对路径（多值，逗号分隔）
	srcsetRe := regexp.MustCompile(`(?i)(\s(?:image)?srcset)="([^"]+)"`)
	html = srcsetRe.ReplaceAllStringFunc(html, func(match string) string {
		sub := srcsetRe.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		attr, value := sub[1], sub[2]
		changed := false
		parts := strings.Split(value, ",")
		for i, part := range parts {
			fields := strings.Fields(strings.TrimSpace(part))
			if len(fields) == 0 {
				continue
			}
			if resolved := resolve(fields[0]); resolved != "" {
				fields[0] = resolved
				parts[i] = " " + strings.Join(fields, " ")
				changed = true
			}
		}
		if !changed {
			return match
		}
		return attr + `="` + strings.TrimLeft(strings.Join(parts, ","), " ") + `"`
	})

	return html
}

// RewriteHTMLFast 使用 strings.NewReplacer 快速重写 HTML 中的资源 URL
// 前置条件：HTML 应先经过 ResolveRelativeURLs 预处理，将相对路径转为绝对 URL
func (r *URLRewriter) RewriteHTMLFast(html string) string {
	var pairs []string

	for originalURL := range r.urlToLocalPath {
		var localURL string
		if r.pageID > 0 && r.timestamp != "" {
			localURL = fmt.Sprintf("/archive/%d/%smp_/%s", r.pageID, r.timestamp, originalURL)
		} else {
			localURL = "/archive/" + r.urlToLocalPath[originalURL]
		}

		// 1. 绝对 URL
		pairs = appendURLPairs(pairs, originalURL, localURL)

		// 2. 协议相对 URL（//example.com/path）
		protocolRelativeURL := strings.TrimPrefix(originalURL, "https:")
		protocolRelativeURL = strings.TrimPrefix(protocolRelativeURL, "http:")
		if protocolRelativeURL != originalURL && strings.HasPrefix(protocolRelativeURL, "//") {
			pairs = appendURLPairs(pairs, protocolRelativeURL, localURL)
		}

		// 3. &amp; 编码变体
		htmlEncodedURL := strings.ReplaceAll(originalURL, "&", "&amp;")
		if htmlEncodedURL != originalURL {
			pairs = appendURLPairs(pairs, htmlEncodedURL, localURL)

			if protocolRelativeURL != originalURL && strings.HasPrefix(protocolRelativeURL, "//") {
				protoRelEncoded := strings.ReplaceAll(protocolRelativeURL, "&", "&amp;")
				pairs = appendURLPairs(pairs, protoRelEncoded, localURL)
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

		// 5. 绝对路径（/assets/style.css）
		parsed, err := url.Parse(originalURL)
		if err == nil && parsed.Path != "" {
			pathWithQuery := parsed.Path
			if parsed.RawQuery != "" {
				pathWithQuery = parsed.Path + "?" + parsed.RawQuery
			}
			pairs = appendURLPairs(pairs, pathWithQuery, localURL)

			// 5a. 绝对路径的 &amp; 编码变体
			pathEncoded := strings.ReplaceAll(pathWithQuery, "&", "&amp;")
			if pathEncoded != pathWithQuery {
				pairs = appendURLPairs(pairs, pathEncoded, localURL)
			}
		}
	}

	replacer := strings.NewReplacer(pairs...)
	html = replacer.Replace(html)

	// 6. 多值 srcset/imagesrcset
	html = r.rewriteMultiValueSrcset(html)

	// 7. 兜底：未映射的绝对路径和协议相对 URL
	if r.pageID > 0 && r.timestamp != "" {
		html = r.rewriteUnmappedAbsolutePaths(html)
	}

	return html
}

// rewriteUnmappedAbsolutePaths 重写所有未被映射的绝对路径
// 这是一个兜底机制，用于处理动态生成或遗漏的资源引用
func (r *URLRewriter) rewriteUnmappedAbsolutePaths(html string) string {
	if r.baseURL == "" {
		return html
	}

	parsed, err := url.Parse(r.baseURL)
	if err != nil {
		return html
	}
	baseHost := parsed.Scheme + "://" + parsed.Host

	attrDQ := regexp.MustCompile(`(\s(?:src|href|poster|srcset))="(/[^"/][^"]*)"`)
	attrSQ := regexp.MustCompile(`(\s(?:src|href|poster|srcset))='(/[^'/][^']*)'`)
	protoRelDQ := regexp.MustCompile(`(\s(?:src|href|poster|srcset))="(//[^"]+)"`)
	protoRelSQ := regexp.MustCompile(`(\s(?:src|href|poster|srcset))='(//[^']+)'`)
	urlRe := regexp.MustCompile(`url\(["']?(/[^"')]+)["']?\)`)

	rewriteAttr := func(re *regexp.Regexp, quote string, buildURL func(string) string) string {
		return re.ReplaceAllStringFunc(html, func(match string) string {
			sub := re.FindStringSubmatch(match)
			if len(sub) < 3 {
				return match
			}
			attr, p := sub[1], sub[2]
			if strings.HasPrefix(p, "/archive/") || strings.HasPrefix(p, "//archive/") {
				return match
			}
			return attr + `=` + quote + buildURL(p) + quote
		})
	}

	// 协议相对 URL
	html = rewriteAttr(protoRelDQ, `"`, func(p string) string {
		return fmt.Sprintf("/archive/%d/%smp_/https:%s", r.pageID, r.timestamp, p)
	})
	html = rewriteAttr(protoRelSQ, `'`, func(p string) string {
		return fmt.Sprintf("/archive/%d/%smp_/https:%s", r.pageID, r.timestamp, p)
	})

	// 绝对路径
	html = rewriteAttr(attrDQ, `"`, func(p string) string {
		return fmt.Sprintf("/archive/%d/%smp_/%s", r.pageID, r.timestamp, baseHost+p)
	})
	html = rewriteAttr(attrSQ, `'`, func(p string) string {
		return fmt.Sprintf("/archive/%d/%smp_/%s", r.pageID, r.timestamp, baseHost+p)
	})

	// url() 中的绝对路径
	html = urlRe.ReplaceAllStringFunc(html, func(match string) string {
		sub := urlRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		p := sub[1]
		if strings.HasPrefix(p, "/archive/") {
			return match
		}
		localURL := fmt.Sprintf("/archive/%d/%smp_/%s", r.pageID, r.timestamp, baseHost+p)
		return `url("` + localURL + `")`
	})

	return html
}

// rewriteMultiValueSrcset 重写 srcset 和 imagesrcset 属性中的多值 URL
func (r *URLRewriter) rewriteMultiValueSrcset(html string) string {
	urlMap := make(map[string]string)

	for originalURL := range r.urlToLocalPath {
		var localURL string
		if r.pageID > 0 && r.timestamp != "" {
			localURL = fmt.Sprintf("/archive/%d/%smp_/%s", r.pageID, r.timestamp, originalURL)
		} else {
			localURL = "/archive/" + r.urlToLocalPath[originalURL]
		}

		urlMap[originalURL] = localURL
		if enc := strings.ReplaceAll(originalURL, "&", "&amp;"); enc != originalURL {
			urlMap[enc] = localURL
		}
		pr := strings.TrimPrefix(strings.TrimPrefix(originalURL, "https:"), "http:")
		if pr != originalURL && strings.HasPrefix(pr, "//") {
			urlMap[pr] = localURL
			if enc := strings.ReplaceAll(pr, "&", "&amp;"); enc != pr {
				urlMap[enc] = localURL
			}
		}
		parsed, err := url.Parse(originalURL)
		if err == nil && parsed.Path != "" {
			pq := parsed.Path
			if parsed.RawQuery != "" {
				pq = parsed.Path + "?" + parsed.RawQuery
			}
			urlMap[pq] = localURL
			if enc := strings.ReplaceAll(pq, "&", "&amp;"); enc != pq {
				urlMap[enc] = localURL
			}
		}
	}

	srcsetDQ := regexp.MustCompile(`(?i)((?:image)?srcset)="([^"]+)"`)
	html = srcsetDQ.ReplaceAllStringFunc(html, func(match string) string {
		sub := srcsetDQ.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		rewritten := rewriteSrcsetValue(sub[2], urlMap)
		if rewritten == sub[2] {
			return match
		}
		return sub[1] + `="` + rewritten + `"`
	})
	srcsetSQ := regexp.MustCompile(`(?i)((?:image)?srcset)='([^']+)'`)
	return srcsetSQ.ReplaceAllStringFunc(html, func(match string) string {
		sub := srcsetSQ.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		rewritten := rewriteSrcsetValue(sub[2], urlMap)
		if rewritten == sub[2] {
			return match
		}
		return sub[1] + `='` + rewritten + `'`
	})
}

// rewriteSrcsetValue 重写 srcset 属性值中的各个 URL
func rewriteSrcsetValue(value string, urlMap map[string]string) string {
	changed := false
	parts := strings.Split(value, ",")
	for i, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) == 0 {
			continue
		}
		if local, ok := urlMap[fields[0]]; ok {
			fields[0] = local
			parts[i] = " " + strings.Join(fields, " ")
			changed = true
		}
	}
	if !changed {
		return value
	}
	return strings.TrimLeft(strings.Join(parts, ","), " ")
}
