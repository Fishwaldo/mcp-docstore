// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"bytes"
	"net/url"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
)

// renderMarkdown converts a document body to sanitized HTML for the browser. goldmark
// produces HTML; bluemonday then strips anything that could execute or exfiltrate —
// scripts, event handlers, javascript: URLs. Image src values are restricted to purely
// host-less relative paths (e.g. "/local.png" or "img.png"): external URLs (http/https),
// protocol-relative URLs (//host/…), and data: URIs are all stripped. A URL is treated as
// host-less relative only when its Host field is empty AND its Scheme is empty. This is the
// single trusted render path; the browser only ever receives the output of this function.
func renderMarkdown(md string) (string, error) {
	var buf bytes.Buffer
	if err := goldmark.Convert([]byte(md), &buf); err != nil {
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
