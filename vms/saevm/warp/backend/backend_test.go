// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package backend

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

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

func TestAddMessage(t *testing.T) {
	sk, err := localsigner.New()
	require.NoError(t, err)
	warpSigner := avalancheWarp.NewSigner(sk, networkID, sourceChainID)
	backend, _, err := New(networkID, sourceChainID, warpSigner, nil, memdb.New(), nil)
	require.NoError(t, err)

	require.NoError(t, backend.AddMessage(testUnsignedMessage))
}

func TestOffChainMessages(t *testing.T) {
	type test struct {
		offchainMessages [][]byte
		check            func(require *require.Assertions, b *Backend)
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
			check: func(require *require.Assertions, b *Backend) {
				msg, err := b.getMessage(testUnsignedMessage.ID())
				require.NoError(err)
				require.Equal(testUnsignedMessage.Bytes(), msg.Bytes())
			},
		},
		"unknown message": {
			check: func(require *require.Assertions, b *Backend) {
				_, err := b.getMessage(testUnsignedMessage.ID())
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

			b, _, err := New(networkID, sourceChainID, warpSigner, nil, memdb.New(), test.offchainMessages)
			require.ErrorIs(err, test.err)
			if test.check != nil {
				test.check(require, b)
			}
		})
	}
}
