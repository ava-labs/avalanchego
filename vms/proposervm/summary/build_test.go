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

	proBlkID := ids.ID{'p', 'r', 'o', 'I', 'D'}
	coreSummary := &block.TestSummary{
		SummaryHeight: 2022,
		SummaryID:     ids.ID{'I', 'D'},
		ContentBytes:  []byte{'b', 'y', 't', 'e', 's'},
	}
	builtSummary, err := BuildProposerSummary(proBlkID, coreSummary)
	assert.NoError(err)

	assert.Equal(builtSummary.Height(), coreSummary.Height())

	assert.Equal(builtSummary.ProposerBlockID(), proBlkID)
	assert.Equal(builtSummary.InnerBytes(), coreSummary.Bytes())
}
