// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"bytes"
	"net/url"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// markdownRenderer parses GitHub Flavored Markdown — tables, strikethrough, task
// lists, and autolinks — which the documents rely on. goldmark.Markdown is safe for
// concurrent use, so it is built once and shared. Its HTML output is always passed
// through bluemonday before reaching the browser (see renderMarkdown).
var markdownRenderer = goldmark.New(goldmark.WithExtensions(extension.GFM))

// sanitizeSnippet keeps only <mark> highlight tags from a Bleve search snippet
// and escapes everything else, preventing indexed document content from injecting
// markup into the SPA via dangerouslySetInnerHTML.
func sanitizeSnippet(s string) string {
	p := bluemonday.NewPolicy()
	p.AllowElements("mark")
	return p.Sanitize(s)
}

// renderMarkdown converts a document body to sanitized HTML for the browser. goldmark
// produces HTML; bluemonday then strips anything that could execute or exfiltrate —
// scripts, event handlers, javascript: URLs. Image src values are restricted to purely
// host-less relative paths (e.g. "/local.png" or "img.png"): external URLs (http/https),
// protocol-relative URLs (//host/…), and data: URIs are all stripped. A URL is treated as
// host-less relative only when its Host field is empty AND its Scheme is empty. This is the
// single trusted render path; the browser only ever receives the output of this function.
func renderMarkdown(md string) (string, error) {
	var buf bytes.Buffer
	if err := markdownRenderer.Convert([]byte(md), &buf); err != nil {
		return "", err
	}

	p := bluemonday.UGCPolicy()

	// UGCPolicy allows http/https globally (needed for anchor hrefs), but that lets
	// external and protocol-relative img src values through validURL. RewriteSrc blanks
	// any src that has a non-empty Host (catches //evil.com) OR an http/https scheme
	// (catches absolute URLs). Only purely host-less relative paths survive.
	p.RewriteSrc(func(u *url.URL) {
		if u.Scheme == "http" || u.Scheme == "https" || u.Host != "" {
			*u = url.URL{}
		}
	})

	return p.Sanitize(buf.String()), nil
}
