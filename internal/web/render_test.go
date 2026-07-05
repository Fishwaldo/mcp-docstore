package web

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderMarkdownRendersGFMTables(t *testing.T) {
	md := "| Setting | Value |\n|---|---|\n| Cores | 8 |\n"
	html, err := renderMarkdown(md)
	require.NoError(t, err)
	require.Contains(t, html, "<table")
	require.Contains(t, html, "<th")
	require.Contains(t, html, "<td")
	require.Contains(t, html, "Cores")
	require.NotContains(t, html, "|---|") // the raw table delimiter must not leak through
}

func TestRenderMarkdownRendersGFMStrikethrough(t *testing.T) {
	html, err := renderMarkdown("~~gone~~")
	require.NoError(t, err)
	require.Contains(t, html, "<del")
}

func TestRenderMarkdownStripsScriptAndEvents(t *testing.T) {
	html, err := renderMarkdown("# Hi\n\n<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>")
	require.NoError(t, err)
	require.NotContains(t, html, "<script")
	require.NotContains(t, html, "onerror")
	require.Contains(t, html, "<h1")
}

func TestRenderMarkdownStripsExternalImages(t *testing.T) {
	html, err := renderMarkdown("![x](https://evil.example/track.png)")
	require.NoError(t, err)
	require.NotContains(t, html, "evil.example")
}

func TestRenderMarkdownAllowsRelativeImages(t *testing.T) {
	html, err := renderMarkdown("![x](/local.png)")
	require.NoError(t, err)
	require.Contains(t, html, "/local.png")
}

func TestRenderMarkdownStripsJavascriptLinks(t *testing.T) {
	html, err := renderMarkdown("[click](javascript:alert(1))")
	require.NoError(t, err)
	require.NotContains(t, html, "javascript:")
}

func TestRenderMarkdownStripsProtocolRelativeImages(t *testing.T) {
	html, err := renderMarkdown("![x](//evil.com/beacon.png)")
	require.NoError(t, err)
	require.NotContains(t, html, "evil.com")
}

func TestRenderMarkdownStripsDataImages(t *testing.T) {
	html, err := renderMarkdown("![x](data:image/png;base64,AAAA)")
	require.NoError(t, err)
	require.NotContains(t, html, "data:")
}

func TestSanitizeSnippetBlocksMaliciousHTML(t *testing.T) {
	input := `<mark>hit</mark> <script>alert(1)</script> <img src=x onerror=evil()> plain`
	out := sanitizeSnippet(input)
	require.Contains(t, out, "<mark>hit</mark>")
	require.NotContains(t, out, "<script")
	require.NotContains(t, out, "onerror")
	require.NotContains(t, out, "<img")
	require.Contains(t, out, "plain")
}
