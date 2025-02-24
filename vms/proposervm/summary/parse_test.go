// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
)

func TestParse(t *testing.T) {
	require := require.New(t)

	forkHeight := uint64(2022)
	block := []byte("blockBytes")
	coreSummary := []byte("coreSummary")
	builtSummary, err := Build(forkHeight, block, coreSummary)
	require.NoError(err)

	summaryBytes := builtSummary.Bytes()
	parsedSummary, err := Parse(summaryBytes)
	require.NoError(err)

	require.Equal(builtSummary.Bytes(), parsedSummary.Bytes())
	require.Equal(builtSummary.ID(), parsedSummary.ID())
	require.Equal(builtSummary.ForkHeight(), parsedSummary.ForkHeight())
	require.Equal(builtSummary.BlockBytes(), parsedSummary.BlockBytes())
	require.Equal(builtSummary.InnerSummaryBytes(), parsedSummary.InnerSummaryBytes())
}

func TestParseGibberish(t *testing.T) {
	require := require.New(t)

	bytes := []byte{0, 1, 2, 3, 4, 5}

	_, err := Parse(bytes)
	require.ErrorIs(err, codec.ErrUnknownVersion)
}
