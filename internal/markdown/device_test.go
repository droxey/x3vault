package markdown

import (
	"strings"
	"testing"
)

func TestFormatForXTEReaderTitleAndTags(t *testing.T) {
	out := FormatForXTEReader("Memex", []string{"history", "tools"}, "Body text.")
	if !strings.HasPrefix(out, "# Memex\n\nBody text.") {
		t.Fatalf("body = %q", out)
	}
	if !strings.Contains(out, "*Tags: history, tools*") {
		t.Fatalf("tags footer missing: %q", out)
	}
}

func TestFormatForXTEReaderStripsObsidianSyntax(t *testing.T) {
	in := "Hello ==highlight== text ^block-id\n<!-- secret -->"
	out := FormatForXTEReader("", nil, in)
	if strings.Contains(out, "==") || strings.Contains(out, "^block-id") || strings.Contains(out, "<!--") {
		t.Fatalf("obsidian syntax not stripped: %q", out)
	}
	if !strings.Contains(out, "**highlight**") {
		t.Fatalf("highlight not converted: %q", out)
	}
}

func TestImageMarkdownUnsupportedOnXTE(t *testing.T) {
	var warnings []string
	out := &NormalizedNote{Warnings: warnings}
	got := imageMarkdown("chart", "assets/ab12/chart.svg", ".svg", out)
	if strings.HasPrefix(got, "!") {
		t.Fatalf("expected link for svg, got %q", got)
	}
	if len(out.Warnings) != 1 {
		t.Fatalf("warnings = %v", out.Warnings)
	}
}

func TestImageMarkdownSupportedOnXTE(t *testing.T) {
	got := imageMarkdown("pic", "assets/ab12/pic.png", ".png", &NormalizedNote{})
	if !strings.HasPrefix(got, "![pic]") {
		t.Fatalf("got %q", got)
	}
}
