package docs

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnifiedDiff(t *testing.T) {
	old := "line one\nline two\nline three\n"
	updated := "line one\nline 2\nline three\n"
	d := UnifiedDiff("v1", "v2", old, updated)
	require.Contains(t, d, "-line two")
	require.Contains(t, d, "+line 2")
	require.Contains(t, d, "line one") // context line retained
}

func TestUnifiedDiffIdenticalIsEmpty(t *testing.T) {
	same := "a\nb\n"
	require.Equal(t, "", strings.TrimSpace(UnifiedDiff("v1", "v2", same, same)))
}
