// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"testing"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/stretchr/testify/require"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	evmwarp "github.com/ava-labs/avalanchego/vms/evm/warp"
	"context"
	"errors"
	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
)

func TestServiceGetMessage(t *testing.T) {
	tests := []struct {
		name         string
		msgsInDB     []*warp.UnsignedMessage
		offChainMsgs [][]byte
		getMsgID ids.ID
		want         []byte
		wantErr  error
	}{
		{
			name:     "unknown message",
			getMsgID: ids.GenerateTestID(),
			wantErr: database.ErrNotFound,
		},
		{
			name: "message in db",
			msgsInDB: []*warp.UnsignedMessage{
				func() *warp.UnsignedMessage {
					msg, err := warp.NewUnsignedMessage(0, ids.Empty, []byte{})
					require.NoError(t, err)
					return msg
				}(),
			},
			getMsgID: func() ids.ID {
				msg, err := warp.NewUnsignedMessage(0, ids.Empty, []byte{})
				require.NoError(t, err)
				return msg.ID()
			}(),
			want: func() []byte {
				msg, err := warp.NewUnsignedMessage(0, ids.Empty, []byte{})
				require.NoError(t, err)
				return msg.Bytes()
			}(),
		},
		{
			name: "off-chain message",
			offChainMsgs: [][]byte{
				func() []byte {
					addressedCall, err := payload.NewAddressedCall([]byte{}, []byte("foo"))
					require.NoError(t, err)

					msg, err := warp.NewUnsignedMessage(0, ids.Empty, addressedCall.Bytes())
					require.NoError(t, err)

					return msg.Bytes()
				}(),
			},
			getMsgID: func() ids.ID {
				addressedCall, err := payload.NewAddressedCall([]byte{}, []byte("foo"))
				require.NoError(t, err)

				msg, err := warp.NewUnsignedMessage(0, ids.Empty, addressedCall.Bytes())
				require.NoError(t, err)

				return msg.ID()
			}(),
			want: func() []byte {
				addressedCall, err := payload.NewAddressedCall([]byte{}, []byte("foo"))
				require.NoError(t, err)

				msg, err := warp.NewUnsignedMessage(0, ids.Empty, addressedCall.Bytes())
				require.NoError(t, err)

				return msg.Bytes()
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := evmwarp.NewDB(memdb.New())

			offChainMsgs, err := evmwarp.NewOffChainMessages(
				0,
				ids.Empty,
				tt.offChainMsgs,
			)
			require.NoError(t, err)

			service, err := NewService(
				0,
				ids.Empty,
				nil,
				db,
				nil,
				nil,
				nil,
				nil,
				offChainMsgs,
			)
			require.NoError(t, err)

			for _, msg := range tt.msgsInDB {
				require.NoError(t, db.Add(msg))
			}

			got, err := service.GetMessage(tt.getMsgID)
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, hexutil.Bytes(tt.want), got)
		})
	}
}

func TestServiceGetMessageSignature(t *testing.T) {
	tests := []struct {
		name         string
		msgsInDB     []*warp.UnsignedMessage
		offChainMsgs [][]byte
		getMsgID     ids.ID
		wantMsg      *warp.UnsignedMessage
		wantErr  error
	} {
		{
			name:     "unknown message",
			getMsgID: ids.GenerateTestID(),
			wantErr:  ErrMessageNotFound,
		},
		{
			name: "message in db",
			msgsInDB: []*warp.UnsignedMessage{
				func() *warp.UnsignedMessage {
					msg, err := warp.NewUnsignedMessage(0, ids.Empty, []byte{})
					require.NoError(t, err)
					return msg
				}(),
			},
			getMsgID: func() ids.ID {
				msg, err := warp.NewUnsignedMessage(0, ids.Empty, []byte{})
				require.NoError(t, err)
				return msg.ID()
			}(),
			wantMsg: func() *warp.UnsignedMessage {
				msg, err := warp.NewUnsignedMessage(0, ids.Empty, []byte{})
				require.NoError(t, err)
				return msg
			}(),
		},
		{
			name: "off-chain message",
			offChainMsgs: [][]byte{
				func() []byte {
					addressedCall, err := payload.NewAddressedCall([]byte{}, []byte("foo"))
					require.NoError(t, err)

					msg, err := warp.NewUnsignedMessage(0, ids.Empty, addressedCall.Bytes())
					require.NoError(t, err)

					return msg.Bytes()
				}(),
			},
			getMsgID: func() ids.ID {
				addressedCall, err := payload.NewAddressedCall([]byte{}, []byte("foo"))
				require.NoError(t, err)

				msg, err := warp.NewUnsignedMessage(0, ids.Empty, addressedCall.Bytes())
				require.NoError(t, err)

				return msg.ID()
			}(),
			wantMsg: func() *warp.UnsignedMessage {
				addressedCall, err := payload.NewAddressedCall([]byte{}, []byte("foo"))
				require.NoError(t, err)

				msg, err := warp.NewUnsignedMessage(0, ids.Empty, addressedCall.Bytes())
				require.NoError(t, err)

				return msg
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := evmwarp.NewDB(memdb.New())
			signer, err := localsigner.New()
			require.NoError(t, err)

			offChainMsgs, err := evmwarp.NewOffChainMessages(
				0,
				ids.Empty,
				tt.offChainMsgs,
			)
			require.NoError(t, err)

			verifier, err := evmwarp.NewVerifier(
				evmwarp.NoVerifier{},
				nil,
				db,
				offChainMsgs,
				prometheus.NewRegistry(),
			)
			require.NoError(t, err)

			warpSigner := warp.NewSigner(signer, 0, ids.Empty)
			service, err := NewService(
				0,
				ids.Empty,
				nil,
				db,
				warpSigner,
				verifier,
				&cache.Empty[ids.ID, []byte]{},
				nil,
				offChainMsgs,
			)
			require.NoError(t, err)

			for _, msg := range tt.msgsInDB {
				require.NoError(t, db.Add(msg))
			}

			gotSig, err := service.GetMessageSignature(t.Context(), tt.getMsgID)
			require.ErrorIs(t, err, tt.wantErr)

			if tt.wantErr != nil {
				return
			}

			wantSig, err := warpSigner.Sign(tt.wantMsg)
			require.NoError(t, err)

			require.Equal(t, hexutil.Bytes(wantSig), gotSig)
		})
	}
}

// func TestServiceGetBlockSignature(t *testing.T) {
// 	tests := []struct {
// 		name    string
// 		blksInDB []ids.ID
// 		getBlkID ids.ID
// 		wantErr  error
// 	} {
// 		{
// 			name:     "unknown block",
// 			getBlkID: ids.GenerateTestID(),
// 			wantErr:  errors.New("todo: return an error here to fix this test"),
// 		},
// 		{
// 			name: "known block",
// 			blksInDB: []ids.ID{ids.Empty},
// 			getBlkID: ids.Empty,
// 		},
// 	}
//
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			db := evmwarp.NewDB(memdb.New())
// 			signer, err := localsigner.New()
// 			require.NoError(t, err)
//
// 			verifier, err := evmwarp.NewVerifier(
// 				evmwarp.NoVerifier{},
// 				nil,
// 				db,
// 				evmwarp.OffChainMessages{},
// 				prometheus.NewRegistry(),
// 			)
// 			require.NoError(t, err)
//
// 			warpSigner := warp.NewSigner(signer, 0, ids.Empty)
// 			service, err := NewService(
// 				0,
// 				ids.Empty,
// 				&validatorstest.State{},
// 				db,
// 				warpSigner,
// 				verifier,
// 				&cache.Empty[ids.ID, []byte]{},
// 				acp118.NewSignatureAggregator(logging.NoLog{}, nil),
// 				offChainMsgs,
// 			)
// 			require.NoError(t, err)
//
// 			gotSig, err := service.GetBlockSignature(t.Context(), tt.getBlkID)
// 			require.ErrorIs(t, err, tt.wantErr)
//
// 			if tt.wantErr != nil {
// 				return
// 			}
//
// 			hash, err := payload.NewHash(tt.getBlkID)
// 			require.NoError(t, err)
//
// 			wantMsg, err := warp.NewUnsignedMessage(0, ids.Empty, hash.Bytes())
// 			require.NoError(t, err)
//
// 			wantSig, err := warpSigner.Sign(wantMsg)
// 			require.NoError(t, err)
//
// 			require.Equal(t, hexutil.Bytes(wantSig), gotSig)
// 		})
// 	}
// }

type testVM struct {
	blks set.Set[ids.ID]
}

var _ evmwarp.VM = (*testVM)(nil)

func (t testVM) HasBlock(_ context.Context, blkID ids.ID) error {
	if !t.blks.Contains(blkID) {
		return errors.New("not found")
	}

	return nil
}
