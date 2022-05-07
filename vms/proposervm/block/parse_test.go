// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"crypto"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/staking"
)

func TestParse(t *testing.T) {
	assert := assert.New(t)

	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}
	chainID := ids.ID{4}

	tlsCert, err := staking.NewTLSCert()
	assert.NoError(err)

	cert := tlsCert.Leaf
	key := tlsCert.PrivateKey.(crypto.Signer)

	builtBlock, err := Build(
		parentID,
		timestamp,
		pChainHeight,
		cert,
		innerBlockBytes,
		chainID,
		key,
	)
	assert.NoError(err)

	builtBlockBytes := builtBlock.Bytes()

	parsedBlockIntf, err := Parse(builtBlockBytes)
	assert.NoError(err)

	parsedBlock, ok := parsedBlockIntf.(SignedBlock)
	assert.True(ok)

	equal(assert, chainID, builtBlock, parsedBlock)
}

func TestParseHeader(t *testing.T) {
	assert := assert.New(t)

	chainID := ids.ID{1}
	parentID := ids.ID{2}
	bodyID := ids.ID{3}

	builtHeader, err := BuildHeader(
		chainID,
		parentID,
		bodyID,
	)
	assert.NoError(err)

	builtHeaderBytes := builtHeader.Bytes()

	parsedHeader, err := ParseHeader(builtHeaderBytes)
	assert.NoError(err)

	equalHeader(assert, builtHeader, parsedHeader)
}

func TestParseOption(t *testing.T) {
	assert := assert.New(t)

	parentID := ids.ID{1}
	innerBlockBytes := []byte{3}

	builtOption, err := BuildOption(parentID, innerBlockBytes)
	assert.NoError(err)

	builtOptionBytes := builtOption.Bytes()

	parsedOption, err := Parse(builtOptionBytes)
	assert.NoError(err)

	equalOption(assert, builtOption, parsedOption)
}

func TestParseUnsigned(t *testing.T) {
	assert := assert.New(t)

	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}

	builtBlock, err := BuildUnsigned(parentID, timestamp, pChainHeight, innerBlockBytes, ids.ShortID{})
	assert.NoError(err)

	builtBlockBytes := builtBlock.Bytes()

	parsedBlockIntf, err := Parse(builtBlockBytes)
	assert.NoError(err)

	parsedBlock, ok := parsedBlockIntf.(SignedBlock)
	assert.True(ok)

	equal(assert, ids.Empty, builtBlock, parsedBlock)
}

func TestParseGibberish(t *testing.T) {
	assert := assert.New(t)

	bytes := []byte{0, 1, 2, 3, 4, 5}

	_, err := Parse(bytes)
	assert.Error(err)
}
