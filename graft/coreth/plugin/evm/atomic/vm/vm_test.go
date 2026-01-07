// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/txpool"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/extension"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap0"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap1"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/vmtest"
	"github.com/ava-labs/avalanchego/graft/coreth/utils/utilstest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
)

func TestMain(m *testing.M) {
	customtypes.Register()
	params.RegisterExtras()
	os.Exit(m.Run())
}

func newAtomicTestVM() *VM {
	return WrapVM(&evm.VM{})
}

func (vm *VM) newImportTx(
	chainID ids.ID, // chain to import from
	to common.Address, // Address of recipient
	baseFee *big.Int, // fee to use post-AP3
	keys []*secp256k1.PrivateKey, // Keys to import the funds
) (*atomic.Tx, error) {
	kc := secp256k1fx.NewKeychain(keys...)
	atomicUTXOs, _, _, err := avax.GetAtomicUTXOs(vm.Ctx.SharedMemory, atomic.Codec, chainID, kc.Addresses(), ids.ShortEmpty, ids.Empty, maxUTXOsToFetch)
	if err != nil {
		return nil, fmt.Errorf("problem retrieving atomic UTXOs: %w", err)
	}

	return atomic.NewImportTx(vm.Ctx, vm.CurrentRules(), vm.clock.Unix(), chainID, to, baseFee, kc, atomicUTXOs)
}

func addUTXO(sharedMemory *avalancheatomic.Memory, ctx *snow.Context, txID ids.ID, index uint32, assetID ids.ID, amount uint64, addr ids.ShortID) (*avax.UTXO, error) {
	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        txID,
			OutputIndex: index,
		},
		Asset: avax.Asset{ID: assetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: amount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{addr},
			},
		},
	}
	utxoBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, utxo)
	if err != nil {
		return nil, err
	}

	xChainSharedMemory := sharedMemory.NewSharedMemory(ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			addr.Bytes(),
		},
	}}}}); err != nil {
		return nil, err
	}

	return utxo, nil
}

func addUTXOs(sharedMemory *avalancheatomic.Memory, ctx *snow.Context, utxos map[ids.ShortID]uint64) error {
	for addr, avaxAmount := range utxos {
		txID, err := ids.ToID(hashing.ComputeHash256(addr.Bytes()))
		if err != nil {
			return fmt.Errorf("Failed to generate txID from addr: %w", err)
		}
		if _, err := addUTXO(sharedMemory, ctx, txID, 0, ctx.AVAXAssetID, avaxAmount, addr); err != nil {
			return fmt.Errorf("Failed to add UTXO to shared memory: %w", err)
		}
	}
	return nil
}

func TestImportMissingUTXOs(t *testing.T) {
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testImportMissingUTXOs(t, scheme)
		})
	}
}

func testImportMissingUTXOs(t *testing.T, scheme string) {
	// make a VM with a shared memory that has an importable UTXO to build a block
	require := require.New(t)
	importAmount := uint64(50000000)
	fork := upgradetest.ApricotPhase2
	vm1 := newAtomicTestVM()
	tvm := vmtest.SetupTestVM(t, vm1, vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	})
	require.NoError(addUTXOs(tvm.AtomicMemory, vm1.Ctx, map[ids.ShortID]uint64{
		vmtest.TestShortIDAddrs[0]: importAmount,
	}))
	defer func() {
		require.NoError(vm1.Shutdown(t.Context()))
	}()

	importTx, err := vm1.newImportTx(vm1.Ctx.XChainID, vmtest.TestEthAddrs[0], vmtest.InitialBaseFee, vmtest.TestKeys[0:1])
	require.NoError(err)
	require.NoError(vm1.mempool.AddLocalTx(importTx))
	msg, err := vm1.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)
	blk, err := vm1.BuildBlock(t.Context())
	require.NoError(err)

	// make another VM which is missing the UTXO in shared memory
	vm2 := newAtomicTestVM()
	vmtest.SetupTestVM(t, vm2, vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	})
	defer func() {
		require.NoError(vm2.Shutdown(t.Context()))
	}()

	vm2Blk, err := vm2.ParseBlock(t.Context(), blk.Bytes())
	require.NoError(err)
	err = vm2Blk.Verify(t.Context())
	require.ErrorIs(err, ErrMissingUTXOs)

	// This should not result in a bad block since the missing UTXO should
	// prevent InsertBlockManual from being called.
	badBlocks, _ := vm2.Ethereum().BlockChain().BadBlocks()
	require.Empty(badBlocks)
}

// Simple test to ensure we can issue an import transaction followed by an export transaction
// and they will be indexed correctly when accepted.
func TestIssueAtomicTxs(t *testing.T) {
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testIssueAtomicTxs(t, scheme)
		})
	}
}

func testIssueAtomicTxs(t *testing.T, scheme string) {
	require := require.New(t)
	importAmount := uint64(50000000)
	vm := newAtomicTestVM()
	fork := upgradetest.ApricotPhase2
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	})
	utxos := map[ids.ShortID]uint64{
		vmtest.TestShortIDAddrs[0]: importAmount,
	}
	require.NoError(addUTXOs(tvm.AtomicMemory, vm.Ctx, utxos))
	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	importTx, err := vm.newImportTx(vm.Ctx.XChainID, vmtest.TestEthAddrs[0], vmtest.InitialBaseFee, vmtest.TestKeys[0:1])
	require.NoError(err)
	require.NoError(vm.mempool.AddLocalTx(importTx))

	msg, err := vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)

	blk, err := vm.BuildBlock(t.Context())
	require.NoError(err)
	require.NoError(blk.Verify(t.Context()))
	require.NoError(vm.SetPreference(t.Context(), blk.ID()))
	require.NoError(blk.Accept(t.Context()))

	lastAcceptedID, err := vm.LastAccepted(t.Context())
	require.NoError(err)
	require.Equal(lastAcceptedID, blk.ID())
	vm.Ethereum().BlockChain().DrainAcceptorQueue()

	state, err := vm.Ethereum().BlockChain().State()
	require.NoError(err)

	wrappedState := extstate.New(state)
	exportTx, err := atomic.NewExportTx(vm.Ctx, vm.CurrentRules(), wrappedState, vm.Ctx.AVAXAssetID, importAmount-(2*ap0.AtomicTxFee), vm.Ctx.XChainID, vmtest.TestShortIDAddrs[0], vmtest.InitialBaseFee, vmtest.TestKeys[0:1])
	require.NoError(err)
	require.NoError(vm.mempool.AddLocalTx(exportTx))

	msg, err = vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)

	blk2, err := vm.BuildBlock(t.Context())
	require.NoError(err)
	require.NoError(blk2.Verify(t.Context()))
	require.NoError(blk2.Accept(t.Context()))

	lastAcceptedID, err = vm.LastAccepted(t.Context())
	require.NoError(err)
	require.Equal(lastAcceptedID, blk2.ID())

	s := AvaxAPI{
		Context:      vm.Ctx,
		Mempool:      vm.mempool,
		PushGossiper: vm.pushGossiper,
		AcceptedTxs:  vm.AtomicTxRepository,
	}

	// Check that both atomic transactions were indexed as expected.
	indexedImportTx, status, height, err := s.getAtomicTx(importTx.ID())
	require.NoError(err)
	require.Equal(atomic.Accepted, status)
	require.Equal(common.PointerTo[json.Uint64](1), height, "expected height of indexed import tx to be 1")
	require.Equal(indexedImportTx.ID(), importTx.ID(), "expected ID of indexed import tx to match original txID")

	indexedExportTx, status, height, err := s.getAtomicTx(exportTx.ID())
	require.NoError(err)
	require.Equal(atomic.Accepted, status)
	require.Equal(common.PointerTo[json.Uint64](2), height, "expected height of indexed export tx to be 2")
	require.Equal(indexedExportTx.ID(), exportTx.ID(), "expected ID of indexed import tx to match original txID")
}

func testConflictingImportTxs(t *testing.T, fork upgradetest.Fork, scheme string) {
	require := require.New(t)
	importAmount := uint64(10000000)
	vm := newAtomicTestVM()
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	})
	require.NoError(addUTXOs(tvm.AtomicMemory, vm.Ctx, map[ids.ShortID]uint64{
		vmtest.TestShortIDAddrs[0]: importAmount,
		vmtest.TestShortIDAddrs[1]: importAmount,
		vmtest.TestShortIDAddrs[2]: importAmount,
	}))

	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	importTxs := make([]*atomic.Tx, 0, 3)
	conflictTxs := make([]*atomic.Tx, 0, 3)
	for i, key := range vmtest.TestKeys {
		importTx, err := vm.newImportTx(vm.Ctx.XChainID, vmtest.TestEthAddrs[i], vmtest.InitialBaseFee, []*secp256k1.PrivateKey{key})
		require.NoError(err)
		importTxs = append(importTxs, importTx)

		conflictAddr := vmtest.TestEthAddrs[(i+1)%len(vmtest.TestEthAddrs)]
		conflictTx, err := vm.newImportTx(vm.Ctx.XChainID, conflictAddr, vmtest.InitialBaseFee, []*secp256k1.PrivateKey{key})
		require.NoError(err)
		conflictTxs = append(conflictTxs, conflictTx)
	}

	expectedParentBlkID, err := vm.LastAccepted(t.Context())
	require.NoError(err)
	for _, tx := range importTxs[:2] {
		require.NoError(vm.mempool.AddLocalTx(tx))

		msg, err := vm.WaitForEvent(t.Context())
		require.NoError(err)
		require.Equal(commonEng.PendingTxs, msg)

		vm.clock.Set(vm.clock.Time().Add(2 * time.Second))
		blk, err := vm.BuildBlock(t.Context())
		require.NoError(err)
		require.NoError(blk.Verify(t.Context()))

		require.Equal(expectedParentBlkID, blk.Parent())

		expectedParentBlkID = blk.ID()
		require.NoError(vm.SetPreference(t.Context(), blk.ID()))
	}

	// Check that for each conflict tx (whose conflict is in the chain ancestry)
	// the VM returns an error when it attempts to issue the conflict into the mempool
	// and when it attempts to build a block with the conflict force added to the mempool.
	for i, tx := range conflictTxs[:2] {
		err = vm.mempool.AddLocalTx(tx)
		require.ErrorIsf(err, ErrConflictingAtomicInputs, "tx index %d", i)
		// Force issue transaction directly to the mempool
		require.NoErrorf(vm.mempool.ForceAddTx(tx), "force issue failed for tx index %d", i)
		msg, err := vm.WaitForEvent(t.Context())
		require.NoErrorf(err, "wait for event failed for tx index %d", i)
		require.Equal(commonEng.PendingTxs, msg)

		vm.clock.Set(vm.clock.Time().Add(2 * time.Second))
		_, err = vm.BuildBlock(t.Context())
		// The new block is verified in BuildBlock, so
		// BuildBlock should fail due to an attempt to
		// double spend an atomic UTXO.
		require.ErrorIsf(err, ErrEmptyBlock, "tx index %d", i)
	}

	// Generate one more valid block so that we can copy the header to create an invalid block
	// with modified extra data. This new block will be invalid for more than one reason (invalid merkle root)
	// so we check to make sure that the expected error is returned from block verification.
	require.NoError(vm.mempool.AddLocalTx(importTxs[2]))
	msg, err := vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)
	vm.clock.Set(vm.clock.Time().Add(2 * time.Second))

	validBlock, err := vm.BuildBlock(t.Context())
	require.NoError(err)
	require.NoError(validBlock.Verify(t.Context()))

	validEthBlock := validBlock.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()

	rules := vm.CurrentRules()
	var extraData []byte
	switch {
	case rules.IsApricotPhase5:
		extraData, err = atomic.Codec.Marshal(atomic.CodecVersion, []*atomic.Tx{conflictTxs[1]})
	default:
		extraData, err = atomic.Codec.Marshal(atomic.CodecVersion, conflictTxs[1])
	}
	require.NoError(err)
	conflictingAtomicTxBlock := customtypes.NewBlockWithExtData(
		types.CopyHeader(validEthBlock.Header()),
		nil,
		nil,
		nil,
		new(trie.Trie),
		extraData,
		true,
	)

	blockBytes, err := rlp.EncodeToBytes(conflictingAtomicTxBlock)
	require.NoError(err)
	parsedBlock, err := vm.ParseBlock(t.Context(), blockBytes)
	require.NoError(err)
	err = parsedBlock.Verify(t.Context())
	require.ErrorIs(err, ErrConflictingAtomicInputs)

	if !rules.IsApricotPhase5 {
		return
	}

	extraData, err = atomic.Codec.Marshal(atomic.CodecVersion, []*atomic.Tx{importTxs[2], conflictTxs[2]})
	require.NoError(err)
	header := types.CopyHeader(validEthBlock.Header())
	headerExtra := customtypes.GetHeaderExtra(header)
	headerExtra.ExtDataGasUsed.Mul(common.Big2, headerExtra.ExtDataGasUsed)

	internalConflictBlock := customtypes.NewBlockWithExtData(
		header,
		nil,
		nil,
		nil,
		new(trie.Trie),
		extraData,
		true,
	)

	blockBytes, err = rlp.EncodeToBytes(internalConflictBlock)
	require.NoError(err)
	parsedBlock, err = vm.ParseBlock(t.Context(), blockBytes)
	require.NoError(err)
	err = parsedBlock.Verify(t.Context())
	require.ErrorIs(err, ErrConflictingAtomicInputs)
}

func TestReissueAtomicTxHigherGasPrice(t *testing.T) {
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testReissueAtomicTxHigherGasPrice(t, scheme)
		})
	}
}

func testReissueAtomicTxHigherGasPrice(t *testing.T, scheme string) {
	kc := secp256k1fx.NewKeychain(vmtest.TestKeys...)

	tests := map[string]func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) (issued []*atomic.Tx, discarded []*atomic.Tx){
		"single UTXO override": func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) (issued []*atomic.Tx, evicted []*atomic.Tx) {
			utxo, err := addUTXO(sharedMemory, vm.Ctx, ids.GenerateTestID(), 0, vm.Ctx.AVAXAssetID, units.Avax, vmtest.TestShortIDAddrs[0])
			require.NoError(t, err)
			tx1, err := atomic.NewImportTx(vm.Ctx, vm.CurrentRules(), vm.clock.Unix(), vm.Ctx.XChainID, vmtest.TestEthAddrs[0], vmtest.InitialBaseFee, kc, []*avax.UTXO{utxo})
			require.NoError(t, err)
			tx2, err := atomic.NewImportTx(vm.Ctx, vm.CurrentRules(), vm.clock.Unix(), vm.Ctx.XChainID, vmtest.TestEthAddrs[0], new(big.Int).Mul(common.Big2, vmtest.InitialBaseFee), kc, []*avax.UTXO{utxo})
			require.NoError(t, err)
			require.NoError(t, vm.mempool.AddLocalTx(tx1))
			require.NoError(t, vm.mempool.AddLocalTx(tx2))

			return []*atomic.Tx{tx2}, []*atomic.Tx{tx1}
		},
		"one of two UTXOs overrides": func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) (issued []*atomic.Tx, evicted []*atomic.Tx) {
			utxo1, err := addUTXO(sharedMemory, vm.Ctx, ids.GenerateTestID(), 0, vm.Ctx.AVAXAssetID, units.Avax, vmtest.TestShortIDAddrs[0])
			require.NoError(t, err)
			utxo2, err := addUTXO(sharedMemory, vm.Ctx, ids.GenerateTestID(), 0, vm.Ctx.AVAXAssetID, units.Avax, vmtest.TestShortIDAddrs[0])
			require.NoError(t, err)
			tx1, err := atomic.NewImportTx(vm.Ctx, vm.CurrentRules(), vm.clock.Unix(), vm.Ctx.XChainID, vmtest.TestEthAddrs[0], vmtest.InitialBaseFee, kc, []*avax.UTXO{utxo1, utxo2})
			require.NoError(t, err)
			tx2, err := atomic.NewImportTx(vm.Ctx, vm.CurrentRules(), vm.clock.Unix(), vm.Ctx.XChainID, vmtest.TestEthAddrs[0], new(big.Int).Mul(common.Big2, vmtest.InitialBaseFee), kc, []*avax.UTXO{utxo1})
			require.NoError(t, err)
			require.NoError(t, vm.mempool.AddLocalTx(tx1))
			require.NoError(t, vm.mempool.AddLocalTx(tx2))

			return []*atomic.Tx{tx2}, []*atomic.Tx{tx1}
		},
		"hola": func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) (issued []*atomic.Tx, evicted []*atomic.Tx) {
			utxo1, err := addUTXO(sharedMemory, vm.Ctx, ids.GenerateTestID(), 0, vm.Ctx.AVAXAssetID, units.Avax, vmtest.TestShortIDAddrs[0])
			require.NoError(t, err)
			utxo2, err := addUTXO(sharedMemory, vm.Ctx, ids.GenerateTestID(), 0, vm.Ctx.AVAXAssetID, units.Avax, vmtest.TestShortIDAddrs[0])
			require.NoError(t, err)
			importTx1, err := atomic.NewImportTx(vm.Ctx, vm.CurrentRules(), vm.clock.Unix(), vm.Ctx.XChainID, vmtest.TestEthAddrs[0], vmtest.InitialBaseFee, kc, []*avax.UTXO{utxo1})
			require.NoError(t, err)
			importTx2, err := atomic.NewImportTx(vm.Ctx, vm.CurrentRules(), vm.clock.Unix(), vm.Ctx.XChainID, vmtest.TestEthAddrs[0], new(big.Int).Mul(big.NewInt(3), vmtest.InitialBaseFee), kc, []*avax.UTXO{utxo2})
			require.NoError(t, err)
			reissuanceTx1, err := atomic.NewImportTx(vm.Ctx, vm.CurrentRules(), vm.clock.Unix(), vm.Ctx.XChainID, vmtest.TestEthAddrs[0], new(big.Int).Mul(big.NewInt(2), vmtest.InitialBaseFee), kc, []*avax.UTXO{utxo1, utxo2})
			require.NoError(t, err)
			require.NoError(t, vm.mempool.AddLocalTx(importTx1))
			require.NoError(t, vm.mempool.AddLocalTx(importTx2))

			err = vm.mempool.AddLocalTx(reissuanceTx1)
			require.ErrorIs(t, err, txpool.ErrConflict)

			require.True(t, vm.mempool.Has(importTx1.ID()))
			require.True(t, vm.mempool.Has(importTx2.ID()))
			require.False(t, vm.mempool.Has(reissuanceTx1.ID()))

			reissuanceTx2, err := atomic.NewImportTx(vm.Ctx, vm.CurrentRules(), vm.clock.Unix(), vm.Ctx.XChainID, vmtest.TestEthAddrs[0], new(big.Int).Mul(big.NewInt(4), vmtest.InitialBaseFee), kc, []*avax.UTXO{utxo1, utxo2})
			require.NoError(t, err)
			require.NoError(t, vm.mempool.AddLocalTx(reissuanceTx2))

			return []*atomic.Tx{reissuanceTx2}, []*atomic.Tx{importTx1, importTx2}
		},
	}
	for name, issueTxs := range tests {
		t.Run(name, func(t *testing.T) {
			fork := upgradetest.ApricotPhase5
			vm := newAtomicTestVM()
			tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
				Fork:       &fork,
				Scheme:     scheme,
				ConfigJSON: `{"pruning-enabled":true}`,
			})
			issuedTxs, evictedTxs := issueTxs(t, vm, tvm.AtomicMemory)

			for i, tx := range issuedTxs {
				_, issued := vm.mempool.GetPendingTx(tx.ID())
				require.True(t, issued, "expected issued tx at index %d to be issued", i)
			}

			for i, tx := range evictedTxs {
				_, discarded, _ := vm.mempool.GetTx(tx.ID())
				require.True(t, discarded, "expected discarded tx at index %d to be discarded", i)
			}
		})
	}
}

func TestConflictingImportTxsAcrossBlocks(t *testing.T) {
	for _, fork := range []upgradetest.Fork{
		upgradetest.ApricotPhase1,
		upgradetest.ApricotPhase2,
		upgradetest.ApricotPhase3,
		upgradetest.ApricotPhase4,
		upgradetest.ApricotPhase5,
	} {
		t.Run(fork.String(), func(t *testing.T) {
			for _, scheme := range vmtest.Schemes {
				t.Run(scheme, func(t *testing.T) {
					testConflictingImportTxs(t, fork, scheme)
				})
			}
		})
	}
}

func TestConflictingTransitiveAncestryWithGap(t *testing.T) {
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testConflictingTransitiveAncestryWithGap(t, scheme)
		})
	}
}

func testConflictingTransitiveAncestryWithGap(t *testing.T, scheme string) {
	require := require.New(t)
	key := utilstest.NewKey(t)

	key0 := vmtest.TestKeys[0]
	addr0 := key0.Address()

	key1 := vmtest.TestKeys[1]
	addr1 := key1.Address()

	importAmount := uint64(1000000000)
	fork := upgradetest.NoUpgrades
	vm := newAtomicTestVM()
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	})
	require.NoError(addUTXOs(tvm.AtomicMemory, vm.Ctx, map[ids.ShortID]uint64{
		addr0: importAmount,
		addr1: importAmount,
	}))

	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.Ethereum().TxPool().SubscribeNewReorgEvent(newTxPoolHeadChan)

	importTx0A, err := vm.newImportTx(vm.Ctx.XChainID, key.Address, vmtest.InitialBaseFee, []*secp256k1.PrivateKey{key0})
	require.NoError(err)
	// Create a conflicting transaction
	importTx0B, err := vm.newImportTx(vm.Ctx.XChainID, vmtest.TestEthAddrs[2], vmtest.InitialBaseFee, []*secp256k1.PrivateKey{key0})
	require.NoError(err)
	require.NoError(vm.mempool.AddLocalTx(importTx0A))

	msg, err := vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)

	blk0, err := vm.BuildBlock(t.Context())
	require.NoError(err)

	require.NoError(blk0.Verify(t.Context()))
	require.NoError(vm.SetPreference(t.Context(), blk0.ID()))

	newHead := <-newTxPoolHeadChan
	require.Equal(common.Hash(blk0.ID()), newHead.Head.Hash())

	tx := types.NewTransaction(0, key.Address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.Ethereum().BlockChain().Config().ChainID), key.PrivateKey)
	require.NoError(err)

	// Add the remote transactions, build the block, and set VM1's preference for block A
	_, err = vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm)
	require.NoError(err)

	importTx1, err := vm.newImportTx(vm.Ctx.XChainID, key.Address, vmtest.InitialBaseFee, []*secp256k1.PrivateKey{key1})
	require.NoError(err)
	require.NoError(vm.mempool.AddLocalTx(importTx1))

	msg, err = vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)

	blk2, err := vm.BuildBlock(t.Context())
	require.NoError(err)
	require.NoError(blk2.Verify(t.Context()))
	require.NoError(vm.SetPreference(t.Context(), blk2.ID()))

	err = vm.mempool.AddLocalTx(importTx0B)
	require.ErrorIs(err, ErrConflictingAtomicInputs)

	// Force issue transaction directly into the mempool
	require.NoError(vm.mempool.ForceAddTx(importTx0B))
	msg, err = vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)

	_, err = vm.BuildBlock(t.Context())
	require.ErrorIs(err, ErrEmptyBlock)
}

func TestBonusBlocksTxs(t *testing.T) {
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testBonusBlocksTxs(t, scheme)
		})
	}
}

func testBonusBlocksTxs(t *testing.T, scheme string) {
	require := require.New(t)
	fork := upgradetest.NoUpgrades
	vm := newAtomicTestVM()
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	})
	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	importAmount := uint64(10000000)
	utxoID := avax.UTXOID{TxID: ids.GenerateTestID()}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: vm.Ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: importAmount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{vmtest.TestKeys[0].Address()},
			},
		},
	}
	utxoBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, utxo)
	require.NoError(err)
	xChainSharedMemory := tvm.AtomicMemory.NewSharedMemory(vm.Ctx.XChainID)
	inputID := utxo.InputID()
	require.NoError(xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{vm.Ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			vmtest.TestKeys[0].Address().Bytes(),
		},
	}}}}))

	importTx, err := vm.newImportTx(vm.Ctx.XChainID, vmtest.TestEthAddrs[0], vmtest.InitialBaseFee, []*secp256k1.PrivateKey{vmtest.TestKeys[0]})
	require.NoError(err)
	require.NoError(vm.mempool.AddLocalTx(importTx))

	msg, err := vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)

	blk, err := vm.BuildBlock(t.Context())
	require.NoError(err)
	// Make [blk] a bonus block.
	vm.AtomicBackend.AddBonusBlock(blk.Height(), blk.ID())

	// Remove the UTXOs from shared memory, so that non-bonus blocks will fail verification
	require.NoError(vm.Ctx.SharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{vm.Ctx.XChainID: {RemoveRequests: [][]byte{inputID[:]}}}))

	require.NoError(blk.Verify(t.Context()))

	require.NoError(vm.SetPreference(t.Context(), blk.ID()))

	require.NoError(blk.Accept(t.Context()))

	lastAcceptedID, err := vm.LastAccepted(t.Context())
	require.NoError(err)
	require.Equal(blk.ID(), lastAcceptedID)
}

// Builds [blkA] with a virtuous import transaction and [blkB] with a separate import transaction
// that does not conflict. Accepts [blkB] and rejects [blkA], then requires that the virtuous atomic
// transaction in [blkA] is correctly re-issued into the atomic transaction mempool.
func TestReissueAtomicTx(t *testing.T) {
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testReissueAtomicTx(t, scheme)
		})
	}
}

func testReissueAtomicTx(t *testing.T, scheme string) {
	require := require.New(t)
	fork := upgradetest.ApricotPhase1
	vm := newAtomicTestVM()
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	})
	require.NoError(addUTXOs(tvm.AtomicMemory, vm.Ctx, map[ids.ShortID]uint64{
		vmtest.TestShortIDAddrs[0]: 10000000,
		vmtest.TestShortIDAddrs[1]: 10000000,
	}))

	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	genesisBlkID, err := vm.LastAccepted(t.Context())
	require.NoError(err)
	importTx, err := vm.newImportTx(vm.Ctx.XChainID, vmtest.TestEthAddrs[0], vmtest.InitialBaseFee, []*secp256k1.PrivateKey{vmtest.TestKeys[0]})
	require.NoError(err)
	require.NoError(vm.mempool.AddLocalTx(importTx))

	msg, err := vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)

	blkA, err := vm.BuildBlock(t.Context())
	require.NoError(err)
	require.NoError(blkA.Verify(t.Context()))

	require.NoError(vm.SetPreference(t.Context(), blkA.ID()))

	// SetPreference to parent before rejecting (will rollback state to genesis
	// so that atomic transaction can be reissued, otherwise current block will
	// conflict with UTXO to be reissued)
	require.NoError(vm.SetPreference(t.Context(), genesisBlkID))

	// Rejecting [blkA] should cause [importTx] to be re-issued into the mempool.
	require.NoError(blkA.Reject(t.Context()))
	// Sleep for a minimum of two seconds to ensure that [blkB] will have a different timestamp
	// than [blkA] so that the block will be unique. This is necessary since we have marked [blkA]
	// as Rejected.
	time.Sleep(2 * time.Second)
	msg, err = vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)
	blkB, err := vm.BuildBlock(t.Context())
	require.NoError(err)
	require.Equal(blkA.Height(), blkB.Height())

	require.NoError(blkB.Verify(t.Context()))

	require.NoError(vm.SetPreference(t.Context(), blkB.ID()))

	require.NoError(blkB.Accept(t.Context()))

	lastAcceptedID, err := vm.LastAccepted(t.Context())
	require.NoError(err)
	require.Equal(blkB.ID(), lastAcceptedID)

	// Check that [importTx] has been indexed correctly after [blkB] is accepted.
	_, height, err := vm.AtomicTxRepository.GetByTxID(importTx.ID())
	require.NoError(err)
	require.Equal(blkB.Height(), height)
}

func TestAtomicTxFailsEVMStateTransferBuildBlock(t *testing.T) {
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testAtomicTxFailsEVMStateTransferBuildBlock(t, scheme)
		})
	}
}

func testAtomicTxFailsEVMStateTransferBuildBlock(t *testing.T, scheme string) {
	require := require.New(t)
	fork := upgradetest.ApricotPhase1
	vm := newAtomicTestVM()
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	})
	require.NoError(addUTXOs(tvm.AtomicMemory, vm.Ctx, map[ids.ShortID]uint64{
		vmtest.TestShortIDAddrs[0]: 10000000,
		vmtest.TestShortIDAddrs[1]: 10000000,
	}))

	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	exportTxs := createExportTxOptions(t, vm, tvm.AtomicMemory)
	exportTx1, exportTx2 := exportTxs[0], exportTxs[1]

	require.NoError(vm.mempool.AddLocalTx(exportTx1))
	msg, err := vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)
	exportBlk1, err := vm.BuildBlock(t.Context())
	require.NoError(err)
	require.NoError(exportBlk1.Verify(t.Context()))

	require.NoError(vm.SetPreference(t.Context(), exportBlk1.ID()))

	err = vm.mempool.AddLocalTx(exportTx2)
	require.ErrorIs(err, atomic.ErrInvalidNonce)

	err = vm.mempool.AddRemoteTx(exportTx2)
	require.ErrorIs(err, atomic.ErrInvalidNonce)

	// Manually add transaction to mempool to bypass validation
	require.NoError(vm.mempool.ForceAddTx(exportTx2))
	msg, err = vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)

	_, err = vm.BuildBlock(t.Context())
	require.ErrorIs(err, ErrEmptyBlock)
}

// This is a regression test to ensure that if two consecutive atomic transactions fail verification
// in onFinalizeAndAssemble it will not cause a panic due to calling RevertToSnapshot(revID) on the
// same revision ID twice.
func TestConsecutiveAtomicTransactionsRevertSnapshot(t *testing.T) {
	require := require.New(t)
	fork := upgradetest.ApricotPhase1
	vm := newAtomicTestVM()
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork: &fork,
	})
	require.NoError(addUTXOs(tvm.AtomicMemory, vm.Ctx, map[ids.ShortID]uint64{
		vmtest.TestShortIDAddrs[0]: 10000000,
		vmtest.TestShortIDAddrs[1]: 10000000,
	}))

	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.Ethereum().TxPool().SubscribeNewReorgEvent(newTxPoolHeadChan)

	// Create three conflicting import transactions
	importTxs := createImportTxOptions(t, vm, tvm.AtomicMemory)

	// Issue the first import transaction, build, and accept the block.
	require.NoError(vm.mempool.AddLocalTx(importTxs[0]))

	msg, err := vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)

	blk, err := vm.BuildBlock(t.Context())
	require.NoError(err)
	require.NoError(blk.Verify(t.Context()))
	require.NoError(vm.SetPreference(t.Context(), blk.ID()))
	require.NoError(blk.Accept(t.Context()))

	newHead := <-newTxPoolHeadChan
	require.Equal(common.Hash(blk.ID()), newHead.Head.Hash())

	// Add the two conflicting transactions directly to the mempool, so that two consecutive transactions
	// will fail verification when build block is called.
	require.NoError(vm.mempool.ForceAddTx(importTxs[1]))
	require.NoError(vm.mempool.ForceAddTx(importTxs[2]))
	require.Equal(2, vm.mempool.Txs.PendingLen())

	_, err = vm.BuildBlock(t.Context())
	require.ErrorIs(err, ErrEmptyBlock)
}

func TestAtomicTxBuildBlockDropsConflicts(t *testing.T) {
	require := require.New(t)
	importAmount := uint64(10000000)
	fork := upgradetest.ApricotPhase5
	vm := newAtomicTestVM()
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork: &fork,
	})
	require.NoError(addUTXOs(tvm.AtomicMemory, vm.Ctx, map[ids.ShortID]uint64{
		vmtest.TestShortIDAddrs[0]: importAmount,
		vmtest.TestShortIDAddrs[1]: importAmount,
		vmtest.TestShortIDAddrs[2]: importAmount,
	}))
	conflictKey := utilstest.NewKey(t)

	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	// Create a conflict set for each pair of transactions
	conflictSets := make([]set.Set[ids.ID], len(vmtest.TestKeys))
	for index, key := range vmtest.TestKeys {
		importTx, err := vm.newImportTx(vm.Ctx.XChainID, vmtest.TestEthAddrs[index], vmtest.InitialBaseFee, []*secp256k1.PrivateKey{key})
		require.NoError(err)
		require.NoError(vm.mempool.AddLocalTx(importTx))
		conflictSets[index].Add(importTx.ID())
		conflictTx, err := vm.newImportTx(vm.Ctx.XChainID, conflictKey.Address, vmtest.InitialBaseFee, []*secp256k1.PrivateKey{key})
		require.NoError(err)
		err = vm.mempool.AddLocalTx(conflictTx)
		require.ErrorIs(err, txpool.ErrConflict)
		// force add the tx
		require.NoError(vm.mempool.ForceAddTx(conflictTx))
		conflictSets[index].Add(conflictTx.ID())
	}
	msg, err := vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)
	// Note: this only checks the path through OnFinalizeAndAssemble, we should make sure to add a test
	// that verifies blocks received from the network will also fail verification
	blk, err := vm.BuildBlock(t.Context())
	require.NoError(err)
	wrappedBlk, ok := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock)
	require.True(ok, "expected block to be a ExtendedBlock")
	blockExtension, ok := wrappedBlk.GetBlockExtension().(*blockExtension)
	require.True(ok, "expected block to be a blockExtension")
	atomicTxs := blockExtension.atomicTxs
	require.Len(atomicTxs, len(vmtest.TestKeys), "Conflict transactions should be out of the batch")
	atomicTxIDs := set.Set[ids.ID]{}
	for _, tx := range atomicTxs {
		atomicTxIDs.Add(tx.ID())
	}

	// Check that removing the txIDs actually included in the block from each conflict set
	// leaves one item remaining for each conflict set ie. only one tx from each conflict set
	// has been included in the block.
	for _, conflictSet := range conflictSets {
		conflictSet.Difference(atomicTxIDs)
		require.Equal(1, conflictSet.Len())
	}

	require.NoError(blk.Verify(t.Context()))
	require.NoError(blk.Accept(t.Context()))
}

func TestBuildBlockDoesNotExceedAtomicGasLimit(t *testing.T) {
	require := require.New(t)
	importAmount := uint64(10000000)
	fork := upgradetest.ApricotPhase5
	vm := newAtomicTestVM()
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork: &fork,
	})

	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	kc := secp256k1fx.NewKeychain(vmtest.TestKeys[0])
	txID, err := ids.ToID(hashing.ComputeHash256(vmtest.TestShortIDAddrs[0][:]))
	require.NoError(err)

	mempoolTxs := 200
	for i := 0; i < mempoolTxs; i++ {
		utxo, err := addUTXO(tvm.AtomicMemory, vm.Ctx, txID, uint32(i), vm.Ctx.AVAXAssetID, importAmount, vmtest.TestShortIDAddrs[0])
		require.NoError(err)

		importTx, err := atomic.NewImportTx(vm.Ctx, vm.CurrentRules(), vm.clock.Unix(), vm.Ctx.XChainID, vmtest.TestEthAddrs[0], vmtest.InitialBaseFee, kc, []*avax.UTXO{utxo})
		require.NoError(err)
		require.NoError(vm.mempool.AddLocalTx(importTx))
	}

	msg, err := vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)
	blk, err := vm.BuildBlock(t.Context())
	require.NoError(err)
	wrappedBlk, ok := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock)
	require.True(ok, "expected block to be a ExtendedBlock")
	blockExtension, ok := wrappedBlk.GetBlockExtension().(*blockExtension)
	require.True(ok, "expected block to be a blockExtension")
	// Need to ensure that not all of the transactions in the mempool are included in the block.
	// This ensures that we hit the atomic gas limit while building the block before we hit the
	// upper limit on the size of the codec for marshalling the atomic transactions.
	atomicTxs := blockExtension.atomicTxs
	require.Lessf(len(atomicTxs), mempoolTxs, "Expected number of atomic transactions included in the block (%d) to be less than the number of transactions added to the mempool (%d)", len(atomicTxs), mempoolTxs)
}

func TestExtraStateChangeAtomicGasLimitExceeded(t *testing.T) {
	require := require.New(t)
	importAmount := uint64(10000000)
	// We create two VMs one in ApriotPhase4 and one in ApricotPhase5, so that we can construct a block
	// containing a large enough atomic transaction that it will exceed the atomic gas limit in
	// ApricotPhase5.
	fork1 := upgradetest.ApricotPhase4
	vm1 := newAtomicTestVM()
	tvm1 := vmtest.SetupTestVM(t, vm1, vmtest.TestVMConfig{
		Fork: &fork1,
	})
	fork2 := upgradetest.ApricotPhase5
	vm2 := newAtomicTestVM()
	tvm2 := vmtest.SetupTestVM(t, vm2, vmtest.TestVMConfig{
		Fork: &fork2,
	})
	defer func() {
		require.NoError(vm1.Shutdown(t.Context()))
		require.NoError(vm2.Shutdown(t.Context()))
	}()

	txID, err := ids.ToID(hashing.ComputeHash256(vmtest.TestShortIDAddrs[0][:]))
	require.NoError(err)

	// Add enough UTXOs, such that the created import transaction will attempt to consume more gas than allowed
	// in ApricotPhase5.
	for i := 0; i < 100; i++ {
		_, err := addUTXO(tvm1.AtomicMemory, vm1.Ctx, txID, uint32(i), vm1.Ctx.AVAXAssetID, importAmount, vmtest.TestShortIDAddrs[0])
		require.NoError(err)

		_, err = addUTXO(tvm2.AtomicMemory, vm2.Ctx, txID, uint32(i), vm2.Ctx.AVAXAssetID, importAmount, vmtest.TestShortIDAddrs[0])
		require.NoError(err)
	}

	// Double the initial base fee used when estimating the cost of this transaction to ensure that when it is
	// used in ApricotPhase5 it still pays a sufficient fee with the fixed fee per atomic transaction.
	importTx, err := vm1.newImportTx(vm1.Ctx.XChainID, vmtest.TestEthAddrs[0], new(big.Int).Mul(common.Big2, vmtest.InitialBaseFee), []*secp256k1.PrivateKey{vmtest.TestKeys[0]})
	require.NoError(err)
	require.NoError(vm1.mempool.ForceAddTx(importTx))

	msg, err := vm1.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)
	blk1, err := vm1.BuildBlock(t.Context())
	require.NoError(err)
	require.NoError(blk1.Verify(t.Context()))

	wrappedBlk, ok := blk1.(*chain.BlockWrapper).Block.(extension.ExtendedBlock)
	require.True(ok, "expected block to be a ExtendedBlock")
	validEthBlock := wrappedBlk.GetEthBlock()
	extraData, err := atomic.Codec.Marshal(atomic.CodecVersion, []*atomic.Tx{importTx})
	require.NoError(err)
	// Construct the new block with the extra data in the new format (slice of atomic transactions).
	ethBlk2 := customtypes.NewBlockWithExtData(
		types.CopyHeader(validEthBlock.Header()),
		nil,
		nil,
		nil,
		new(trie.Trie),
		extraData,
		true,
	)

	state, err := vm2.Ethereum().BlockChain().State()
	require.NoError(err)

	// Hack: test [onExtraStateChange] directly to ensure it catches the atomic gas limit error correctly.
	_, _, err = vm2.onExtraStateChange(ethBlk2, nil, state)
	require.ErrorIs(err, errAtomicGasExceedsLimit)
}

// Regression test to ensure that a VM that is not able to parse a block that
// contains no transactions.
func TestEmptyBlock(t *testing.T) {
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testEmptyBlock(t, scheme)
		})
	}
}

func testEmptyBlock(t *testing.T, scheme string) {
	require := require.New(t)
	importAmount := uint64(1000000000)
	fork := upgradetest.NoUpgrades
	vm := newAtomicTestVM()
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	})
	require.NoError(addUTXOs(tvm.AtomicMemory, vm.Ctx, map[ids.ShortID]uint64{
		vmtest.TestShortIDAddrs[0]: importAmount,
	}))

	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	importTx, err := vm.newImportTx(vm.Ctx.XChainID, vmtest.TestEthAddrs[0], vmtest.InitialBaseFee, []*secp256k1.PrivateKey{vmtest.TestKeys[0]})
	require.NoError(err)
	require.NoError(vm.mempool.AddLocalTx(importTx))

	msg, err := vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)

	blk, err := vm.BuildBlock(t.Context())
	require.NoError(err)

	// Create empty block from blkA
	wrappedBlk, ok := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock)
	require.True(ok, "expected block to be a ExtendedBlock")
	ethBlock := wrappedBlk.GetEthBlock()

	emptyEthBlock := customtypes.NewBlockWithExtData(
		types.CopyHeader(ethBlock.Header()),
		nil,
		nil,
		nil,
		new(trie.Trie),
		nil,
		false,
	)

	require.Empty(customtypes.BlockExtData(emptyEthBlock))
	require.Equal(common.Hash{}, customtypes.GetHeaderExtra(emptyEthBlock.Header()).ExtDataHash)

	emptyBlockBytes, err := rlp.EncodeToBytes(emptyEthBlock)
	require.NoError(err)

	_, err = vm.ParseBlock(t.Context(), emptyBlockBytes)
	require.ErrorIs(err, ErrEmptyBlock)
}

// Regression test to ensure we can build blocks if we are starting with the
// Apricot Phase 5 ruleset in genesis.
func TestBuildApricotPhase5Block(t *testing.T) {
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testBuildApricotPhase5Block(t, scheme)
		})
	}
}

func testBuildApricotPhase5Block(t *testing.T, scheme string) {
	require := require.New(t)
	fork := upgradetest.ApricotPhase5
	vm := newAtomicTestVM()
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	})

	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.Ethereum().TxPool().SubscribeNewReorgEvent(newTxPoolHeadChan)

	key := vmtest.TestKeys[0].ToECDSA()
	address := vmtest.TestEthAddrs[0]

	importAmount := uint64(1000000000)
	utxoID := avax.UTXOID{TxID: ids.GenerateTestID()}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: vm.Ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: importAmount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{vmtest.TestKeys[0].Address()},
			},
		},
	}
	utxoBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, utxo)
	require.NoError(err)
	xChainSharedMemory := tvm.AtomicMemory.NewSharedMemory(vm.Ctx.XChainID)
	inputID := utxo.InputID()
	require.NoError(xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{vm.Ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			vmtest.TestKeys[0].Address().Bytes(),
		},
	}}}}))

	importTx, err := vm.newImportTx(vm.Ctx.XChainID, address, vmtest.InitialBaseFee, []*secp256k1.PrivateKey{vmtest.TestKeys[0]})
	require.NoError(err)
	require.NoError(vm.mempool.AddLocalTx(importTx))

	msg, err := vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)

	blk, err := vm.BuildBlock(t.Context())
	require.NoError(err)
	require.NoError(blk.Verify(t.Context()))

	require.NoError(vm.SetPreference(t.Context(), blk.ID()))

	require.NoError(blk.Accept(t.Context()))

	wrappedBlk, ok := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock)
	require.True(ok, "expected block to be a ExtendedBlock")
	ethBlk := wrappedBlk.GetEthBlock()
	eBlockGasCost := customtypes.BlockGasCost(ethBlk)
	require.NotNil(eBlockGasCost)
	require.Zerof(eBlockGasCost.Cmp(common.Big0), "expected blockGasCost to be greater than 0 but got %d", eBlockGasCost)
	eExtDataGasUsed := customtypes.BlockExtDataGasUsed(ethBlk)
	require.NotNil(eExtDataGasUsed)
	require.Zero(eExtDataGasUsed.Cmp(big.NewInt(11230)), "expected extDataGasUsed to be 11230 but got %d", eExtDataGasUsed)

	newHead := <-newTxPoolHeadChan
	require.Equal(common.Hash(blk.ID()), newHead.Head.Hash())

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice*3), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.Ethereum().BlockChain().Config().ChainID), key)
		require.NoError(err)
		txs[i] = signedTx
	}
	errs := vm.Ethereum().TxPool().Add(txs, false, false)
	for _, err := range errs {
		require.NoError(err)
	}

	msg, err = vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)

	blk, err = vm.BuildBlock(t.Context())
	require.NoError(err)
	require.NoError(blk.Verify(t.Context()))

	require.NoError(blk.Accept(t.Context()))

	wrappedBlk, ok = blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock)
	require.True(ok, "expected block to be a ExtendedBlock")
	ethBlk = wrappedBlk.GetEthBlock()
	blockGasCost := customtypes.BlockGasCost(ethBlk)
	require.NotNil(blockGasCost)
	require.LessOrEqualf(0, blockGasCost.Cmp(big.NewInt(100)), "expected blockGasCost to be at least 100 but got %d", blockGasCost)
	eDataGasUsed := customtypes.BlockExtDataGasUsed(ethBlk)
	require.NotNil(eDataGasUsed)
	require.Zero(eDataGasUsed.Cmp(common.Big0), "expected extDataGasUsed to be 0 but got %d", eDataGasUsed)

	lastAcceptedID, err := vm.LastAccepted(t.Context())
	require.NoError(err)
	require.Equal(blk.ID(), lastAcceptedID)

	// Confirm all txs are present
	ethBlkTxs := vm.Ethereum().BlockChain().GetBlockByNumber(2).Transactions()
	for i, tx := range txs {
		require.Greater(len(ethBlkTxs), i, "missing transactions expected: %d but found: %d", len(txs), len(ethBlkTxs))
		require.Equal(ethBlkTxs[i].Hash(), tx.Hash(), "expected tx at index %d to have hash: %x but has: %x", i, txs[i].Hash(), tx.Hash())
	}
}

// Regression test to ensure we can build blocks if we are starting with the
// Apricot Phase 4 ruleset in genesis.
func TestBuildApricotPhase4Block(t *testing.T) {
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testBuildApricotPhase4Block(t, scheme)
		})
	}
}

func testBuildApricotPhase4Block(t *testing.T, scheme string) {
	require := require.New(t)
	fork := upgradetest.ApricotPhase4
	vm := newAtomicTestVM()
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	})

	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.Ethereum().TxPool().SubscribeNewReorgEvent(newTxPoolHeadChan)

	key := vmtest.TestKeys[0].ToECDSA()
	address := vmtest.TestEthAddrs[0]

	importAmount := uint64(1000000000)
	utxoID := avax.UTXOID{TxID: ids.GenerateTestID()}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: vm.Ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: importAmount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{vmtest.TestKeys[0].Address()},
			},
		},
	}
	utxoBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, utxo)
	require.NoError(err)
	xChainSharedMemory := tvm.AtomicMemory.NewSharedMemory(vm.Ctx.XChainID)
	inputID := utxo.InputID()
	require.NoError(xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{vm.Ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			vmtest.TestKeys[0].Address().Bytes(),
		},
	}}}}))

	importTx, err := vm.newImportTx(vm.Ctx.XChainID, address, vmtest.InitialBaseFee, []*secp256k1.PrivateKey{vmtest.TestKeys[0]})
	require.NoError(err)
	require.NoError(vm.mempool.AddLocalTx(importTx))

	msg, err := vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)

	blk, err := vm.BuildBlock(t.Context())
	require.NoError(err)
	require.NoError(blk.Verify(t.Context()))

	require.NoError(vm.SetPreference(t.Context(), blk.ID()))

	require.NoError(blk.Accept(t.Context()))

	wrappedBlk, ok := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock)
	require.True(ok, "expected block to be a ExtendedBlock")
	ethBlk := wrappedBlk.GetEthBlock()
	eBlockGasCost := customtypes.BlockGasCost(ethBlk)
	require.NotNil(eBlockGasCost)
	require.Zerof(eBlockGasCost.Cmp(common.Big0), "expected blockGasCost to be 0 but got %d", eBlockGasCost)
	eExtDataGasUsed := customtypes.BlockExtDataGasUsed(ethBlk)
	require.NotNil(eExtDataGasUsed)
	require.Zerof(eExtDataGasUsed.Cmp(big.NewInt(1230)), "expected extDataGasUsed to be 1000 but got %d", eExtDataGasUsed)

	newHead := <-newTxPoolHeadChan
	require.Equal(common.Hash(blk.ID()), newHead.Head.Hash())

	txs := make([]*types.Transaction, 10)
	chainID := vm.Ethereum().BlockChain().Config().ChainID
	for i := 0; i < 5; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), key)
		require.NoError(err)
		txs[i] = signedTx
	}
	for i := 5; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap1.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), key)
		require.NoError(err)
		txs[i] = signedTx
	}
	blk, err = vmtest.IssueTxsAndBuild(txs, vm)
	require.NoError(err)
	require.NoError(blk.Accept(t.Context()))

	wrappedBlk, ok = blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock)
	require.True(ok, "expected block to be a ExtendedBlock")
	ethBlk = wrappedBlk.GetEthBlock()
	blockGasCost := customtypes.BlockGasCost(ethBlk)
	require.NotNil(blockGasCost)
	require.GreaterOrEqualf(blockGasCost.Cmp(big.NewInt(100)), 0, "expected blockGasCost to be at least 100 but got %d", blockGasCost)
	eDataGasUsed := customtypes.BlockExtDataGasUsed(ethBlk)
	require.NotNil(eDataGasUsed)
	require.Zero(eDataGasUsed.Cmp(common.Big0), "expected extDataGasUsed to be 0 but got %d", eDataGasUsed)

	lastAcceptedID, err := vm.LastAccepted(t.Context())
	require.NoError(err)
	require.Equal(blk.ID(), lastAcceptedID)

	// Confirm all txs are present
	ethBlkTxs := vm.Ethereum().BlockChain().GetBlockByNumber(2).Transactions()
	for i, tx := range txs {
		require.Greaterf(len(ethBlkTxs), i, "missing transactions expected: %d but found: %d", len(txs), len(ethBlkTxs))
		require.Equal(ethBlkTxs[i].Hash(), tx.Hash(), "expected tx at index %d to have hash: %x but has: %x", i, txs[i].Hash(), tx.Hash())
	}
}

func TestBuildInvalidBlockHead(t *testing.T) {
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testBuildInvalidBlockHead(t, scheme)
		})
	}
}

func testBuildInvalidBlockHead(t *testing.T, scheme string) {
	require := require.New(t)
	fork := upgradetest.NoUpgrades
	vm := newAtomicTestVM()
	vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	})

	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	key0 := vmtest.TestKeys[0]
	addr0 := key0.Address()

	// Create the transaction
	utx := &atomic.UnsignedImportTx{
		NetworkID:    vm.Ctx.NetworkID,
		BlockchainID: vm.Ctx.ChainID,
		Outs: []atomic.EVMOutput{{
			Address: common.Address(addr0),
			Amount:  1 * units.Avax,
			AssetID: vm.Ctx.AVAXAssetID,
		}},
		ImportedInputs: []*avax.TransferableInput{
			{
				Asset: avax.Asset{ID: vm.Ctx.AVAXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1 * units.Avax,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{0},
					},
				},
			},
		},
		SourceChain: vm.Ctx.XChainID,
	}
	tx := &atomic.Tx{UnsignedAtomicTx: utx}
	require.NoError(tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{key0}}))

	currentBlock := vm.Ethereum().BlockChain().CurrentBlock()

	// Verify that the transaction fails verification when attempting to issue
	// it into the atomic mempool.
	err := vm.mempool.AddLocalTx(tx)
	require.ErrorIs(err, errFailedToFetchImportUTXOs)
	// Force issue the transaction directly to the mempool
	require.NoError(vm.mempool.ForceAddTx(tx))

	msg, err := vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)

	_, err = vm.BuildBlock(t.Context())
	require.ErrorIs(err, ErrEmptyBlock)

	newCurrentBlock := vm.Ethereum().BlockChain().CurrentBlock()

	require.Equal(currentBlock.Hash(), newCurrentBlock.Hash())
}

// shows that a locally generated AtomicTx can be added to mempool and then
// removed by inclusion in a block
func TestMempoolAddLocallyCreateAtomicTx(t *testing.T) {
	for _, name := range []string{"import", "export"} {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			// we use AP3 here to not trip any block fees
			fork := upgradetest.ApricotPhase3
			vm := newAtomicTestVM()
			tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
				Fork: &fork,
			})
			defer func() {
				require.NoError(vm.Shutdown(t.Context()))
			}()
			mempool := vm.mempool

			// generate a valid and conflicting tx
			var (
				tx, conflictingTx *atomic.Tx
			)
			if name == "import" {
				importTxs := createImportTxOptions(t, vm, tvm.AtomicMemory)
				tx, conflictingTx = importTxs[0], importTxs[1]
			} else {
				exportTxs := createExportTxOptions(t, vm, tvm.AtomicMemory)
				tx, conflictingTx = exportTxs[0], exportTxs[1]
			}
			txID := tx.ID()
			conflictingTxID := conflictingTx.ID()

			// add a tx to the mempool
			require.NoError(vm.mempool.AddLocalTx(tx))
			has := mempool.Has(txID)
			require.True(has, "valid tx not recorded into mempool")

			// try to add a conflicting tx
			err := vm.mempool.AddLocalTx(conflictingTx)
			require.ErrorIs(err, txpool.ErrConflict)
			has = mempool.Has(conflictingTxID)
			require.False(has, "conflicting tx in mempool")

			msg, err := vm.WaitForEvent(t.Context())
			require.NoError(err)
			require.Equal(commonEng.PendingTxs, msg)

			has = mempool.Has(txID)
			require.True(has, "valid tx not recorded into mempool")

			// Show that BuildBlock generates a block containing [txID] and that it is
			// still present in the mempool.
			blk, err := vm.BuildBlock(t.Context())
			require.NoError(err, "could not build block out of mempool")

			wrappedBlock, ok := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock)
			require.True(ok, "unknown block type")

			blockExtension, ok := wrappedBlock.GetBlockExtension().(*blockExtension)
			require.True(ok, "unknown block extension type")

			atomicTxs := blockExtension.atomicTxs
			require.Equal(txID, atomicTxs[0].ID(), "block does not include expected transaction")

			has = mempool.Has(txID)
			require.True(has, "tx should stay in mempool until block is accepted")

			require.NoError(blk.Verify(t.Context()))
			require.NoError(blk.Accept(t.Context()))

			has = mempool.Has(txID)
			require.False(has, "tx shouldn't be in mempool after block is accepted")
		})
	}
}

func TestWaitForEvent(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		testCase func(*testing.T, *VM, common.Address, *ecdsa.PrivateKey)
	}{
		{
			name: "WaitForEvent with context cancelled returns 0",
			testCase: func(t *testing.T, vm *VM, _ common.Address, _ *ecdsa.PrivateKey) {
				ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
				defer cancel()

				msg, err := vm.WaitForEvent(ctx)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.Zero(t, msg)
			},
		},
		{
			name: "WaitForEvent returns when a transaction is added to the mempool",
			testCase: func(t *testing.T, vm *VM, address common.Address, _ *ecdsa.PrivateKey) {
				importTx, err := vm.newImportTx(vm.Ctx.XChainID, address, vmtest.InitialBaseFee, []*secp256k1.PrivateKey{vmtest.TestKeys[0]})
				require.NoError(t, err)

				type result struct {
					msg commonEng.Message
					err error
				}
				results := make(chan result)
				go func() {
					msg, err := vm.WaitForEvent(t.Context())
					results <- result{
						msg: msg,
						err: err,
					}
				}()

				require.NoError(t, vm.mempool.AddLocalTx(importTx))

				r := <-results
				require.NoError(t, r.err)
				require.Equal(t, commonEng.PendingTxs, r.msg)
			},
		},
		{
			name: "WaitForEvent doesn't return if both mempool is empty",
			testCase: func(t *testing.T, vm *VM, address common.Address, key *ecdsa.PrivateKey) {
				ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond*100)
				defer cancel()

				msg, err := vm.WaitForEvent(ctx)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.Zero(t, msg)

				t.Log("WaitForEvent returns when regular transactions are added to the mempool")

				txs := make([]*types.Transaction, 10)
				for i := 0; i < 10; i++ {
					tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(3*ap0.MinGasPrice), nil)
					signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.Ethereum().BlockChain().Config().ChainID), key)
					require.NoError(t, err)

					txs[i] = signedTx
				}
				errs := vm.Ethereum().TxPool().Add(txs, false, false)
				for _, err := range errs {
					require.NoError(t, err)
				}

				msg, err = vm.WaitForEvent(t.Context())
				require.NoError(t, err)
				require.Equal(t, commonEng.PendingTxs, msg)

				// Build a block again to wipe out the subscription
				blk, err := vm.BuildBlock(t.Context())
				require.NoError(t, err)

				require.NoError(t, blk.Verify(t.Context()))

				require.NoError(t, vm.SetPreference(t.Context(), blk.ID()))

				require.NoError(t, blk.Accept(t.Context()))
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fork := upgradetest.Latest
			vm := newAtomicTestVM()
			tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
				Fork: &fork,
			})
			key := vmtest.TestKeys[0].ToECDSA()
			address := vmtest.TestEthAddrs[0]

			importAmount := uint64(1000000000)
			utxoID := avax.UTXOID{TxID: ids.GenerateTestID()}

			utxo := &avax.UTXO{
				UTXOID: utxoID,
				Asset:  avax.Asset{ID: vm.Ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: importAmount,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{vmtest.TestKeys[0].Address()},
					},
				},
			}
			utxoBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, utxo)
			require.NoError(t, err)

			xChainSharedMemory := tvm.AtomicMemory.NewSharedMemory(vm.Ctx.XChainID)
			inputID := utxo.InputID()
			require.NoError(t, xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{vm.Ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
				Key:   inputID[:],
				Value: utxoBytes,
				Traits: [][]byte{
					vmtest.TestKeys[0].Address().Bytes(),
				},
			}}}}))
			testCase.testCase(t, vm, address, key)
			require.NoError(t, vm.Shutdown(t.Context()))
		})
	}
}
