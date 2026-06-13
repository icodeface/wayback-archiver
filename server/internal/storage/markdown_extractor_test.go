package storage

import (
	"strings"
	"testing"
)

func TestExtractMarkdown_Headings(t *testing.T) {
	html := `<html><body><h1>Title</h1><h2>Subtitle</h2><h3>Section</h3></body></html>`
	md := ExtractMarkdown(html)
	if !strings.Contains(md, "# Title") {
		t.Errorf("Expected '# Title', got:\n%s", md)
	}
	if !strings.Contains(md, "## Subtitle") {
		t.Errorf("Expected '## Subtitle', got:\n%s", md)
	}
	if !strings.Contains(md, "### Section") {
		t.Errorf("Expected '### Section', got:\n%s", md)
	}
}

func TestExtractMarkdown_Paragraph(t *testing.T) {
	html := `<html><body><p>Hello world</p><p>Second paragraph</p></body></html>`
	md := ExtractMarkdown(html)
	if !strings.Contains(md, "Hello world") {
		t.Errorf("Expected 'Hello world', got:\n%s", md)
	}
	if !strings.Contains(md, "Second paragraph") {
		t.Errorf("Expected 'Second paragraph', got:\n%s", md)
	}
}

func TestExtractMarkdown_InlineFormatting(t *testing.T) {
	html := `<html><body><p><strong>bold</strong> and <em>italic</em> and <del>deleted</del></p></body></html>`
	md := ExtractMarkdown(html)
	if !strings.Contains(md, "**bold**") {
		t.Errorf("Expected '**bold**', got:\n%s", md)
	}
	if !strings.Contains(md, "*italic*") {
		t.Errorf("Expected '*italic*', got:\n%s", md)
	}
	if !strings.Contains(md, "~~deleted~~") {
		t.Errorf("Expected '~~deleted~~', got:\n%s", md)
	}
}

func TestExtractMarkdown_Links(t *testing.T) {
	html := `<html><body><p><a href="https://example.com">click here</a></p></body></html>`
	md := ExtractMarkdown(html)
	if !strings.Contains(md, "[click here](https://example.com)") {
		t.Errorf("Expected markdown link, got:\n%s", md)
	}
}

func TestExtractMarkdown_Image(t *testing.T) {
	html := `<html><body><img src="/img/photo.jpg" alt="A photo"></body></html>`
	md := ExtractMarkdown(html)
	if !strings.Contains(md, "![A photo](/img/photo.jpg)") {
		t.Errorf("Expected markdown image, got:\n%s", md)
	}
}

func TestExtractMarkdown_Code(t *testing.T) {
	html := `<html><body><p>Use <code>fmt.Println</code> to print</p></body></html>`
	md := ExtractMarkdown(html)
	if !strings.Contains(md, "`fmt.Println`") {
		t.Errorf("Expected inline code, got:\n%s", md)
	}
}

func TestExtractMarkdown_PreBlock(t *testing.T) {
	html := "<html><body><pre><code>func main() {\n  fmt.Println(\"hello\")\n}</code></pre></body></html>"
	md := ExtractMarkdown(html)
	if !strings.Contains(md, "func main()") {
		t.Errorf("Expected code block content, got:\n%s", md)
	}
}

func TestExtractMarkdown_UnorderedList(t *testing.T) {
	html := `<html><body><ul><li>Apple</li><li>Banana</li><li>Cherry</li></ul></body></html>`
	md := ExtractMarkdown(html)
	if !strings.Contains(md, "Apple") {
		t.Errorf("Expected 'Apple', got:\n%s", md)
	}
	if !strings.Contains(md, "Banana") {
		t.Errorf("Expected 'Banana', got:\n%s", md)
	}
}

func TestExtractMarkdown_OrderedList(t *testing.T) {
	html := `<html><body><ol><li>First</li><li>Second</li><li>Third</li></ol></body></html>`
	md := ExtractMarkdown(html)
	if !strings.Contains(md, "First") {
		t.Errorf("Expected 'First', got:\n%s", md)
	}
	if !strings.Contains(md, "Second") {
		t.Errorf("Expected 'Second', got:\n%s", md)
	}
}

func TestExtractMarkdown_Blockquote(t *testing.T) {
	html := `<html><body><blockquote><p>A wise quote</p></blockquote></body></html>`
	md := ExtractMarkdown(html)
	if !strings.Contains(md, "> A wise quote") {
		t.Errorf("Expected blockquote, got:\n%s", md)
	}
}

func TestExtractMarkdown_Table(t *testing.T) {
	html := `<html><body><table><tr><th>Name</th><th>Age</th></tr><tr><td>Alice</td><td>30</td></tr></table></body></html>`
	md := ExtractMarkdown(html)
	if !strings.Contains(md, "Name") || !strings.Contains(md, "Age") {
		t.Errorf("Expected table header content, got:\n%s", md)
	}
	if !strings.Contains(md, "Alice") || !strings.Contains(md, "30") {
		t.Errorf("Expected table row content, got:\n%s", md)
	}
	if !strings.Contains(md, "|") {
		t.Errorf("Expected markdown table pipe syntax, got:\n%s", md)
	}
}

func TestExtractMarkdown_SkipScript(t *testing.T) {
	html := `<html><body><p>visible</p><script>alert('xss')</script></body></html>`
	md := ExtractMarkdown(html)
	if strings.Contains(md, "alert") {
		t.Errorf("Should skip script content, got:\n%s", md)
	}
	if !strings.Contains(md, "visible") {
		t.Errorf("Should keep visible content, got:\n%s", md)
	}
}

func TestExtractMarkdown_SkipNav(t *testing.T) {
	html := `<html><body><nav><a href="/">Home</a><a href="/about">About</a></nav><article><p>Main content</p></article></body></html>`
	md := ExtractMarkdown(html)
	if strings.Contains(md, "Home") {
		t.Errorf("Should skip nav content, got:\n%s", md)
	}
	if !strings.Contains(md, "Main content") {
		t.Errorf("Should keep article content, got:\n%s", md)
	}
}

func TestExtractMarkdown_CollapseBlankLines(t *testing.T) {
	html := `<html><body><p>A</p><p></p><p></p><p></p><p>B</p></body></html>`
	md := ExtractMarkdown(html)
	if strings.Contains(md, "\n\n\n") {
		t.Errorf("Should collapse blank lines, got:\n%q", md)
	}
}

func TestExtractMarkdown_HrBr(t *testing.T) {
	html := `<html><body><p>Line1<br>Line2</p><hr><p>After</p></body></html>`
	md := ExtractMarkdown(html)
	if !strings.Contains(md, "Line1") || !strings.Contains(md, "Line2") {
		t.Errorf("Expected br content, got:\n%s", md)
	}
	if !strings.Contains(md, "After") {
		t.Errorf("Expected content after hr, got:\n%s", md)
	}
}

func TestExtractMarkdown_SkipFooter(t *testing.T) {
	html := `<html><body><main><p>Content</p></main><footer><p>Copyright 2024</p></footer></body></html>`
	md := ExtractMarkdown(html)
	if strings.Contains(md, "Copyright") {
		t.Errorf("Should skip footer, got:\n%s", md)
	}
}

func TestExtractMarkdown_PrefersSemanticArticle(t *testing.T) {
	html := `<html><body>
		<header>Top navigation</header>
		<main>
			<aside>Related links</aside>
			<article>
				<h1>Article Title</h1>
				<p>This is the saved article body.</p>
			</article>
		</main>
	</body></html>`
	md := ExtractMarkdown(html)
	if !strings.Contains(md, "# Article Title") || !strings.Contains(md, "saved article body") {
		t.Errorf("Should keep article content, got:\n%s", md)
	}
	if strings.Contains(md, "Top navigation") || strings.Contains(md, "Related links") {
		t.Errorf("Should skip page chrome around article, got:\n%s", md)
	}
}

func TestExtractMarkdown_SelectsNonSemanticMainContent(t *testing.T) {
	html := `<html><body>
		<div id="Top"><a href="/">Home</a><a href="/settings">Settings</a></div>
		<div id="Rightbar">
			<p>Promoted product should not be selected as the page body.</p>
			<p>Related link one. Related link two. Related link three.</p>
		</div>
		<div id="Main">
			<h1>Thread Title</h1>
			<div class="topic_content">
				<p>This is the original post with enough text to be selected as readable content.</p>
				<p>It should keep the main discussion and ignore sidebars around it.</p>
			</div>
			<div class="reply_content">A useful reply that is part of the captured discussion.</div>
		</div>
	</body></html>`

	md := ExtractMarkdown(html)
	if !strings.Contains(md, "# Thread Title") || !strings.Contains(md, "useful reply") {
		t.Errorf("Should keep non-semantic main content, got:\n%s", md)
	}
	if strings.Contains(md, "Promoted product") || strings.Contains(md, "Settings") {
		t.Errorf("Should skip page chrome around non-semantic main content, got:\n%s", md)
	}
}

func TestExtractMarkdown_RemovesHiddenStyleContent(t *testing.T) {
	html := `<html><body>
		<main>
			<p>Visible article text.</p>
			<div style="display: none"><p>Hidden promo text.</p></div>
			<div style="visibility:hidden"><p>Invisible tracking text.</p></div>
		</main>
	</body></html>`

	md := ExtractMarkdown(html)
	if !strings.Contains(md, "Visible article text") {
		t.Errorf("Should keep visible text, got:\n%s", md)
	}
	if strings.Contains(md, "Hidden promo") || strings.Contains(md, "Invisible tracking") {
		t.Errorf("Should skip hidden style content, got:\n%s", md)
	}
}

func TestExtractMarkdown_KeepsOpacityZeroAnimatedContent(t *testing.T) {
	html := `<html><body>
		<main>
			<h1>Animated Page</h1>
			<div style="opacity: 0; transform: translateY(50px)">
				<p>Content waiting for a scroll animation should still be readable.</p>
			</div>
		</main>
	</body></html>`

	md := ExtractMarkdown(html)
	if !strings.Contains(md, "scroll animation should still be readable") {
		t.Errorf("Should keep opacity-zero animated content, got:\n%s", md)
	}
}

func TestExtractMarkdown_DemotesLayoutTables(t *testing.T) {
	html := `<html><body>
		<main>
			<h1>Discussion</h1>
			<table cellpadding="0" cellspacing="0" border="0">
				<tr>
					<td><img src="/avatar.png" alt="Alice"></td>
					<td><a href="/member/alice">Alice</a></td>
					<td><div class="reply_content">This table is only used for layout.</div></td>
				</tr>
			</table>
		</main>
	</body></html>`

	md := ExtractMarkdown(html)
	if !strings.Contains(md, "This table is only used for layout") {
		t.Errorf("Should keep layout table text, got:\n%s", md)
	}
	if strings.Contains(md, "|") {
		t.Errorf("Should not render layout table as Markdown table, got:\n%s", md)
	}
}

func TestExtractMarkdown_KeepsSemanticTables(t *testing.T) {
	html := `<html><body>
		<main>
			<table>
				<thead><tr><th>Name</th><th>Score</th></tr></thead>
				<tbody><tr><td>Alice</td><td>10</td></tr></tbody>
			</table>
		</main>
	</body></html>`

	md := ExtractMarkdown(html)
	if !strings.Contains(md, "|") || !strings.Contains(md, "Alice") {
		t.Errorf("Should keep semantic table markdown, got:\n%s", md)
	}
}

func TestExtractMarkdown_RemovesInteractiveChrome(t *testing.T) {
	html := `<html><body>
		<form><input value="search"><button>Search</button></form>
		<template><p>Hidden template text</p></template>
		<p>Readable fallback body.</p>
	</body></html>`
	md := ExtractMarkdown(html)
	if !strings.Contains(md, "Readable fallback body") {
		t.Errorf("Should keep readable body, got:\n%s", md)
	}
	if strings.Contains(md, "Search") || strings.Contains(md, "Hidden template text") {
		t.Errorf("Should skip interactive chrome, got:\n%s", md)
	}
}

func TestExtractMarkdown_EmptyHTML(t *testing.T) {
	md := ExtractMarkdown("")
	if md != "\n" {
		t.Errorf("Expected single newline for empty input, got: %q", md)
	}
}
