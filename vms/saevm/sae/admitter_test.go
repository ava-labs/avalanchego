// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks/blockstest"
	"github.com/ava-labs/avalanchego/vms/saevm/hook/hookstest"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/saevm/saexec"
)

// admitterOption configures an [admitterFixture] before [saexec.Executor]
// construction.
type admitterOption func(*hookstest.Stub)

// withRequiresAdmissionCheck installs a stub for
// [hook.Points.RequiresTransactionAdmissionCheck].
func withRequiresAdmissionCheck(fn func(params.Rules) bool) admitterOption {
	return func(s *hookstest.Stub) { s.RequiresAdmissionCheck = fn }
}

// withCanExecuteTransaction installs a stub for
// [hook.Points.CanExecuteTransaction].
func withCanExecuteTransaction(fn func(params.Rules, common.Address, *common.Address, libevm.StateReader) error) admitterOption {
	return func(s *hookstest.Stub) { s.CanExecuteTransactionFn = fn }
}

// admitterFixture wires a real [saexec.Executor] (sitting on the genesis
// block) to a [hookstest.Stub] and an [admitter]. Tests pass
// [admitterOption]s (e.g. [withRequiresAdmissionCheck],
// [withCanExecuteTransaction]) to drive admission decisions.
type admitterFixture struct {
	admit  *admitter
	hooks  *hookstest.Stub
	wallet *saetest.Wallet
	exec   *saexec.Executor
}

// newAdmitterFixture builds an [admitterFixture] funded with `keys`. Callers
// are expected to derive the wallet keychain themselves so they can refer to
// the addresses (e.g. in table rows) before the fixture exists.
func newAdmitterFixture(t *testing.T, keys *saetest.KeyChain, opts ...admitterOption) *admitterFixture {
	t.Helper()

	logger := saetest.NewTBLogger(t, logging.Warn)
	config := saetest.ChainConfig()
	wallet := saetest.NewWalletWithKeyChain(keys, types.LatestSigner(config))

	db := rawdb.NewMemoryDatabase()
	xdb := saetest.NewExecutionResultsDB()
	genesis := blockstest.NewGenesis(t, db, xdb, config, saetest.MaxAllocFor(keys.Addresses()...))
	src := blocks.Source(blockstest.NewChainBuilder(genesis).GetBlock)

	hooks := hookstest.NewStub(1e6)
	for _, opt := range opts {
		opt(hooks)
	}

	exec, err := saexec.New(genesis, src.AsHeaderSource(), config, db, xdb, saedb.Config{}, hooks, logger)
	require.NoError(t, err, "saexec.New()")
	t.Cleanup(func() {
		require.NoErrorf(t, exec.Close(), "%T.Close()", exec)
	})

	return &admitterFixture{
		admit:  newAdmitter(exec, hooks, config),
		hooks:  hooks,
		wallet: wallet,
		exec:   exec,
	}
}

// signTx signs a trivial value transfer from `from` to `to`.
func (f *admitterFixture) signTx(t *testing.T, from int, to common.Address) *types.Transaction {
	t.Helper()
	return f.wallet.SetNonceAndSign(t, from, &types.DynamicFeeTx{
		To:        &to,
		Gas:       params.TxGas,
		GasFeeCap: big.NewInt(1),
	})
}

// TestAdmit cross-products the two hook decisions
// ([RequiresTransactionAdmissionCheck] x [CanExecuteTransaction]) by
// stubbing the corresponding [hookstest.Stub] fields directly per row.
func TestAdmit(t *testing.T) {
	keys := saetest.NewUNSAFEKeyChain(t, 2)
	addrs := keys.Addresses()

	blockedErr := errors.New("not allow-listed")

	type hit struct{ from, to common.Address }

	tests := []struct {
		name                   string
		RequiresAdmissionCheck func(params.Rules) bool
		// CanExecuteTransaction MAY assert on its received `from` / `to`
		// directly (e.g. to selectively block a specific sender) and is
		// expected to surface its decision via the returned error.
		CanExecuteTransaction func(params.Rules, common.Address, *common.Address, libevm.StateReader) error
		wantErr               map[common.Address]error
	}{
		{
			name:                   "no_check_required_hook_skipped",
			RequiresAdmissionCheck: func(params.Rules) bool { return false },
			CanExecuteTransaction: func(params.Rules, common.Address, *common.Address, libevm.StateReader) error {
				t.Fatal("CanExecuteTransaction must not be reached when RequiresAdmissionCheck is false")
				return nil
			},
		},
		{
			name:                   "check_required_and_allowed",
			RequiresAdmissionCheck: func(params.Rules) bool { return true },
			CanExecuteTransaction: func(params.Rules, common.Address, *common.Address, libevm.StateReader) error {
				return nil
			},
		},
		{
			name:                   "check_required_and_blocked_for_specific_sender",
			RequiresAdmissionCheck: func(params.Rules) bool { return true },
			// Reject only when the recovered sender matches `from`. This
			// proves the hook receives the exact tx sender (not e.g. the
			// recipient) by acting on it directly.
			CanExecuteTransaction: func(_ params.Rules, from common.Address, _ *common.Address, _ libevm.StateReader) error {
				if from == addrs[0] {
					return blockedErr
				}
				return nil
			},
			wantErr: map[common.Address]error{
				addrs[0]: blockedErr,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newAdmitterFixture(t, keys,
				withRequiresAdmissionCheck(tt.RequiresAdmissionCheck),
				withCanExecuteTransaction(tt.CanExecuteTransaction),
			)

			for idx, address := range addrs {
				err := f.admit.Admit(f.signTx(t, idx, address))
				require.ErrorIs(t, err, tt.wantErr[address])
			}
		})
	}
}
