// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuild(t *testing.T) {
	assert := assert.New(t)

	forkHeight := uint64(2022)
	block := []byte("blockBytes")
	coreSummary := []byte("coreSummary")
	builtSummary, err := Build(forkHeight, block, coreSummary)
	assert.NoError(err)

	assert.Equal(builtSummary.ForkHeight(), forkHeight)
	assert.Equal(builtSummary.BlockBytes(), block)
	assert.Equal(builtSummary.InnerSummaryBytes(), coreSummary)
}
