// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/linked"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
)

func TestWalletService_SendMultiple(t *testing.T) {
	require := require.New(t)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			env := setup(t, &envConfig{
				fork:             upgradetest.Latest,
				isCustomFeeAsset: !tc.avaxAsset,
				keystoreUsers: []*user{{
					username:    username,
					password:    password,
					initialKeys: keys,
				}},
			})
			env.vm.ctx.Lock.Unlock()

			walletService := &WalletService{
				vm:         env.vm,
				pendingTxs: linked.NewHashmap[ids.ID, *txs.Tx](),
			}

			assetID := env.genesisTx.ID()
			addr := keys[0].PublicKey().Address()

			addrStr, err := env.vm.FormatLocalAddress(addr)
			require.NoError(err)
			changeAddrStr, err := env.vm.FormatLocalAddress(testChangeAddr)
			require.NoError(err)
			_, fromAddrsStr := sampleAddrs(t, env.vm.AddressManager, addrs)

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
			require.NoError(walletService.SendMultiple(nil, args, reply))
			require.Equal(changeAddrStr, reply.ChangeAddr)

			buildAndAccept(require, env.vm, env.issuer, reply.TxID)

			env.vm.ctx.Lock.Lock()
			_, err = env.vm.state.GetTx(reply.TxID)
			env.vm.ctx.Lock.Unlock()
			require.NoError(err)
		})
	}
}
