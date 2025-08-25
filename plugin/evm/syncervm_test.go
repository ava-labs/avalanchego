// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	"github.com/ava-labs/coreth/plugin/evm/vmtest"
)

func TestEVMSyncerVM(t *testing.T) {
	for _, test := range vmtest.SyncerVMTests {
		t.Run(test.Name, func(t *testing.T) {
			genFn := func(_ int, vm extension.InnerVM, gen *core.BlockGen) {
				br := predicate.BlockResults{}
				b, err := br.Bytes()
				require.NoError(t, err)
				gen.AppendExtra(b)

				tx := types.NewTransaction(gen.TxNonce(vmtest.TestEthAddrs[0]), vmtest.TestEthAddrs[1], common.Big1, params.TxGas, vmtest.InitialBaseFee, nil)
				signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.Ethereum().BlockChain().Config().ChainID), vmtest.TestKeys[0].ToECDSA())
				require.NoError(t, err)
				gen.AddTx(signedTx)
			}
			newVMFn := func() (extension.InnerVM, dummy.ConsensusCallbacks) {
				vm := newDefaultTestVM()
				return vm, vm.extensionConfig.ConsensusCallbacks
			}

			testSetup := &vmtest.SyncTestSetup{
				NewVM:             newVMFn,
				GenFn:             genFn,
				ExtraSyncerVMTest: nil,
			}
			test.TestFunc(t, testSetup)
		})
	}
}
