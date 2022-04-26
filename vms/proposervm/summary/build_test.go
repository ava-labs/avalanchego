// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/stretchr/testify/assert"
)

func TestBuild(t *testing.T) {
	assert := assert.New(t)

	proBlkBytes := []byte("proBlkBytes")
	coreSummary := &block.TestSummary{
		HeightV: 2022,
		IDV:     ids.ID{'I', 'D'},
		BytesV:  []byte{'b', 'y', 't', 'e', 's'},
	}
	builtSummary, err := BuildProposerSummary(proBlkBytes, coreSummary)
	assert.NoError(err)

	assert.Equal(builtSummary.Height(), coreSummary.Height())
	assert.Equal(builtSummary.BlockBytes(), proBlkBytes)
	assert.Equal(builtSummary.InnerSummaryBytes(), coreSummary.Bytes())
}

func TestBuildEmptySummary(t *testing.T) {
	assert := assert.New(t)

	emptySummary := &block.TestSummary{}
	builtSummary, err := BuildEmptyProposerSummary()
	assert.NoError(err)

	assert.Equal(builtSummary.ID(), ids.Empty)
	assert.Equal(builtSummary.Height(), uint64(0))
	assert.Equal(builtSummary.Bytes(), []byte(nil))
	assert.Equal(builtSummary.BlockBytes(), []byte(nil))
	assert.Equal(builtSummary.InnerSummaryBytes(), emptySummary.Bytes())
}
