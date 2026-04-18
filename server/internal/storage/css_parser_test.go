package storage

import "testing"

func TestCSSParserExtractResources_SkipsFragmentOnlyURLs(t *testing.T) {
	parser := NewCSSParser()
	resources := parser.ExtractResources(`.mask{filter:url(#goo)} .bg{background:url("icons.svg#sprite")} @import url("theme.css")`)

	for _, resource := range resources {
		if resource == "#goo" {
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
