package storage

import "testing"

func TestCSSParserExtractResources_SkipsFragmentOnlyURLs(t *testing.T) {
	parser := NewCSSParser()
	resources := parser.ExtractResources(`.mask{filter:url(#goo)} .quoted{filter:url("#paint")} .bg{background:url("icons.svg#sprite")} @import url("theme.css")`)

	for _, resource := range resources {
		if resource == "#goo" || resource == "#paint" {
			t.Fatal("fragment-only CSS url() should not be extracted")
		}
	}

	if len(resources) != 2 {
		t.Fatalf("expected 2 extracted resources, got %d: %#v", len(resources), resources)
	}

	if resources[0] != "theme.css" && resources[1] != "theme.css" {
		t.Fatalf("expected theme.css to be preserved, got %#v", resources)
	}

	if resources[0] != "icons.svg#sprite" && resources[1] != "icons.svg#sprite" {
		t.Fatalf("expected icons.svg#sprite to be preserved, got %#v", resources)
	}
}

func TestCSSParserExtractResources_SkipsEncodedFragmentOnlyURLs(t *testing.T) {
	parser := NewCSSParser()
	resources := parser.ExtractResources(`.mask{filter:url(%23clip)} .quoted{filter:url(" %23paint ")} .bg{background:url("icons.svg#sprite")} .encoded{background:url("icons.svg%23sprite")}`)

	if len(resources) != 2 {
		t.Fatalf("expected only the external SVG sprites, got %#v", resources)
	}

	foundPlainFragment := false
	foundEncodedFragment := false
	for _, resource := range resources {
		switch resource {
		case "icons.svg#sprite":
			foundPlainFragment = true
		case "icons.svg%23sprite":
			foundEncodedFragment = true
		case "%23clip", "%23paint":
			t.Fatalf("encoded fragment-only CSS url() should not be extracted: %#v", resources)
		}
	}
	if !foundPlainFragment || !foundEncodedFragment {
		t.Fatalf("expected external SVG fragments to be preserved, got %#v", resources)
	}
}

func TestCSSParserExtractResources_SkipsDataURLNestedFragmentReferences(t *testing.T) {
	parser := NewCSSParser()
	resources := parser.ExtractResources(`.icon{background-image:url("data:image/svg+xml,%3Csvg%3E%3Ccircle mask='url(%23clip)'/%3E%3C/svg%3E")}`)

	if len(resources) != 0 {
		t.Fatalf("data URL internals should not be extracted as resources, got %#v", resources)
	}
}

func TestCSSParserExtractResources_SkipsUnsupportedSchemes(t *testing.T) {
	parser := NewCSSParser()
	resources := parser.ExtractResources(`.blob{background:url(BLOB:https://example.com/id)} .js{background:url(" JAVASCRIPT:noop ")} .mail{background:url(mailto:a@example.com)} .about{background:url(about:blank)} .img{background:url(/img.png)}`)

	if len(resources) != 1 {
		t.Fatalf("expected only the downloadable image URL, got %#v", resources)
	}
	if resources[0] != "/img.png" {
		t.Fatalf("expected /img.png to be preserved, got %#v", resources)
	}
}
