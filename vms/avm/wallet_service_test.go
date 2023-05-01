// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/keystore"
)

// Returns:
// 1) genesis bytes of vm
// 2) the VM
// 3) The wallet service that wraps the VM
// 4) atomic memory to use in tests
func setupWS(t *testing.T, isAVAXAsset bool) ([]byte, *VM, *WalletService, *atomic.Memory, *txs.Tx) {
	var genesisBytes []byte
	var vm *VM
	var m *atomic.Memory
	var genesisTx *txs.Tx
	if isAVAXAsset {
		genesisBytes, _, vm, m = GenesisVM(t)
		genesisTx = GetAVAXTxFromGenesisTest(genesisBytes, t)
	} else {
		genesisBytes, _, vm, m = setupTxFeeAssets(t)
		genesisTx = GetCreateTxFromGenesisTest(t, genesisBytes, feeAssetName)
	}

	ws := &WalletService{
		vm:         vm,
		pendingTxs: linkedhashmap.New[ids.ID, *txs.Tx](),
	}
	return genesisBytes, vm, ws, m, genesisTx
}

// Returns:
// 1) genesis bytes of vm
// 2) the VM
// 3) The wallet service that wraps the VM
// 4) atomic memory to use in tests
func setupWSWithKeys(t *testing.T, isAVAXAsset bool) ([]byte, *VM, *WalletService, *atomic.Memory, *txs.Tx) {
	require := require.New(t)

	genesisBytes, vm, ws, m, tx := setupWS(t, isAVAXAsset)

	// Import the initially funded private keys
	user, err := keystore.NewUserFromKeystore(ws.vm.ctx.Keystore, username, password)
	require.NoError(err)

	require.NoError(user.PutKeys(keys...))

	require.NoError(user.Close())
	return genesisBytes, vm, ws, m, tx
}

func TestWalletService_SendMultiple(t *testing.T) {
	require := require.New(t)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, vm, ws, _, genesisTx := setupWSWithKeys(t, tc.avaxAsset)
			defer func() {
				require.NoError(vm.Shutdown(context.Background()))
				vm.ctx.Lock.Unlock()
			}()

			assetID := genesisTx.ID()
			addr := keys[0].PublicKey().Address()

			addrStr, err := vm.FormatLocalAddress(addr)
			require.NoError(err)
			changeAddrStr, err := vm.FormatLocalAddress(testChangeAddr)
			require.NoError(err)
			_, fromAddrsStr := sampleAddrs(t, vm, addrs)

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
			vm.timer.Cancel()
			require.NoError(ws.SendMultiple(nil, args, reply))
			require.Equal(changeAddrStr, reply.ChangeAddr)

			pendingTxs := vm.txs
			require.Len(pendingTxs, 1)

			require.Equal(pendingTxs[0].ID(), reply.TxID)

			_, err = vm.GetTx(context.Background(), reply.TxID)
			require.NoError(err)
		})
	}
}
