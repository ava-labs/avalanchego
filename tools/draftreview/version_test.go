package draftreview

import (
	"strings"
	"testing"
)

func TestVersionString(t *testing.T) {
	t.Parallel()

	got := VersionString()
	if !strings.HasPrefix(got, "gh-pending-review commit=") {
		t.Fatalf("unexpected version string %q", got)
	}
}
