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
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/hashing"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/stretchr/testify/require"
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

func TestClearDB(t *testing.T) {
	db := memdb.New()

	sk, err := bls.NewSecretKey()
	require.NoError(t, err)
	warpSigner := avalancheWarp.NewSigner(sk, networkID, sourceChainID)
	backendIntf, err := NewBackend(networkID, sourceChainID, warpSigner, nil, db, 500, nil)
	require.NoError(t, err)
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
	backend, err := NewBackend(networkID, sourceChainID, warpSigner, nil, db, 500, nil)
	require.NoError(t, err)

	// Add testUnsignedMessage to the warp backend
	err = backend.AddMessage(testUnsignedMessage)
	require.NoError(t, err)

	// Verify that a signature is returned successfully, and compare to expected signature.
	messageID := testUnsignedMessage.ID()
	signature, err := backend.GetMessageSignature(messageID)
	require.NoError(t, err)

	expectedSig, err := warpSigner.Sign(testUnsignedMessage)
	require.NoError(t, err)
	require.Equal(t, expectedSig, signature[:])
}

func TestAddAndGetUnknownMessage(t *testing.T) {
	db := memdb.New()

	sk, err := bls.NewSecretKey()
	require.NoError(t, err)
	warpSigner := avalancheWarp.NewSigner(sk, networkID, sourceChainID)
	backend, err := NewBackend(networkID, sourceChainID, warpSigner, nil, db, 500, nil)
	require.NoError(t, err)

	// Try getting a signature for a message that was not added.
	messageID := testUnsignedMessage.ID()
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
	backend, err := NewBackend(networkID, sourceChainID, warpSigner, testVM, db, 500, nil)
	require.NoError(err)

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
	backend, err := NewBackend(networkID, sourceChainID, warpSigner, nil, db, 0, nil)
	require.NoError(t, err)

	// Add testUnsignedMessage to the warp backend
	err = backend.AddMessage(testUnsignedMessage)
	require.NoError(t, err)

	// Verify that a signature is returned successfully, and compare to expected signature.
	messageID := testUnsignedMessage.ID()
	signature, err := backend.GetMessageSignature(messageID)
	require.NoError(t, err)

	expectedSig, err := warpSigner.Sign(testUnsignedMessage)
	require.NoError(t, err)
	require.Equal(t, expectedSig, signature[:])
}

func TestOffChainMessages(t *testing.T) {
	type test struct {
		offchainMessages [][]byte
		check            func(require *require.Assertions, b Backend)
		err              error
	}
	sk, err := bls.NewSecretKey()
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

				signature, err := b.GetMessageSignature(testUnsignedMessage.ID())
				require.NoError(err)
				expectedSignatureBytes, err := warpSigner.Sign(msg)
				require.NoError(err)
				require.Equal(expectedSignatureBytes, signature[:])
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

			backend, err := NewBackend(networkID, sourceChainID, warpSigner, nil, db, 0, test.offchainMessages)
			require.ErrorIs(err, test.err)
			if test.check != nil {
				test.check(require, backend)
			}
		})
	}
}
