// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

const networkID uint32 = 54321

var sourceChainID = ids.GenerateTestID()

func newHash(tb testing.TB) (*warp.UnsignedMessage, *payload.Hash) {
	p, err := payload.NewHash(
		ids.GenerateTestID(),
	)
	require.NoError(tb, err)

	m, err := warp.NewUnsignedMessage(networkID, sourceChainID, p.Bytes())
	require.NoError(tb, err)
	return m, p
}

// newAddressedCall builds an addressed-call warp message with a random
// non-empty 20-byte source address.
func newAddressedCall(tb testing.TB, data []byte) *warp.UnsignedMessage {
	p, err := payload.NewAddressedCall(
		utils.RandomBytes(20),
		data,
	)
	require.NoError(tb, err)

	m, err := warp.NewUnsignedMessage(networkID, sourceChainID, p.Bytes())
	require.NoError(tb, err)
	return m
}
