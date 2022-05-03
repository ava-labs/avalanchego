// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParse(t *testing.T) {
	assert := assert.New(t)

	forkHeight := uint64(2022)
	block := []byte("blockBytes")
	coreSummary := []byte("coreSummary")
	builtSummary, err := Build(forkHeight, block, coreSummary)
	assert.NoError(err)

	summaryBytes := builtSummary.Bytes()
	parsedSummary, err := Parse(summaryBytes)
	assert.NoError(err)

	assert.Equal(builtSummary.Bytes(), parsedSummary.Bytes())
	assert.Equal(builtSummary.ID(), parsedSummary.ID())
	assert.Equal(builtSummary.ForkHeight(), parsedSummary.ForkHeight())
	assert.Equal(builtSummary.BlockBytes(), parsedSummary.BlockBytes())
	assert.Equal(builtSummary.InnerSummaryBytes(), parsedSummary.InnerSummaryBytes())
}

func TestParseGibberish(t *testing.T) {
	assert := assert.New(t)

	bytes := []byte{0, 1, 2, 3, 4, 5}

	_, err := Parse(bytes)
	assert.Error(err)
}
