// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

// Package docs provides markdown-aware editing helpers built on goldmark.
package docs

import (
	"errors"
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// ErrHeadingNotFound is returned when no heading matches the requested name.
var ErrHeadingNotFound = errors.New("heading not found")

// HeadingNotFoundError reports that the requested heading was absent and carries
// the headings the parser actually saw, so callers can surface the available
// options (and a UI heading picker has the data it needs). It satisfies
// errors.Is(err, ErrHeadingNotFound) so existing sentinel checks keep working.
type HeadingNotFoundError struct {
	Requested string
	Seen      []string
}

func (e *HeadingNotFoundError) Error() string {
	return fmt.Sprintf("heading not found: %q (available: %s)", e.Requested, strings.Join(e.Seen, ", "))
}

func (e *HeadingNotFoundError) Is(target error) bool { return target == ErrHeadingNotFound }

// normalizeHeading strips a leading ATX marker run (one to six '#') plus its
// following space, so a caller may pass either the raw markdown form ("## Foo")
// or the plain text goldmark parses ("Foo"). Surrounding whitespace is trimmed
// either way. A '#' run not followed by a space is left intact (it is part of the
// text, not an ATX marker).
func normalizeHeading(h string) string {
	h = strings.TrimSpace(h)
	trimmed := strings.TrimLeft(h, "#")
	if len(trimmed) < len(h) && strings.HasPrefix(trimmed, " ") {
		return strings.TrimSpace(trimmed)
	}
	return h
}

type headingInfo struct {
	line  int    // 0-based source line index of the heading
	level int    // 1..6
	text  string // trimmed heading text
}

// sectionRange parses source, finds the heading, and returns the split lines, the
// heading's line index, and the [start,end) body line range (start = line after the
// heading; end = next same-or-higher heading line, or len(lines)).
// Returns ErrHeadingNotFound if no heading matches.
func sectionRange(source, heading string) (lines []string, headingLine, start, end int, err error) {
	lines = strings.Split(source, "\n")
	headings := enumerateHeadings([]byte(source))

	target := -1
	want := normalizeHeading(heading)
	for i, h := range headings {
		if h.text == want {
			target = i
			break
		}
	}
	if target == -1 {
		seen := make([]string, len(headings))
		for i, h := range headings {
			seen[i] = h.text
		}
		return nil, 0, 0, 0, &HeadingNotFoundError{Requested: heading, Seen: seen}
	}

	headingLine = headings[target].line
	start = headingLine + 1
	end = len(lines)
	for j := target + 1; j < len(headings); j++ {
		if headings[j].level <= headings[target].level {
			end = headings[j].line
			break
		}
	}
	return lines, headingLine, start, end, nil
}

// ReplaceSection replaces the body under the heading whose text equals `heading`
// (case-sensitive, trimmed). The heading line is preserved; everything from the next
// line up to the next heading of the same-or-higher level (or end of document) is
// replaced with newContent. Headings inside fenced code blocks are ignored.
func ReplaceSection(source, heading, newContent string) (string, error) {
	lines, _, start, end, err := sectionRange(source, heading)
	if err != nil {
		return "", err
	}

	// Rebuild: lines[:start] + heading body replacement + lines[end:].
	var b strings.Builder
	for i := 0; i < start; i++ {
		b.WriteString(lines[i])
		b.WriteByte('\n')
	}
	b.WriteString(newContent)
	if !strings.HasSuffix(newContent, "\n") {
		b.WriteByte('\n')
	}
	for i := end; i < len(lines); i++ {
		b.WriteString(lines[i])
		if i < len(lines)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String(), nil
}

// GetSection returns the body text under the heading (excluding the heading line),
// from the line after the heading to the next same-or-higher heading (or EOF).
// Returns ErrHeadingNotFound if no heading matches.
func GetSection(source, heading string) (string, error) {
	lines, _, start, end, err := sectionRange(source, heading)
	if err != nil {
		return "", err
	}
	return strings.Join(lines[start:end], "\n"), nil
}

// DeleteSection removes the heading line and its body (up to the next same-or-higher
// heading, or EOF), returning the resulting document.
// Returns ErrHeadingNotFound if no heading matches.
func DeleteSection(source, heading string) (string, error) {
	lines, headingLine, _, end, err := sectionRange(source, heading)
	if err != nil {
		return "", err
	}
	kept := append(append([]string{}, lines[:headingLine]...), lines[end:]...)
	return strings.Join(kept, "\n"), nil
}

// enumerateHeadings parses the source with goldmark and returns real headings
// (excludes #-lines inside code fences) with their source line index, level, and text.
func enumerateHeadings(source []byte) []headingInfo {
	lineStarts := computeLineStarts(source)
	doc := goldmark.DefaultParser().Parse(text.NewReader(source))

	var out []headingInfo
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		h, ok := n.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}
		out = append(out, headingInfo{
			line:  lineForOffset(lineStarts, headingOffset(h)),
			level: h.Level,
			text:  headingText(h, source),
		})
		return ast.WalkSkipChildren, nil
	})
	return out
}

// headingText extracts the trimmed text of an ATX/Setext heading. In goldmark
// v1.8.2 the heading's Lines() segments do not reliably carry the inline text,
// so we walk the heading's child inline nodes and collect their source segment
// values (text.Segment.Value), which yields the rendered heading text.
func headingText(h *ast.Heading, source []byte) string {
	var sb strings.Builder
	// Walk ALL descendants, not just direct children, so headings with inline
	// formatting (e.g. "## Foo **bar**" or "## **Bold**") yield their plain text
	// ("Foo bar" / "Bold") consistently rather than dropping the formatted spans.
	_ = ast.Walk(h, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if t, ok := n.(*ast.Text); ok {
			sb.Write(t.Segment.Value(source))
		}
		return ast.WalkContinue, nil
	})
	if sb.Len() == 0 {
		// Fall back to the heading's own line segments (e.g. heading with no text nodes).
		for i := 0; i < h.Lines().Len(); i++ {
			seg := h.Lines().At(i)
			sb.Write(seg.Value(source))
		}
	}
	return strings.TrimSpace(sb.String())
}

// headingOffset returns the byte offset of the start of the heading in source,
// preferring the heading's line segments and falling back to its first child.
func headingOffset(h *ast.Heading) int {
	if h.Lines().Len() > 0 {
		return h.Lines().At(0).Start
	}
	if t, ok := h.FirstChild().(*ast.Text); ok {
		return t.Segment.Start
	}
	return -1
}

func computeLineStarts(source []byte) []int {
	starts := []int{0}
	for i, c := range source {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// lineForOffset returns the 0-based line index containing the given byte offset.
func lineForOffset(starts []int, offset int) int {
	if offset < 0 {
		return 0
	}
	line := 0
	for i, s := range starts {
		if s <= offset {
			line = i
		} else {
			break
		}
	}
	return line
}
