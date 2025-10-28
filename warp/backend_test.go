// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"testing"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/warp/warptest"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

var (
	networkID           uint32 = 54321
	sourceChainID              = ids.GenerateTestID()
	testSourceAddress          = utils.RandomBytes(20)
	testPayload                = []byte("test")
	testUnsignedMessage *avalancheWarp.UnsignedMessage
)

func init() {
	testAddressedCallPayload, err := payload.NewAddressedCall(testSourceAddress, testPayload)
	if err != nil {
		panic(err)
	}
	testUnsignedMessage, err = avalancheWarp.NewUnsignedMessage(networkID, sourceChainID, testAddressedCallPayload.Bytes())
	if err != nil {
		panic(err)
	}
}

func TestAddAndGetValidMessage(t *testing.T) {
	db := memdb.New()

	sk, err := localsigner.New()
	require.NoError(t, err)
	warpSigner := avalancheWarp.NewSigner(sk, networkID, sourceChainID)
	messageSignatureCache := lru.NewCache[ids.ID, []byte](500)
	backend, err := NewBackend(networkID, sourceChainID, warpSigner, nil, db, messageSignatureCache, nil)
	require.NoError(t, err)

	// Add testUnsignedMessage to the warp backend
	require.NoError(t, backend.AddMessage(testUnsignedMessage))

	// Verify that a signature is returned successfully, and compare to expected signature.
	signature, err := backend.GetMessageSignature(context.TODO(), testUnsignedMessage)
	require.NoError(t, err)

	expectedSig, err := warpSigner.Sign(testUnsignedMessage)
	require.NoError(t, err)
	require.Equal(t, expectedSig, signature)
}

func TestAddAndGetUnknownMessage(t *testing.T) {
	db := memdb.New()

	sk, err := localsigner.New()
	require.NoError(t, err)
	warpSigner := avalancheWarp.NewSigner(sk, networkID, sourceChainID)
	messageSignatureCache := lru.NewCache[ids.ID, []byte](500)
	backend, err := NewBackend(networkID, sourceChainID, warpSigner, nil, db, messageSignatureCache, nil)
	require.NoError(t, err)

	// Try getting a signature for a message that was not added.
	_, err = backend.GetMessageSignature(context.TODO(), testUnsignedMessage)
	require.ErrorIs(t, err, ErrVerifyWarpMessage)
}

func TestGetBlockSignature(t *testing.T) {
	require := require.New(t)

	blkID := ids.GenerateTestID()
	blockClient := warptest.MakeBlockClient(blkID)
	db := memdb.New()

	sk, err := localsigner.New()
	require.NoError(err)
	warpSigner := avalancheWarp.NewSigner(sk, networkID, sourceChainID)
	messageSignatureCache := lru.NewCache[ids.ID, []byte](500)
	backend, err := NewBackend(networkID, sourceChainID, warpSigner, blockClient, db, messageSignatureCache, nil)
	require.NoError(err)

	blockHashPayload, err := payload.NewHash(blkID)
	require.NoError(err)
	unsignedMessage, err := avalancheWarp.NewUnsignedMessage(networkID, sourceChainID, blockHashPayload.Bytes())
	require.NoError(err)
	expectedSig, err := warpSigner.Sign(unsignedMessage)
	require.NoError(err)

	signature, err := backend.GetBlockSignature(context.TODO(), blkID)
	require.NoError(err)
	require.Equal(expectedSig, signature)

	_, err = backend.GetBlockSignature(context.TODO(), ids.GenerateTestID())
	require.ErrorIs(err, ErrValidateBlock)
}

func TestZeroSizedCache(t *testing.T) {
	db := memdb.New()

	sk, err := localsigner.New()
	require.NoError(t, err)
	warpSigner := avalancheWarp.NewSigner(sk, networkID, sourceChainID)

	// Verify zero sized cache works normally, because the lru cache will be initialized to size 1 for any size parameter <= 0.
	messageSignatureCache := lru.NewCache[ids.ID, []byte](0)
	backend, err := NewBackend(networkID, sourceChainID, warpSigner, nil, db, messageSignatureCache, nil)
	require.NoError(t, err)

	// Add testUnsignedMessage to the warp backend
	require.NoError(t, backend.AddMessage(testUnsignedMessage))

	// Verify that a signature is returned successfully, and compare to expected signature.
	signature, err := backend.GetMessageSignature(context.TODO(), testUnsignedMessage)
	require.NoError(t, err)

	expectedSig, err := warpSigner.Sign(testUnsignedMessage)
	require.NoError(t, err)
	require.Equal(t, expectedSig, signature)
}

func TestOffChainMessages(t *testing.T) {
	type test struct {
		offchainMessages [][]byte
		check            func(require *require.Assertions, b Backend)
		err              error
	}
	sk, err := localsigner.New()
	require.NoError(t, err)
	warpSigner := avalancheWarp.NewSigner(sk, networkID, sourceChainID)

	for name, test := range map[string]test{
		"no offchain messages": {},
		"single off-chain message": {
			offchainMessages: [][]byte{
				testUnsignedMessage.Bytes(),
			},
			check: func(require *require.Assertions, b Backend) {
				msg, err := b.GetMessage(testUnsignedMessage.ID())
				require.NoError(err)
				require.Equal(testUnsignedMessage.Bytes(), msg.Bytes())

				signature, err := b.GetMessageSignature(context.TODO(), testUnsignedMessage)
				require.NoError(err)
				expectedSignatureBytes, err := warpSigner.Sign(msg)
				require.NoError(err)
				require.Equal(expectedSignatureBytes, signature)
			},
		},
		"unknown message": {
			check: func(require *require.Assertions, b Backend) {
				_, err := b.GetMessage(testUnsignedMessage.ID())
				require.ErrorIs(err, database.ErrNotFound)
			},
		},
		"invalid message": {
			offchainMessages: [][]byte{{1, 2, 3}},
			err:              errParsingOffChainMessage,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			db := memdb.New()

			messageSignatureCache := lru.NewCache[ids.ID, []byte](0)
			backend, err := NewBackend(networkID, sourceChainID, warpSigner, nil, db, messageSignatureCache, test.offchainMessages)
			require.ErrorIs(err, test.err)
			if test.check != nil {
				test.check(require, backend)
			}
		})
	}
}
