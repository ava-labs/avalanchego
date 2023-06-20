// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/avm/blocks/executor"
)

func TestWalletService_SendMultiple(t *testing.T) {
	require := require.New(t)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			env := setup(t, tc.avaxAsset)
			defer func() {
				require.NoError(env.vm.Shutdown(context.Background()))
				env.vm.ctx.Lock.Unlock()
			}()

			assetID := env.genesisTx.ID()
			addr := keys[0].PublicKey().Address()

			addrStr, err := env.vm.FormatLocalAddress(addr)
			require.NoError(err)
			changeAddrStr, err := env.vm.FormatLocalAddress(testChangeAddr)
			require.NoError(err)
			_, fromAddrsStr := sampleAddrs(t, env.vm, addrs)

			args := &SendMultipleArgs{
				JSONSpendHeader: api.JSONSpendHeader{
					UserPass: api.UserPass{
						Username: username,
						Password: password,
					},
					JSONFromAddrs:  api.JSONFromAddrs{From: fromAddrsStr},
					JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddrStr},
				},
				Outputs: []SendOutput{
					{
						Amount:  500,
						AssetID: assetID.String(),
						To:      addrStr,
					},
					{
						Amount:  1000,
						AssetID: assetID.String(),
						To:      addrStr,
					},
				},
			}
			reply := &api.JSONTxIDChangeAddr{}
			require.NoError(env.walletService.SendMultiple(nil, args, reply))
			require.Equal(changeAddrStr, reply.ChangeAddr)

			require.Equal(common.PendingTxs, <-env.issuer)

			blkIntf, err := env.vm.BuildBlock(context.Background())
			require.NoError(err)

			blk := blkIntf.(*executor.Block)
			txs := blk.Txs()
			require.Len(txs, 1)
			require.Equal(txs[0].ID(), reply.TxID)

			require.NoError(blk.Verify(context.Background()))
			require.NoError(blk.Accept(context.Background()))

			_, err = env.vm.state.GetTx(reply.TxID)
			require.NoError(err)
		})
	}
}
