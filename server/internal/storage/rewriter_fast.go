package storage

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
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

			// 5a. 绝对路径的 &amp; 编码变体（HTML 属性中 & 常被编码为 &amp;）
			pathWithQueryEncoded := strings.ReplaceAll(pathWithQuery, "&", "&amp;")
			if pathWithQueryEncoded != pathWithQuery {
				pairs = append(pairs,
					` src="`+pathWithQueryEncoded+`"`, ` src="`+localURL+`"`,
					` href="`+pathWithQueryEncoded+`"`, ` href="`+localURL+`"`,
					` poster="`+pathWithQueryEncoded+`"`, ` poster="`+localURL+`"`,
					` srcset="`+pathWithQueryEncoded+`"`, ` srcset="`+localURL+`"`,
					`url("`+pathWithQueryEncoded+`")`, `url("`+localURL+`")`,
					`url('`+pathWithQueryEncoded+`')`, `url('`+localURL+`')`,
					`url(`+pathWithQueryEncoded+`)`, `url(`+localURL+`)`,
				)
			}

			// 5b. 相对路径变体（如 ./style.css 和 style.css）
			// 从绝对路径中提取文件名部分，生成相对路径变体
			fileName := path.Base(parsed.Path)
			if fileName != "" && fileName != "." && fileName != "/" {
				// 带 ./ 前缀的相对路径
				relWithQuery := "./" + fileName
				// 裸文件名（不带 ./ 前缀）
				bareWithQuery := fileName
				if parsed.RawQuery != "" {
					relWithQuery = "./" + fileName + "?" + parsed.RawQuery
					bareWithQuery = fileName + "?" + parsed.RawQuery
				}
				pairs = append(pairs,
					` src="`+relWithQuery+`"`, ` src="`+localURL+`"`,
					` href="`+relWithQuery+`"`, ` href="`+localURL+`"`,
					` poster="`+relWithQuery+`"`, ` poster="`+localURL+`"`,
					` srcset="`+relWithQuery+`"`, ` srcset="`+localURL+`"`,
					`url("`+relWithQuery+`")`, `url("`+localURL+`")`,
					`url('`+relWithQuery+`')`, `url('`+localURL+`')`,
					`url(`+relWithQuery+`)`, `url(`+localURL+`)`,
				)

				// 裸文件名变体（如 style.css，不带 ./ 前缀）
				if bareWithQuery != pathWithQuery {
					pairs = append(pairs,
						` src="`+bareWithQuery+`"`, ` src="`+localURL+`"`,
						` href="`+bareWithQuery+`"`, ` href="`+localURL+`"`,
						` poster="`+bareWithQuery+`"`, ` poster="`+localURL+`"`,
						` srcset="`+bareWithQuery+`"`, ` srcset="`+localURL+`"`,
						`url("`+bareWithQuery+`")`, `url("`+localURL+`")`,
						`url('`+bareWithQuery+`')`, `url('`+localURL+`')`,
						`url(`+bareWithQuery+`)`, `url(`+localURL+`)`,
					)
				}

				// 5c. &amp; 编码变体
				relEncoded := strings.ReplaceAll(relWithQuery, "&", "&amp;")
				if relEncoded != relWithQuery {
					pairs = append(pairs,
						` src="`+relEncoded+`"`, ` src="`+localURL+`"`,
						` href="`+relEncoded+`"`, ` href="`+localURL+`"`,
						` poster="`+relEncoded+`"`, ` poster="`+localURL+`"`,
						` srcset="`+relEncoded+`"`, ` srcset="`+localURL+`"`,
						`url("`+relEncoded+`")`, `url("`+localURL+`")`,
						`url('`+relEncoded+`')`, `url('`+localURL+`')`,
						`url(`+relEncoded+`)`, `url(`+localURL+`)`,
					)
				}
				bareEncoded := strings.ReplaceAll(bareWithQuery, "&", "&amp;")
				if bareEncoded != bareWithQuery {
					pairs = append(pairs,
						` src="`+bareEncoded+`"`, ` src="`+localURL+`"`,
						` href="`+bareEncoded+`"`, ` href="`+localURL+`"`,
						` poster="`+bareEncoded+`"`, ` poster="`+localURL+`"`,
						` srcset="`+bareEncoded+`"`, ` srcset="`+localURL+`"`,
						`url("`+bareEncoded+`")`, `url("`+localURL+`")`,
						`url('`+bareEncoded+`')`, `url('`+localURL+`')`,
						`url(`+bareEncoded+`)`, `url(`+localURL+`)`,
					)
				}
			}
		}
	}

	// 使用 strings.NewReplacer 做单次遍历替换
	replacer := strings.NewReplacer(pairs...)
	html = replacer.Replace(html)

	// 6. 处理多值 srcset/imagesrcset（如 srcset="url1 1x, url2 2x"）
	// strings.NewReplacer 只能匹配 srcset="<单个URL>"，无法处理逗号分隔的多值
	html = r.rewriteMultiValueSrcset(html)

	// 7. 兜底：重写所有未被替换的绝对路径（如 /assets/...）
	// 这些路径可能是动态生成的，或者在资源提取时被遗漏
	// 将它们重写为归档路径格式，让服务器的 view handler 处理
	if r.pageID > 0 && r.timestamp != "" {
		html = r.rewriteUnmappedAbsolutePaths(html)
	}

	return html
}

// rewriteUnmappedAbsolutePaths 重写所有未被映射的绝对路径
// 这是一个兜底机制，用于处理动态生成或遗漏的资源引用
func (r *URLRewriter) rewriteUnmappedAbsolutePaths(html string) string {
	// 必须有 baseURL 才能构建完整的归档路径
	if r.baseURL == "" {
		return html
	}

	parsed, err := url.Parse(r.baseURL)
	if err != nil {
		return html
	}
	baseHost := parsed.Scheme + "://" + parsed.Host

	// 预编译正则（避免在闭包内重复编译）
	// 绝对路径：以单个 / 开头，但不是 // 开头（协议相对 URL）
	attrDQ := regexp.MustCompile(`(\s(?:src|href|poster|srcset))="(/[^"/][^"]*)"`)
	attrSQ := regexp.MustCompile(`(\s(?:src|href|poster|srcset))='(/[^'/][^']*)'`)
	protoRelDQ := regexp.MustCompile(`(\s(?:src|href|poster|srcset))="(//[^"]+)"`)
	protoRelSQ := regexp.MustCompile(`(\s(?:src|href|poster|srcset))='(//[^']+)'`)
	urlRe := regexp.MustCompile(`url\(["']?(/[^"')]+)["']?\)`)

	// 1. 先处理协议相对 URL（如 //cdn.example.com/path）（双引号）
	html = protoRelDQ.ReplaceAllStringFunc(html, func(match string) string {
		sub := protoRelDQ.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		attr := sub[1]
		p := sub[2]
		if strings.HasPrefix(p, "//archive/") {
			return match
		}
		// 协议相对 URL 补全为 https
		localURL := fmt.Sprintf("/archive/%d/%smp_/https:%s", r.pageID, r.timestamp, p)
		return attr + `="` + localURL + `"`
	})

	// 1b. 协议相对 URL（单引号）
	html = protoRelSQ.ReplaceAllStringFunc(html, func(match string) string {
		sub := protoRelSQ.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		attr := sub[1]
		p := sub[2]
		if strings.HasPrefix(p, "//archive/") {
			return match
		}
		localURL := fmt.Sprintf("/archive/%d/%smp_/https:%s", r.pageID, r.timestamp, p)
		return attr + `='` + localURL + `'`
	})

	// 2. 再处理绝对路径（以单个 / 开头）（双引号）
	html = attrDQ.ReplaceAllStringFunc(html, func(match string) string {
		sub := attrDQ.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		attr := sub[1]
		p := sub[2]
		if strings.HasPrefix(p, "/archive/") {
			return match
		}
		localURL := fmt.Sprintf("/archive/%d/%smp_/%s", r.pageID, r.timestamp, baseHost+p)
		return attr + `="` + localURL + `"`
	})

	// 2b. 绝对路径（单引号）
	html = attrSQ.ReplaceAllStringFunc(html, func(match string) string {
		sub := attrSQ.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		attr := sub[1]
		p := sub[2]
		if strings.HasPrefix(p, "/archive/") {
			return match
		}
		localURL := fmt.Sprintf("/archive/%d/%smp_/%s", r.pageID, r.timestamp, baseHost+p)
		return attr + `='` + localURL + `'`
	})

	// 3. 重写 url() 中的绝对路径
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
	// 构建 URL 查找表：各种 URL 变体 -> 本地 URL
	urlMap := make(map[string]string)
	for originalURL := range r.urlToLocalPath {
		var localURL string
		if r.pageID > 0 && r.timestamp != "" {
			localURL = fmt.Sprintf("/archive/%d/%smp_/%s", r.pageID, r.timestamp, originalURL)
		} else {
			localURL = "/archive/" + r.urlToLocalPath[originalURL]
		}

		// 绝对 URL
		urlMap[originalURL] = localURL
		// &amp; 编码
		if enc := strings.ReplaceAll(originalURL, "&", "&amp;"); enc != originalURL {
			urlMap[enc] = localURL
		}
		// 协议相对
		pr := strings.TrimPrefix(strings.TrimPrefix(originalURL, "https:"), "http:")
		if pr != originalURL && strings.HasPrefix(pr, "//") {
			urlMap[pr] = localURL
			if enc := strings.ReplaceAll(pr, "&", "&amp;"); enc != pr {
				urlMap[enc] = localURL
			}
		}
		// 绝对路径 + query
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

	// 匹配 srcset="..." 和 imagesrcset="..."
	srcsetDQ := regexp.MustCompile(`(?i)((?:image)?srcset)="([^"]+)"`)
	html = srcsetDQ.ReplaceAllStringFunc(html, func(match string) string {
		sub := srcsetDQ.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		attrName := sub[1]
		value := sub[2]
		rewritten := rewriteSrcsetValue(value, urlMap)
		if rewritten == value {
			return match
		}
		return attrName + `="` + rewritten + `"`
	})
	srcsetSQ := regexp.MustCompile(`(?i)((?:image)?srcset)='([^']+)'`)
	return srcsetSQ.ReplaceAllStringFunc(html, func(match string) string {
		sub := srcsetSQ.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		attrName := sub[1]
		value := sub[2]
		rewritten := rewriteSrcsetValue(value, urlMap)
		if rewritten == value {
			return match
		}
		return attrName + `='` + rewritten + `'`
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
