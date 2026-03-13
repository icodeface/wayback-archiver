package api

import (
	"fmt"
	"net/url"
	"regexp"

	"wayback/internal/models"
)

// injectArchiveHeader 在页面顶部注入归档信息栏
func injectArchiveHeader(html string, page *models.Page, prev *models.Page, next *models.Page, snapshotTotal int, nonce string) string {
	// 格式化时间
	capturedTime := page.CapturedAt.Format("2006-01-02 15:04:05")

	// 构建快照导航 HTML
	var navHTML string
	if snapshotTotal > 1 {
		prevLink := ""
		if prev != nil {
			prevLink = fmt.Sprintf(`<a href="/view/%d" style="color:white;text-decoration:none;padding:4px 10px;border:1px solid rgba(255,255,255,0.3);border-radius:3px;font-size:12px;background:rgba(255,255,255,0.1);" title="%s">◀ %s</a>`,
				prev.ID, prev.FirstVisited.Format("2006-01-02 15:04:05"), prev.FirstVisited.Format("01-02 15:04"))
		} else {
			prevLink = `<span style="padding:4px 10px;font-size:12px;opacity:0.3;">◀</span>`
		}

		nextLink := ""
		if next != nil {
			nextLink = fmt.Sprintf(`<a href="/view/%d" style="color:white;text-decoration:none;padding:4px 10px;border:1px solid rgba(255,255,255,0.3);border-radius:3px;font-size:12px;background:rgba(255,255,255,0.1);" title="%s">%s ▶</a>`,
				next.ID, next.FirstVisited.Format("2006-01-02 15:04:05"), next.FirstVisited.Format("01-02 15:04"))
		} else {
			nextLink = `<span style="padding:4px 10px;font-size:12px;opacity:0.3;">▶</span>`
		}

		timelineLink := fmt.Sprintf(`<a href="/timeline?url=%s" style="color:white;text-decoration:none;padding:4px 10px;border:1px solid rgba(255,255,255,0.3);border-radius:3px;font-size:12px;background:rgba(255,255,255,0.1);" title="查看所有快照">%d snapshots</a>`,
			url.QueryEscape(page.URL), snapshotTotal)

		navHTML = fmt.Sprintf(`
		<div style="display:flex;align-items:center;gap:6px;">
			%s %s %s
		</div>`, prevLink, timelineLink, nextLink)
	}

	// 归档信息栏 HTML
	archiveHeader := fmt.Sprintf(`
<div id="wayback-archive-header" style="
	position: fixed;
	top: 0;
	left: 0;
	right: 0;
	height: 48px;
	background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%);
	color: white;
	padding: 0 20px;
	box-shadow: 0 2px 8px rgba(0,0,0,0.15);
	z-index: 999999;
	font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
	font-size: 13px;
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 16px;
	overflow: hidden;
">
	<div style="display:flex;align-items:center;gap:12px;min-width:0;flex:1;overflow:hidden;">
		<span style="background:rgba(255,255,255,0.2);padding:3px 10px;border-radius:4px;font-size:11px;font-weight:600;letter-spacing:0.5px;white-space:nowrap;">📚 ARCHIVED</span>
		<a href="%s" style="color:white;text-decoration:none;font-family:monospace;font-size:12px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;min-width:0;opacity:0.95;" title="%s">%s</a>
		<span style="font-size:11px;opacity:0.7;white-space:nowrap;">%s</span>
	</div>
	<div style="display:flex;align-items:center;gap:8px;flex-shrink:0;">
		%s
		<a href="/" style="color:white;text-decoration:none;padding:4px 12px;border:1px solid rgba(255,255,255,0.3);border-radius:4px;font-size:12px;background:rgba(255,255,255,0.1);white-space:nowrap;">← Archives</a>
	</div>
</div>
<style>
	:root {
		--wayback-header-height: 48px;
	}
	body {
		margin-top: var(--wayback-header-height) !important;
		padding-top: 0 !important;
	}
	/* 将页面自身的 fixed/sticky 顶部元素下移，避免被归档 header 遮挡 */
	/* 针对内联样式定义的 fixed/sticky 元素 */
	[style*="position: fixed"][style*="top: 0"]:not(#wayback-archive-header),
	[style*="position:fixed"][style*="top: 0"]:not(#wayback-archive-header),
	[style*="position: fixed"][style*="top:0"]:not(#wayback-archive-header),
	[style*="position:fixed"][style*="top:0"]:not(#wayback-archive-header),
	[style*="position: sticky"][style*="top: 0"]:not(#wayback-archive-header),
	[style*="position:sticky"][style*="top: 0"]:not(#wayback-archive-header),
	[style*="position: sticky"][style*="top:0"]:not(#wayback-archive-header),
	[style*="position:sticky"][style*="top:0"]:not(#wayback-archive-header) {
		top: var(--wayback-header-height) !important;
	}
	/* SPA 框架常用 height:100%% 配合 JS 滚动，静态模式下会截断内容 */
	html, body, #app, #root, #__next, #__nuxt {
		height: auto !important;
		min-height: 100%% !important;
		overflow: visible !important;
	}
	/* 修复 flex 容器 height:100%% 截断内容（:has 排除空容器，避免撑开广告占位） */
	#app > div:has(> *:not(a):not(br):not(hr)), #root > div:has(> *:not(a):not(br):not(hr)),
	#__next > div:has(> *:not(a):not(br):not(hr)), #__nuxt > div:has(> *:not(a):not(br):not(hr)) {
		height: auto !important;
		min-height: 100%% !important;
	}
	/* 修复 vue-recycle-scroller 虚拟滚动：移除 transform 定位，让 items 自然排列 */
	.vue-recycle-scroller__item-wrapper {
		min-height: 0 !important;
	}
	.vue-recycle-scroller__item-view {
		position: static !important;
		transform: none !important;
	}
</style>
<script nonce="%s">
(function() {
	'use strict';
	// 修复所有 fixed/sticky 定位的顶部元素，避免被归档 header 遮挡
	function fixPositionedElements() {
		const HEADER_HEIGHT = 48;
		const elements = document.querySelectorAll('*:not(#wayback-archive-header):not(#wayback-archive-header *)');

		elements.forEach(function(el) {
			const style = window.getComputedStyle(el);
			const position = style.position;
			const top = style.top;

			// 检查是否是 fixed 或 sticky 定位，且 top 为 0 或接近 0
			if ((position === 'fixed' || position === 'sticky') &&
			    (top === '0px' || top === '0' || parseInt(top) === 0)) {
				// 设置新的 top 值，避免被归档 header 遮挡
				el.style.setProperty('top', HEADER_HEIGHT + 'px', 'important');
			}
		});
	}

	// 页面加载完成后执行
	if (document.readyState === 'loading') {
		document.addEventListener('DOMContentLoaded', fixPositionedElements);
	} else {
		fixPositionedElements();
	}

	// 延迟执行一次，确保动态加载的元素也被处理
	setTimeout(fixPositionedElements, 100);
	setTimeout(fixPositionedElements, 500);

})();
</script>
`, page.URL, escapeHTML(page.URL), escapeHTML(page.URL), capturedTime, navHTML, nonce)

	// 在 <body> 标签后注入
	if bodyTagRe.MatchString(html) {
		html = bodyTagRe.ReplaceAllString(html, "$1"+archiveHeader)
	} else {
		// 如果没有 <body> 标签，在开头注入
		html = archiveHeader + html
	}

	return html
}

// removeExternalResources 移除HTML中的外部资源引用
func removeExternalResources(html string) string {
	// 移除外部字体预连接和DNS预取
	preconnectRe := regexp.MustCompile(`(?i)<link[^>]*rel=["'](preconnect|dns-prefetch)["'][^>]*>`)
	html = preconnectRe.ReplaceAllString(html, "")

	// 移除外部字体链接（匹配常见的字体CDN）
	externalFontRe := regexp.MustCompile(`(?i)<link[^>]*href=["']https?://[^"']*\.(googleapis\.com|gstatic\.com|fonts\.net|typekit\.net)[^"']*["'][^>]*>`)
	html = externalFontRe.ReplaceAllString(html, "")

	// 移除外部CSS CDN链接（匹配常见的CDN域名）
	externalCSSRe := regexp.MustCompile(`(?i)<link[^>]*rel=["']stylesheet["'][^>]*href=["']https?://[^"']*(cdn\.|cloudflare\.|jsdelivr\.|unpkg\.|cdnjs\.)[^"']*["'][^>]*>`)
	html = externalCSSRe.ReplaceAllString(html, "")

	// 移除外部script CDN
	externalScriptRe := regexp.MustCompile(`(?i)<script[^>]*src=["']https?://[^"']*(cdn\.|cloudflare\.|jsdelivr\.|unpkg\.|cdnjs\.)[^"']*["'][^>]*>.*?</script>`)
	html = externalScriptRe.ReplaceAllString(html, "")

	// 移除CSS中的外部@import（匹配http/https开头的）
	externalImportRe := regexp.MustCompile(`(?i)@import\s+url\(["']?https?://[^"')]*["']?\);?`)
	html = externalImportRe.ReplaceAllString(html, "")

	return html
}

// injectAntiRefreshScript 注入脚本来阻止页面刷新和导航
func injectAntiRefreshScript(html string) string {
	// 防刷新脚本 - 基于研究文档的最佳实践
	antiRefreshScript := `
<script>
(function() {
	'use strict';
	console.log('[Wayback] Anti-refresh protection loading...');

	// 1. 阻止所有导航
	if (window.history && window.history.pushState) {
		const originalPushState = window.history.pushState;
		const originalReplaceState = window.history.replaceState;

		window.history.pushState = function() {
			console.log('[Wayback] Blocked history.pushState');
			return;
		};

		window.history.replaceState = function() {
			console.log('[Wayback] Blocked history.replaceState');
			return;
		};
	}

	// 2. 阻止 location 修改
	try {
		Object.defineProperty(window, 'location', {
			get: function() { return document.location; },
			set: function(val) {
				console.log('[Wayback] Blocked location change to:', val);
				return document.location;
			}
		});
	} catch(e) {
		console.log('[Wayback] Could not override location:', e);
	}

	// 3. 阻止表单提交
	document.addEventListener('submit', function(e) {
		console.log('[Wayback] Blocked form submission');
		e.preventDefault();
		e.stopPropagation();
		return false;
	}, true);

	// 4. 阻止所有链接点击
	document.addEventListener('click', function(e) {
		let target = e.target;
		while (target && target.tagName !== 'A') {
			target = target.parentElement;
		}
		if (target && target.tagName === 'A') {
			const href = target.getAttribute('href');
			if (href && href !== '#' && !href.startsWith('javascript:')) {
				console.log('[Wayback] Blocked link navigation to:', href);
				e.preventDefault();
				e.stopPropagation();
				return false;
			}
		}
	}, true);

	// 5. 移除 meta refresh
	const metaTags = document.querySelectorAll('meta[http-equiv="refresh"]');
	metaTags.forEach(function(tag) {
		console.log('[Wayback] Removed meta refresh tag');
		tag.remove();
	});

	// 6. 阻止 window.open
	window.open = function() {
		console.log('[Wayback] Blocked window.open');
		return null;
	};

	console.log('[Wayback] Anti-refresh protection enabled');
})();
</script>
`

	// 在 <html> 标签后立即注入（确保最先执行）
	if htmlTagRe.MatchString(html) {
		html = htmlTagRe.ReplaceAllString(html, "$1"+antiRefreshScript)
	} else {
		// 如果没有 <html> 标签，在 <head> 后注入
		if headTagRe.MatchString(html) {
			html = headTagRe.ReplaceAllString(html, "$1"+antiRefreshScript)
		} else {
			// 如果连 <head> 都没有，直接在开头注入
			html = antiRefreshScript + html
		}
	}

	return html
}
