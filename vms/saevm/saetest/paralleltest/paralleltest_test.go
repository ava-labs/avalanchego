// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package paralleltest

import (
	"math/big"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/libevm/precompiles/parallel"
	"github.com/ava-labs/libevm/params"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/saevm/cmputils"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, goleak.IgnoreCurrent())
}

var (
	_ parallel.Handler[
		struct{},
		common.Hash,
		*txHashEchoer,
		map[int]common.Hash,
	] = (*handler)(nil)

	_ parallel.PrecompileResult = (*txHashEchoer)(nil)
)

// handler is a deliberately simple implementation of [parallel.Handler],
// intended only to demonstrate integration between SAE hooks and the [parallel]
// package. Full testing of parallel handlers is performed in that package.
type handler struct {
	addr common.Address
	gas  uint64
}

func (*handler) BeforeBlock(libevm.StateReader, *types.Header) struct{} {
	return struct{}{}
}

func (h *handler) ShouldProcess(tx parallel.IndexedTx, _ struct{}) (bool, uint64) {
	if to := tx.To(); to == nil || *to != h.addr {
		return false, 0
	}
	return true, h.gas
}

func (*handler) Prefetch(_ libevm.StateReader, tx parallel.IndexedTx, _ struct{}) common.Hash {
	return tx.Hash()
}

func (*handler) Process(_ libevm.StateReader, _ parallel.IndexedTx, _ struct{}, txHash common.Hash) *txHashEchoer {
	return &txHashEchoer{hash: txHash}
}

func (*handler) PostProcess(_ struct{}, res parallel.Results[*txHashEchoer]) map[int]common.Hash {
	out := make(map[int]common.Hash)
	for r := range res.ProcessOrder {
		// Although the hash is available in the [parallel.TxResult], we use the
		// value in the [txHashEchoer] to test the pipeline.
		out[r.Tx.Index] = r.Result.hash
	}
	return out
}

func (*handler) AfterBlock(parallel.StateDB, map[int]common.Hash, *types.Block, types.Receipts) {}

type txHashEchoer struct {
	hash common.Hash
}

func shouldRevert(h common.Hash) bool {
	return h[0]%2 == 0
}

func (e *txHashEchoer) PrecompileOutput(env vm.PrecompileEnvironment, _ []byte) ([]byte, error) {
	ret := slices.Clone(e.hash.Bytes())
	if shouldRevert(e.hash) {
		return ret, vm.ErrExecutionReverted
	}

	for _, l := range logs(env.Addresses().EVMSemantic.Self, e.hash) {
		env.StateDB().AddLog(l)
	}
	return ret, nil
}

func logs(a common.Address, h common.Hash) []*types.Log {
	return []*types.Log{{
		Address: a,
		Topics:  []common.Hash{h},
	}}
}

func TestNewExecutor(t *testing.T) {
	logger := loggingtest.New(t, logging.Warn)
	ctx := logger.CancelOnError(t.Context())

	config := saetest.ChainConfig()
	signer := types.LatestSigner(config)
	wallet := saetest.NewUNSAFEWallet(t, 1, signer)

	precompileAddr := common.Address{'p', 'r', 'e'}
	const precompileGas = 1000
	h := &handler{precompileAddr, precompileGas}

	// SUT
	exec, chain := NewExecutor(
		t,
		logger,
		rawdb.NewMemoryDatabase(),
		config,
		saetest.MaxAllocFor(wallet.Addresses()...),
		precompileAddr,
		h, 4, 4,
	)

	const gasLimit = params.TxGas + precompileGas
	var txs []common.Hash
	for range 10 {
		tx := wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			To:        &precompileAddr,
			Gas:       gasLimit,
			GasFeeCap: big.NewInt(100),
		})
		txs = append(txs, tx.Hash())

		b := chain.NewBlock(t, types.Transactions{tx})
		require.NoErrorf(t, exec.Enqueue(ctx, b), "%T.Enqueue()", exec)
	}
	require.NoErrorf(t, chain.Last().WaitUntilExecuted(ctx), "%T.Last().WaitUntilExecuted()", chain)

	for i, b := range chain.AllExceptGenesis() {
		require.Len(t, b.Receipts(), 1, "%T[%d].Receipts()", b, b.NumberU64())

		ignore := cmp.Options{
			cmpopts.IgnoreFields(
				types.Receipt{},
				"Type",
				"Bloom",
				"EffectiveGasPrice",
			),
			cmpopts.IgnoreFields(
				types.Log{},
				"BlockNumber", "TxHash", "BlockHash",
			),
		}
		want := &types.Receipt{
			BlockHash:         b.Hash(),
			BlockNumber:       b.Number(),
			TxHash:            txs[i],
			GasUsed:           gasLimit,
			CumulativeGasUsed: gasLimit,
		}
		if shouldRevert(want.TxHash) {
			want.Status = types.ReceiptStatusFailed
		} else {
			want.Status = types.ReceiptStatusSuccessful
			want.Logs = logs(precompileAddr, txs[i])
		}

		if diff := cmp.Diff(want, b.Receipts()[0], ignore, cmputils.BigInts()); diff != "" {
			t.Errorf("%T diff (-want +got):\n%s", b.Receipts()[0], diff)
		}
	}
}
