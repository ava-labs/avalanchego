// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionString(t *testing.T) {
	t.Parallel()

	got := VersionString()
	require.True(t, strings.HasPrefix(got, "gh-pending-review commit="), "unexpected version string %q", got)
}
