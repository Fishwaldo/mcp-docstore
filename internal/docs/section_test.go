// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package docs

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const sample = "# Title\n\nintro\n\n## Overview\n\nold overview\n\n## Details\n\nd1\n\n### Sub\n\nsub body\n\n## End\n\nlast\n"

func TestReplaceSectionReplacesOnlyThatSection(t *testing.T) {
	out, err := ReplaceSection(sample, "Overview", "new overview text")
	require.NoError(t, err)
	require.Contains(t, out, "## Overview\nnew overview text\n")
	require.NotContains(t, out, "old overview")
	// Other sections untouched.
	require.Contains(t, out, "## Details")
	require.Contains(t, out, "d1")
	require.Contains(t, out, "last")
}

func TestReplaceSectionStopsAtSameLevelHeading(t *testing.T) {
	// Replacing "Details" must keep its nested "### Sub" content out of "End",
	// and must stop at the next "## " (same level), not at "### Sub".
	out, err := ReplaceSection(sample, "Details", "brand new details")
	require.NoError(t, err)
	require.Contains(t, out, "## Details\nbrand new details\n")
	require.NotContains(t, out, "### Sub") // nested content was part of the section, replaced
	require.NotContains(t, out, "sub body")
	require.Contains(t, out, "## End") // next same-level heading preserved
	require.Contains(t, out, "last")
}

func TestReplaceSectionLastSection(t *testing.T) {
	out, err := ReplaceSection(sample, "End", "the new end")
	require.NoError(t, err)
	require.Contains(t, out, "## End\nthe new end")
	require.NotContains(t, out, "\nlast")
}

func TestReplaceSectionHeadingNotFound(t *testing.T) {
	_, err := ReplaceSection(sample, "Nonexistent", "x")
	require.ErrorIs(t, err, ErrHeadingNotFound)
}

func TestReplaceSectionInlineFormattedHeading(t *testing.T) {
	// A heading with inline formatting must match on its plain text.
	src := "## Foo **bar**\n\nold\n\n## Next\n\nkeep\n"
	out, err := ReplaceSection(src, "Foo bar", "replaced")
	require.NoError(t, err)
	require.Contains(t, out, "## Foo **bar**\nreplaced")
	require.NotContains(t, out, "old")
	require.Contains(t, out, "## Next")

	// Inline-only (bold) heading matches on its plain text too.
	src2 := "## **Bold only**\n\nx\n"
	out2, err := ReplaceSection(src2, "Bold only", "y")
	require.NoError(t, err)
	require.Contains(t, out2, "## **Bold only**\ny")
}

func TestReplaceSectionH1Target(t *testing.T) {
	// A level-1 target with only deeper headings after it replaces to EOF.
	src := "# Title\n\nbody\n\n## Sub\n\nmore\n"
	out, err := ReplaceSection(src, "Title", "new")
	require.NoError(t, err)
	require.Contains(t, out, "# Title\nnew")
	require.NotContains(t, out, "## Sub")
	require.NotContains(t, out, "more")
}

func TestReplaceSectionIgnoresHeadingInCodeFence(t *testing.T) {
	src := "## Real\n\nbefore\n\n```\n## Fake heading in code\n```\n\nafter\n"
	out, err := ReplaceSection(src, "Real", "replaced")
	require.NoError(t, err)
	// The whole section (including the fenced block) is replaced; the fake heading
	// was never treated as a section boundary.
	require.Contains(t, out, "## Real\nreplaced")
	require.NotContains(t, out, "Fake heading")
	// Matching the fake heading must fail — it is not a real heading.
	_, err = ReplaceSection(src, "Fake heading in code", "x")
	require.ErrorIs(t, err, ErrHeadingNotFound)
}

func TestGetSection(t *testing.T) {
	body, err := GetSection(sample, "Overview")
	require.NoError(t, err)
	require.Equal(t, "old overview", strings.TrimSpace(body))

	// Section with nested subsection returns the whole section body (incl. the ### Sub).
	body, err = GetSection(sample, "Details")
	require.NoError(t, err)
	require.Contains(t, body, "d1")
	require.Contains(t, body, "### Sub")
	require.Contains(t, body, "sub body")

	_, err = GetSection(sample, "Nope")
	require.ErrorIs(t, err, ErrHeadingNotFound)
}

func TestDeleteSection(t *testing.T) {
	out, err := DeleteSection(sample, "Overview")
	require.NoError(t, err)
	require.NotContains(t, out, "## Overview")
	require.NotContains(t, out, "old overview")
	// Neighbors preserved.
	require.Contains(t, out, "# Title")
	require.Contains(t, out, "## Details")
	require.Contains(t, out, "## End")

	// Deleting a section removes its nested subsections too.
	out, err = DeleteSection(sample, "Details")
	require.NoError(t, err)
	require.NotContains(t, out, "## Details")
	require.NotContains(t, out, "### Sub")
	require.NotContains(t, out, "sub body")
	require.Contains(t, out, "## End")

	_, err = DeleteSection(sample, "Nope")
	require.ErrorIs(t, err, ErrHeadingNotFound)
}

func TestSectionMatchesWithATXPrefix(t *testing.T) {
	src := "## Foo\nbody"
	got, err := GetSection(src, "## Foo")
	if err != nil {
		t.Fatalf("want match with ATX prefix, got error: %v", err)
	}
	if got != "body" {
		t.Fatalf("got %q want %q", got, "body")
	}
}

func TestSectionMatchesPlainAndPunctuated(t *testing.T) {
	src := "## 6. Inference Stack (install order)\nalpha\n## 4. Proxmox — Gotchas\nbeta\n"
	for _, h := range []string{
		"## 6. Inference Stack (install order)",
		"6. Inference Stack (install order)",
		"## 4. Proxmox — Gotchas",
	} {
		if _, err := GetSection(src, h); err != nil {
			t.Errorf("heading %q should match, got: %v", h, err)
		}
	}
}

func TestHeadingNotFoundListsSeenHeadings(t *testing.T) {
	src := "## Alpha\na\n## Beta\nb\n"
	_, err := GetSection(src, "Gamma")
	if !errors.Is(err, ErrHeadingNotFound) {
		t.Fatalf("want errors.Is ErrHeadingNotFound, got %v", err)
	}
	var hnf *HeadingNotFoundError
	if !errors.As(err, &hnf) {
		t.Fatalf("want *HeadingNotFoundError, got %T", err)
	}
	if got := strings.Join(hnf.Seen, ","); got != "Alpha,Beta" {
		t.Fatalf("seen = %q want %q", got, "Alpha,Beta")
	}
}

func TestReplaceSectionPreservesTrailingBlankLine(t *testing.T) {
	src := "# One\nalpha\n\n# Two\nbeta\n"
	got, err := ReplaceSection(src, "One", "alpha2")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "alpha2\n\n# Two") {
		t.Fatalf("blank line before next heading collapsed: %q", got)
	}
}

func TestReplaceSectionFinalSectionNoSpuriousBlankLine(t *testing.T) {
	src := "# One\nalpha\n"
	got, err := ReplaceSection(src, "One", "alpha2")
	if err != nil {
		t.Fatal(err)
	}
	if got != "# One\nalpha2\n" {
		t.Fatalf("final-section replace corrupted output: %q want %q", got, "# One\nalpha2\n")
	}
}
