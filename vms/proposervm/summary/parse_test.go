// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/stretchr/testify/assert"
)

func TestParse(t *testing.T) {
	assert := assert.New(t)

	proBlkID := ids.ID{'p', 'r', 'o', 'I', 'D'}
	coreSummary := &block.TestSummary{
		SummaryKey:   2022,
		SummaryID:    ids.ID{'I', 'D'},
		ContentBytes: []byte{'b', 'y', 't', 'e', 's'},
	}
	builtSummary, err := BuildProposerSummary(proBlkID, coreSummary)
	assert.NoError(err)

	proSummaryBytes := builtSummary.Bytes()

	parsedSummaryIntf, err := Parse(proSummaryBytes)
	assert.NoError(err)

	parsedBlock, ok := parsedSummaryIntf.(*StatelessSummary)
	assert.True(ok)

	assert.Equal(builtSummary.Bytes(), parsedBlock.Bytes())
	assert.Equal(builtSummary.ID(), parsedBlock.ID())
	assert.Equal(builtSummary.ProposerBlockID(), parsedBlock.ProposerBlockID())
	assert.Equal(builtSummary.InnerBytes(), parsedBlock.InnerBytes())
}

func TestParseGibberish(t *testing.T) {
	assert := assert.New(t)

	bytes := []byte{0, 1, 2, 3, 4, 5}

	_, err := Parse(bytes)
	assert.Error(err)
}
