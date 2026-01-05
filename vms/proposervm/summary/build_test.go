// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuild(t *testing.T) {
	require := require.New(t)

	forkHeight := uint64(2022)
	block := []byte("blockBytes")
	coreSummary := []byte("coreSummary")
	builtSummary, err := Build(forkHeight, block, coreSummary)
	require.NoError(err)

	require.Equal(builtSummary.ForkHeight(), forkHeight)
	require.Equal(builtSummary.BlockBytes(), block)
	require.Equal(builtSummary.InnerSummaryBytes(), coreSummary)
}
