// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

// TestSpliceBlockRLPExtras guards that the C-Chain [ethtypes.BlockBodyHooks]
// encode a block as its header followed by its body's fields, which
// [blocks.SpliceBlockRLP] relies on without any runtime verification. A
// failure here means splicing corrupts C-Chain blocks and MUST NOT ship.
func TestSpliceBlockRLPExtras(t *testing.T) {
	header := &ethtypes.Header{
		Number:     big.NewInt(1),
		Difficulty: big.NewInt(0),
	}
	b := customtypes.NewBlockWithExtData(
		header,
		nil, nil, nil,
		trie.NewStackTrie(nil),
		[]byte("cross-chain transactions"),
		true,
	)

	headerRLP, err := rlp.EncodeToBytes(b.Header())
	require.NoErrorf(t, err, "rlp.EncodeToBytes(%T)", b.Header())
	bodyRLP, err := rlp.EncodeToBytes(b.Body())
	require.NoErrorf(t, err, "rlp.EncodeToBytes(%T)", b.Body())
	want, err := rlp.EncodeToBytes(b)
	require.NoErrorf(t, err, "rlp.EncodeToBytes(%T)", b)

	got, err := blocks.SpliceBlockRLP(headerRLP, bodyRLP)
	require.NoError(t, err, "blocks.SpliceBlockRLP()")
	require.Equal(t, want, got, "blocks.SpliceBlockRLP()")
}
