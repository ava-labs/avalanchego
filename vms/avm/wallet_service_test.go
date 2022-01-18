// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"container/list"
	"testing"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/keystore"
)

// Returns:
// 1) genesis bytes of vm
// 2) the VM
// 3) The wallet service that wraps the VM
// 4) atomic memory to use in tests
func setupWS(t *testing.T, isAVAXAsset bool) ([]byte, *VM, *WalletService, *atomic.Memory, *Tx) {
	var genesisBytes []byte
	var vm *VM
	var m *atomic.Memory
	var genesisTx *Tx
	if isAVAXAsset {
		genesisBytes, _, vm, m = GenesisVM(t)
		genesisTx = GetAVAXTxFromGenesisTest(genesisBytes, t)
	} else {
		genesisBytes, _, vm, m = setupTxFeeAssets(t)
		genesisTx = GetCreateTxFromGenesisTest(t, genesisBytes, feeAssetName)
	}

	ws := &WalletService{vm: vm, pendingTxMap: make(map[ids.ID]*list.Element), pendingTxOrdering: list.New()}
	return genesisBytes, vm, ws, m, genesisTx
}

// Returns:
// 1) genesis bytes of vm
// 2) the VM
// 3) The wallet service that wraps the VM
// 4) atomic memory to use in tests
func setupWSWithKeys(t *testing.T, isAVAXAsset bool) ([]byte, *VM, *WalletService, *atomic.Memory, *Tx) {
	genesisBytes, vm, ws, m, tx := setupWS(t, isAVAXAsset)

	// Import the initially funded private keys
	user, err := keystore.NewUserFromKeystore(ws.vm.ctx.Keystore, username, password)
	if err != nil {
		t.Fatal(err)
	}

	if err := user.PutKeys(keys...); err != nil {
		t.Fatalf("Failed to set key for user: %s", err)
	}

	if err := user.Close(); err != nil {
		t.Fatal(err)
	}
	return genesisBytes, vm, ws, m, tx
}

func TestWalletService_SendMultiple(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, vm, ws, _, genesisTx := setupWSWithKeys(t, tc.avaxAsset)
			defer func() {
				if err := vm.Shutdown(); err != nil {
					t.Fatal(err)
				}
				vm.ctx.Lock.Unlock()
			}()

			assetID := genesisTx.ID()
			addr := keys[0].PublicKey().Address()

			addrStr, err := vm.FormatLocalAddress(addr)
			if err != nil {
				t.Fatal(err)
			}
			changeAddrStr, err := vm.FormatLocalAddress(testChangeAddr)
			if err != nil {
				t.Fatal(err)
			}
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
			if err := ws.SendMultiple(nil, args, reply); err != nil {
				t.Fatalf("Failed to send transaction: %s", err)
			} else if reply.ChangeAddr != changeAddrStr {
				t.Fatalf("expected change address to be %s but got %s", changeAddrStr, reply.ChangeAddr)
			}

			pendingTxs := vm.txs
			if len(pendingTxs) != 1 {
				t.Fatalf("Expected to find 1 pending tx after send, but found %d", len(pendingTxs))
			}

			if reply.TxID != pendingTxs[0].ID() {
				t.Fatal("Transaction ID returned by SendMultiple does not match the transaction found in vm's pending transactions")
			}

			if _, err = vm.GetTx(reply.TxID); err != nil {
				t.Fatalf("Failed to retrieve created transaction: %s", err)
			}
		})
	}
}
