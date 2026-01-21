// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"testing"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/stretchr/testify/require"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"errors"
	evmwarp "github.com/ava-labs/avalanchego/vms/evm/warp"
	"github.com/ava-labs/avalanchego/database"
	"context"
)

func TestServiceGetMessageSignature(t *testing.T) {
	tests := []struct {
		name    string
		msgInDB  *warp.UnsignedMessage
		// offChainMsgs [][]byte
		getMsgID ids.ID
		wantErr  error
	} {
		{
			name:     "unknown message",
			getMsgID: ids.GenerateTestID(),
			wantErr:  ErrMessageNotFound,
		},
		{
			name: "known message",
			msgInDB: func() *warp.UnsignedMessage{
				msg, err := warp.NewUnsignedMessage(0, ids.Empty, []byte{})
				require.NoError(t, err)
				return msg
			}(),
			getMsgID: func() ids.ID{
				msg, err := warp.NewUnsignedMessage(0, ids.Empty, []byte{})
				require.NoError(t, err)
				return msg.ID()
			}(),
		},
		// {
		// 	name: "off-chain message",
		// 	offChainMsgs: [][]byte{
		// 		func() []byte{
		// 			uptimeBytes, err := (&evmwarp.ValidatorUptime{}).Bytes()
		// 			require.NoError(t, err)
		//
		// 			addressedCall, err := payload.NewAddressedCall([]byte{}, uptimeBytes)
		// 			require.NoError(t, err)
		//
		// 			msg, err := warp.NewUnsignedMessage(0, ids.Empty, addressedCall.Bytes())
		// 			require.NoError(t, err)
		//
		// 			return msg.Bytes()
		// 		}(),
		// 	},
		// 	getMsgID: func() ids.ID{
		// 		uptimeBytes, err := (&evmwarp.ValidatorUptime{}).Bytes()
		// 		require.NoError(t, err)
		//
		// 		addressedCall, err := payload.NewAddressedCall([]byte{}, uptimeBytes)
		// 		require.NoError(t, err)
		//
		// 		msg, err := warp.NewUnsignedMessage(0, ids.Empty, addressedCall.Bytes())
		// 		require.NoError(t, err)
		// 		return msg.ID()
		// 	}(),
		// },
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := evmwarp.NewDB(memdb.New())
			signer, err := localsigner.New()
			require.NoError(t, err)

			verifier, err := evmwarp.NewVerifier(nil, db, prometheus.NewRegistry())
			require.NoError(t, err)

			warpSigner := warp.NewSigner(signer, 0, ids.Empty)
			service, err := NewService(
				0,
				ids.Empty,
				&validatorstest.State{},
				db,
				warpSigner,
				verifier,
				&cache.Empty[ids.ID, []byte]{},
				acp118.NewSignatureAggregator(logging.NoLog{}, nil),
				nil,
			)
			require.NoError(t, err)

			if tt.msgInDB != nil {
				require.NoError(t, db.Add(tt.msgInDB))
			}

			gotSig, err := service.GetMessageSignature(t.Context(), tt.getMsgID)
			require.ErrorIs(t, err, tt.wantErr)

			if tt.wantErr != nil {
				return
			}

			wantSig, err := warpSigner.Sign(tt.msgInDB)
			require.NoError(t, err)

			require.Equal(t, hexutil.Bytes(wantSig), gotSig)
		})
	}
}

func TestServiceGetBlockSignature(t *testing.T) {
	tests := []struct {
		name    string
		blksInDB []ids.ID
		getBlkID ids.ID
		wantErr  error
	} {
		{
			name:     "unknown block",
			getBlkID: ids.GenerateTestID(),
			wantErr:  errors.New("todo: return an error here to fix this test"),
		},
		{
			name: "known block",
			blksInDB: []ids.ID{ids.Empty},
			getBlkID: ids.Empty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := evmwarp.NewDB(memdb.New())
			signer, err := localsigner.New()
			require.NoError(t, err)

			verifier, err := evmwarp.NewVerifier(
				// testBlockStore(set.Of(tt.blksInDB...)),
				nil,
				db,
				prometheus.NewRegistry(),
			)
			require.NoError(t, err)

			warpSigner := warp.NewSigner(signer, 0, ids.Empty)
			service, err := NewService(
				0,
				ids.Empty,
				&validatorstest.State{},
				db,
				warpSigner,
				verifier,
				&cache.Empty[ids.ID, []byte]{},
				acp118.NewSignatureAggregator(logging.NoLog{}, nil),
				[][]byte{},
			)
			require.NoError(t, err)

			gotSig, err := service.GetBlockSignature(t.Context(), tt.getBlkID)
			require.ErrorIs(t, err, tt.wantErr)

			if tt.wantErr != nil {
				return
			}

			hash, err := payload.NewHash(tt.getBlkID)
			require.NoError(t, err)

			wantMsg, err := warp.NewUnsignedMessage(0, ids.Empty, hash.Bytes())
			require.NoError(t, err)

			wantSig, err := warpSigner.Sign(wantMsg)
			require.NoError(t, err)

			require.Equal(t, hexutil.Bytes(wantSig), gotSig)
		})
	}
}

type testBlockStore set.Set[ids.ID]

func (t testBlockStore) HasBlock(_ context.Context, blockID ids.ID) error {
	s := set.Set[ids.ID](t)
	if !s.Contains(blockID) {
		return database.ErrNotFound
	}
	return nil
}
