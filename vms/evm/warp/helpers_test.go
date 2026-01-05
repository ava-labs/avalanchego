// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

var (
	networkID           uint32 = 54321
	sourceChainID              = ids.GenerateTestID()
	testSourceAddress          = utils.RandomBytes(20)
	testPayload                = []byte("test")
	testUnsignedMessage *warp.UnsignedMessage

	emptyBlockStore BlockStore = testBlockStore{}
)

func init() {
	testAddressedCallPayload, err := payload.NewAddressedCall(testSourceAddress, testPayload)
	if err != nil {
		panic(err)
	}
	testUnsignedMessage, err = warp.NewUnsignedMessage(networkID, sourceChainID, testAddressedCallPayload.Bytes())
	if err != nil {
		panic(err)
	}
}

// testBlockStore implements BlockStore for testing
type testBlockStore set.Set[ids.ID]

func (t testBlockStore) HasBlock(_ context.Context, blockID ids.ID) error {
	s := set.Set[ids.ID](t)
	if !s.Contains(blockID) {
		return database.ErrNotFound
	}
	return nil
}
