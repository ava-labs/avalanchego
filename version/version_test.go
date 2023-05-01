// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSemanticString(t *testing.T) {
	require := require.New(t)

	v := Semantic{
		Major: 1,
		Minor: 2,
		Patch: 3,
	}

	require.Equal("v1.2.3", v.String())
}
