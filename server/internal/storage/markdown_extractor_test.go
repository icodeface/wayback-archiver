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

func TestExtractMarkdown_EmptyHTML(t *testing.T) {
	md := ExtractMarkdown("")
	if md != "\n" {
		t.Errorf("Expected single newline for empty input, got: %q", md)
	}
}
