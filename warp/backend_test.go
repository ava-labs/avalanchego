// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/hashing"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/stretchr/testify/require"
)

var (
	networkID     uint32 = 54321
	sourceChainID        = ids.GenerateTestID()
	testPayload          = []byte("test")
)

func TestClearDB(t *testing.T) {
	db := memdb.New()

	sk, err := bls.NewSecretKey()
	require.NoError(t, err)
	warpSigner := avalancheWarp.NewSigner(sk, networkID, sourceChainID)
	backendIntf := NewBackend(networkID, sourceChainID, warpSigner, nil, db, 500)
	backend, ok := backendIntf.(*backend)
	require.True(t, ok)

	// use multiple messages to test that all messages get cleared
	payloads := [][]byte{[]byte("test1"), []byte("test2"), []byte("test3"), []byte("test4"), []byte("test5")}
	messageIDs := []ids.ID{}

	// add all messages
	for _, payload := range payloads {
		unsignedMsg, err := avalancheWarp.NewUnsignedMessage(networkID, sourceChainID, payload)
		require.NoError(t, err)
		messageID := hashing.ComputeHash256Array(unsignedMsg.Bytes())
		messageIDs = append(messageIDs, messageID)
		err = backend.AddMessage(unsignedMsg)
		require.NoError(t, err)
		// ensure that the message was added
		_, err = backend.GetMessageSignature(messageID)
		require.NoError(t, err)
	}

	err = backend.Clear()
	require.NoError(t, err)
	require.Zero(t, backend.messageCache.Len())
	require.Zero(t, backend.messageSignatureCache.Len())
	require.Zero(t, backend.blockSignatureCache.Len())
	it := db.NewIterator()
	defer it.Release()
	require.False(t, it.Next())

	// ensure all messages have been deleted
	for _, messageID := range messageIDs {
		_, err := backend.GetMessageSignature(messageID)
		require.ErrorContains(t, err, "failed to get warp message")
	}
}

func TestAddAndGetValidMessage(t *testing.T) {
	db := memdb.New()

	sk, err := bls.NewSecretKey()
	require.NoError(t, err)
	warpSigner := avalancheWarp.NewSigner(sk, networkID, sourceChainID)
	backend := NewBackend(networkID, sourceChainID, warpSigner, nil, db, 500)

	// Create a new unsigned message and add it to the warp backend.
	unsignedMsg, err := avalancheWarp.NewUnsignedMessage(networkID, sourceChainID, testPayload)
	require.NoError(t, err)
	err = backend.AddMessage(unsignedMsg)
	require.NoError(t, err)

	// Verify that a signature is returned successfully, and compare to expected signature.
	messageID := unsignedMsg.ID()
	signature, err := backend.GetMessageSignature(messageID)
	require.NoError(t, err)

	expectedSig, err := warpSigner.Sign(unsignedMsg)
	require.NoError(t, err)
	require.Equal(t, expectedSig, signature[:])
}

func TestAddAndGetUnknownMessage(t *testing.T) {
	db := memdb.New()

	sk, err := bls.NewSecretKey()
	require.NoError(t, err)
	warpSigner := avalancheWarp.NewSigner(sk, networkID, sourceChainID)
	backend := NewBackend(networkID, sourceChainID, warpSigner, nil, db, 500)
	unsignedMsg, err := avalancheWarp.NewUnsignedMessage(networkID, sourceChainID, testPayload)
	require.NoError(t, err)

	// Try getting a signature for a message that was not added.
	messageID := unsignedMsg.ID()
	_, err = backend.GetMessageSignature(messageID)
	require.Error(t, err)
}

func TestGetBlockSignature(t *testing.T) {
	require := require.New(t)

	blkID := ids.GenerateTestID()
	testVM := &block.TestVM{
		TestVM: common.TestVM{T: t},
		GetBlockF: func(ctx context.Context, i ids.ID) (snowman.Block, error) {
			if i == blkID {
				return &snowman.TestBlock{
					TestDecidable: choices.TestDecidable{
						IDV:     blkID,
						StatusV: choices.Accepted,
					},
				}, nil
			}
			return nil, errors.New("invalid blockID")
		},
	}
	db := memdb.New()

	sk, err := bls.NewSecretKey()
	require.NoError(err)
	warpSigner := avalancheWarp.NewSigner(sk, networkID, sourceChainID)
	backend := NewBackend(networkID, sourceChainID, warpSigner, testVM, db, 500)

	blockHashPayload, err := payload.NewHash(blkID)
	require.NoError(err)
	unsignedMessage, err := avalancheWarp.NewUnsignedMessage(networkID, sourceChainID, blockHashPayload.Bytes())
	require.NoError(err)
	expectedSig, err := warpSigner.Sign(unsignedMessage)
	require.NoError(err)

	signature, err := backend.GetBlockSignature(blkID)
	require.NoError(err)
	require.Equal(expectedSig, signature[:])

	_, err = backend.GetBlockSignature(ids.GenerateTestID())
	require.Error(err)
}

func TestZeroSizedCache(t *testing.T) {
	db := memdb.New()

	sk, err := bls.NewSecretKey()
	require.NoError(t, err)
	warpSigner := avalancheWarp.NewSigner(sk, networkID, sourceChainID)

	// Verify zero sized cache works normally, because the lru cache will be initialized to size 1 for any size parameter <= 0.
	backend := NewBackend(networkID, sourceChainID, warpSigner, nil, db, 0)

	// Create a new unsigned message and add it to the warp backend.
	unsignedMsg, err := avalancheWarp.NewUnsignedMessage(networkID, sourceChainID, testPayload)
	require.NoError(t, err)
	err = backend.AddMessage(unsignedMsg)
	require.NoError(t, err)

	// Verify that a signature is returned successfully, and compare to expected signature.
	messageID := unsignedMsg.ID()
	signature, err := backend.GetMessageSignature(messageID)
	require.NoError(t, err)

	expectedSig, err := warpSigner.Sign(unsignedMsg)
	require.NoError(t, err)
	require.Equal(t, expectedSig, signature[:])
}
