// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/antithesishq/antithesis-sdk-go/lifecycle"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/antithesis"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	timerpkg "github.com/ava-labs/avalanchego/utils/timer"
	xtxs "github.com/ava-labs/avalanchego/vms/avm/txs"
	ptxs "github.com/ava-labs/avalanchego/vms/platformvm/txs"
	xbuilder "github.com/ava-labs/avalanchego/wallet/chain/x/builder"
)

const NumKeys = 5

// TODO(marun) Extract the common elements of test execution for reuse across test setups

func main() {
	// TODO(marun) Support choosing the log format
	tc := antithesis.NewInstrumentedTestContext(tests.NewDefaultLogger(""))
	defer tc.RecoverAndExit()
	require := require.New(tc)

	c := antithesis.NewConfig(
		tc,
		&tmpnet.Network{
			Owner: "antithesis-avalanchego",
		},
	)
	ctx := tests.DefaultNotifyContext(c.Duration, tc.DeferCleanup)
	// Ensure contexts sourced from the test context use the notify context as their parent
	tc.SetDefaultContextParent(ctx)

	kc := secp256k1fx.NewKeychain(genesis.EWOQKey)
	walletSyncStartTime := time.Now()
	wallet := e2e.NewWallet(tc, kc, tmpnet.NodeURI{URI: c.URIs[0]})
	tc.Log().Info("synced wallet",
		zap.Duration("duration", time.Since(walletSyncStartTime)),
	)

	genesisWorkload := &workload{
		id:     0,
		log:    tests.NewDefaultLogger(fmt.Sprintf("worker %d", 0)),
		wallet: wallet,
		addrs:  set.Of(genesis.EWOQKey.Address()),
		uris:   c.URIs,
	}

	workloads := make([]*workload, NumKeys)
	workloads[0] = genesisWorkload

	var (
		genesisXWallet  = wallet.X()
		genesisXBuilder = genesisXWallet.Builder()
		genesisXContext = genesisXBuilder.Context()
		avaxAssetID     = genesisXContext.AVAXAssetID
	)
	for i := 1; i < NumKeys; i++ {
		key, err := secp256k1.NewPrivateKey()
		require.NoError(err, "failed to generate key")

		var (
			addr          = key.Address()
			baseStartTime = time.Now()
		)
		baseTx, err := genesisXWallet.IssueBaseTx([]*avax.TransferableOutput{{
			Asset: avax.Asset{
				ID: avaxAssetID,
			},
			Out: &secp256k1fx.TransferOutput{
				Amt: 100 * units.KiloAvax,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs: []ids.ShortID{
						addr,
					},
				},
			},
		}})
		require.NoError(err, "failed to issue initial funding X-chain baseTx")
		tc.Log().Info("issued initial funding X-chain baseTx",
			zap.Stringer("txID", baseTx.ID()),
			zap.Duration("duration", time.Since(baseStartTime)),
		)

		require.NoError(genesisWorkload.confirmXChainTx(ctx, baseTx), "failed to confirm initial funding X-chain baseTx")

		uri := c.URIs[i%len(c.URIs)]
		kc := secp256k1fx.NewKeychain(key)
		walletSyncStartTime := time.Now()
		wallet := e2e.NewWallet(tc, kc, tmpnet.NodeURI{URI: uri})
		tc.Log().Info("synced wallet",
			zap.Duration("duration", time.Since(walletSyncStartTime)),
		)

		workloads[i] = &workload{
			id:     i,
			log:    tests.NewDefaultLogger(fmt.Sprintf("worker %d", i)),
			wallet: wallet,
			addrs:  set.Of(addr),
			uris:   c.URIs,
		}
	}

	lifecycle.SetupComplete(map[string]any{
		"msg":        "initialized workers",
		"numWorkers": NumKeys,
	})

	for _, w := range workloads[1:] {
		go w.run(ctx)
	}
	genesisWorkload.run(ctx)
}

type workload struct {
	id     int
	log    logging.Logger
	wallet *primary.Wallet
	addrs  set.Set[ids.ShortID]
	uris   []string
}

// newTestContext returns a test context that ensures that log output and assertions are
// associated with this worker.
func (w *workload) newTestContext(ctx context.Context) *tests.SimpleTestContext {
	return antithesis.NewInstrumentedTestContextWithArgs(
		ctx,
		w.log,
		map[string]any{
			"worker": w.id,
		},
	)
}

func (w *workload) run(ctx context.Context) {
	timer := timerpkg.StoppedTimer()

	tc := w.newTestContext(ctx)
	// Any assertion failure from this test context will result in process exit due to the
	// panic being rethrown. This ensures that failures in test setup are fatal.
	defer tc.RecoverAndRethrow()
	require := require.New(tc)

	xAVAX, pAVAX := e2e.GetWalletBalances(tc, w.wallet)
	assert.Reachable("wallet starting", map[string]any{
		"worker":   w.id,
		"xBalance": xAVAX,
		"pBalance": pAVAX,
	})

	for {
		w.executeTest(ctx)

		val, err := rand.Int(rand.Reader, big.NewInt(int64(time.Second)))
		require.NoError(err, "failed to read randomness")

		timer.Reset(time.Duration(val.Int64()))
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}

// executeTest executes a test at random.
func (w *workload) executeTest(ctx context.Context) {
	tc := w.newTestContext(ctx)
	// Panics will be recovered without being rethrown, ensuring that test failures are not fatal.
	defer tc.Recover()
	require := require.New(tc)

	// Ensure this value matches the number of tests + 1 to offset
	// 0-based + 1 for sleep case in the switch statement for flowID
	testCount := int64(6)

	val, err := rand.Int(rand.Reader, big.NewInt(testCount))
	require.NoError(err, "failed to read randomness")

	flowID := val.Int64()
	switch flowID {
	case 0:
		// TODO(marun) Create abstraction for a test that supports a name e.g. `aTest{name: "foo", mytestfunc}`
		w.log.Info("executing issueXChainBaseTx")
		w.issueXChainBaseTx(ctx)
	case 1:
		w.log.Info("executing issueXChainCreateAssetTx")
		w.issueXChainCreateAssetTx(ctx)
	case 2:
		w.log.Info("executing issueXChainOperationTx")
		w.issueXChainOperationTx(ctx)
	case 3:
		w.log.Info("executing issueXToPTransfer")
		w.issueXToPTransfer(ctx)
	case 4:
		w.log.Info("executing issuePToXTransfer")
		w.issuePToXTransfer(ctx)
	case 5:
		w.log.Info("sleeping")
	}

	// TODO(marun) Enable execution of the banff e2e test as part of https://github.com/ava-labs/avalanchego/issues/4049
	// w.log.Info("executing banff.TestCustomAssetTransfer")
	// addr, _ := w.addrs.Peek()
	// banff.TestCustomAssetTransfer(tc, *w.wallet, addr)
}

func (w *workload) issueXChainBaseTx(ctx context.Context) {
	var (
		xWallet  = w.wallet.X()
		xBuilder = xWallet.Builder()
	)
	balances, err := xBuilder.GetFTBalance()
	if err != nil {
		w.log.Error("failed to fetch X-chain balances",
			zap.Error(err),
		)
		assert.Unreachable("failed to fetch X-chain balances", map[string]any{
			"worker": w.id,
			"err":    err,
		})
		return
	}

	var (
		xContext      = xBuilder.Context()
		avaxAssetID   = xContext.AVAXAssetID
		avaxBalance   = balances[avaxAssetID]
		baseTxFee     = xContext.BaseTxFee
		neededBalance = baseTxFee + units.Schmeckle
	)
	if avaxBalance < neededBalance {
		w.log.Info("skipping X-chain tx issuance due to insufficient balance",
			zap.Uint64("balance", avaxBalance),
			zap.Uint64("neededBalance", neededBalance),
		)
		return
	}

	var (
		owner         = w.makeOwner()
		baseStartTime = time.Now()
	)
	baseTx, err := xWallet.IssueBaseTx(
		[]*avax.TransferableOutput{
			{
				Asset: avax.Asset{
					ID: avaxAssetID,
				},
				Out: &secp256k1fx.TransferOutput{
					Amt:          units.Schmeckle,
					OutputOwners: owner,
				},
			},
		},
	)
	if err != nil {
		w.log.Warn("failed to issue X-chain baseTx",
			zap.Error(err),
		)
		return
	}
	w.log.Info("issued new X-chain baseTx",
		zap.Stringer("txID", baseTx.ID()),
		zap.Duration("duration", time.Since(baseStartTime)),
	)

	if err := w.confirmXChainTx(ctx, baseTx); err != nil {
		w.log.Warn("failed to confirm transaction",
			zap.String("chain", "X"),
			zap.String("txType", "base"),
			zap.Error(err),
		)
		return
	}

	w.verifyXChainTxConsumedUTXOs(ctx, baseTx)
}

func (w *workload) issueXChainCreateAssetTx(ctx context.Context) {
	var (
		xWallet  = w.wallet.X()
		xBuilder = xWallet.Builder()
	)
	balances, err := xBuilder.GetFTBalance()
	if err != nil {
		w.log.Error("failed to fetch X-chain balances",
			zap.Error(err),
		)
		assert.Unreachable("failed to fetch X-chain balances", map[string]any{
			"worker": w.id,
			"err":    err,
		})
		return
	}

	var (
		xContext      = xBuilder.Context()
		avaxAssetID   = xContext.AVAXAssetID
		avaxBalance   = balances[avaxAssetID]
		neededBalance = xContext.CreateAssetTxFee
	)
	if avaxBalance < neededBalance {
		w.log.Info("skipping X-chain tx issuance due to insufficient balance",
			zap.Uint64("balance", avaxBalance),
			zap.Uint64("neededBalance", neededBalance),
		)
		return
	}

	var (
		owner                = w.makeOwner()
		createAssetStartTime = time.Now()
	)
	createAssetTx, err := xWallet.IssueCreateAssetTx(
		"HI",
		"HI",
		1,
		map[uint32][]verify.State{
			0: {
				&secp256k1fx.TransferOutput{
					Amt:          units.Schmeckle,
					OutputOwners: owner,
				},
			},
		},
	)
	if err != nil {
		w.log.Warn("failed to issue X-chain create asset transaction",
			zap.Error(err),
		)
		return
	}
	w.log.Info("created new X-chain asset",
		zap.Stringer("txID", createAssetTx.ID()),
		zap.Duration("duration", time.Since(createAssetStartTime)),
	)

	if err := w.confirmXChainTx(ctx, createAssetTx); err != nil {
		w.log.Warn("failed to confirm transaction",
			zap.String("chain", "X"),
			zap.String("txType", "createAsset"),
			zap.Error(err),
		)
		return
	}

	w.verifyXChainTxConsumedUTXOs(ctx, createAssetTx)
}

func (w *workload) issueXChainOperationTx(ctx context.Context) {
	var (
		xWallet  = w.wallet.X()
		xBuilder = xWallet.Builder()
	)
	balances, err := xBuilder.GetFTBalance()
	if err != nil {
		w.log.Error("failed to fetch X-chain balances",
			zap.Error(err),
		)
		assert.Unreachable("failed to fetch X-chain balances", map[string]any{
			"worker": w.id,
			"err":    err,
		})
		return
	}

	var (
		xContext         = xBuilder.Context()
		avaxAssetID      = xContext.AVAXAssetID
		avaxBalance      = balances[avaxAssetID]
		createAssetTxFee = xContext.CreateAssetTxFee
		baseTxFee        = xContext.BaseTxFee
		neededBalance    = createAssetTxFee + baseTxFee
	)
	if avaxBalance < neededBalance {
		w.log.Info("skipping X-chain tx issuance due to insufficient balance",
			zap.Uint64("balance", avaxBalance),
			zap.Uint64("neededBalance", neededBalance),
		)
		return
	}

	var (
		owner                = w.makeOwner()
		createAssetStartTime = time.Now()
	)
	createAssetTx, err := xWallet.IssueCreateAssetTx(
		"HI",
		"HI",
		1,
		map[uint32][]verify.State{
			2: {
				&propertyfx.MintOutput{
					OutputOwners: owner,
				},
			},
		},
	)
	if err != nil {
		w.log.Warn("failed to issue X-chain create asset transaction",
			zap.Error(err),
		)
		return
	}
	w.log.Info("created new X-chain asset",
		zap.Stringer("txID", createAssetTx.ID()),
		zap.Duration("duration", time.Since(createAssetStartTime)),
	)

	operationStartTime := time.Now()
	operationTx, err := xWallet.IssueOperationTxMintProperty(
		createAssetTx.ID(),
		&owner,
	)
	if err != nil {
		w.log.Warn("failed to issue X-chain operation transaction",
			zap.Error(err),
		)
		return
	}
	w.log.Info("issued X-chain operation transaction",
		zap.Stringer("txID", operationTx.ID()),
		zap.Duration("duration", time.Since(operationStartTime)),
	)

	if err := w.confirmXChainTx(ctx, createAssetTx); err != nil {
		w.log.Warn("failed to confirm transaction",
			zap.String("chain", "X"),
			zap.String("txType", "createAsset"),
			zap.Error(err),
		)
		return
	}

	w.verifyXChainTxConsumedUTXOs(ctx, createAssetTx)

	if err := w.confirmXChainTx(ctx, operationTx); err != nil {
		w.log.Warn("failed to confirm transaction",
			zap.String("chain", "X"),
			zap.String("txType", "operation"),
			zap.Error(err),
		)
		return
	}

	w.verifyXChainTxConsumedUTXOs(ctx, operationTx)
}

func (w *workload) issueXToPTransfer(ctx context.Context) {
	var (
		xWallet  = w.wallet.X()
		pWallet  = w.wallet.P()
		xBuilder = xWallet.Builder()
	)
	balances, err := xBuilder.GetFTBalance()
	if err != nil {
		w.log.Error("failed to fetch X-chain balances",
			zap.Error(err),
		)
		assert.Unreachable("failed to fetch X-chain balances", map[string]any{
			"worker": w.id,
			"err":    err,
		})
		return
	}

	var (
		xContext      = xBuilder.Context()
		avaxAssetID   = xContext.AVAXAssetID
		avaxBalance   = balances[avaxAssetID]
		xBaseTxFee    = xContext.BaseTxFee
		neededBalance = xBaseTxFee + units.Avax
	)
	if avaxBalance < neededBalance {
		w.log.Info("skipping X-chain tx issuance due to insufficient balance",
			zap.Uint64("balance", avaxBalance),
			zap.Uint64("neededBalance", neededBalance),
		)
		return
	}

	var (
		owner           = w.makeOwner()
		exportStartTime = time.Now()
	)
	exportTx, err := xWallet.IssueExportTx(
		constants.PlatformChainID,
		[]*avax.TransferableOutput{{
			Asset: avax.Asset{
				ID: avaxAssetID,
			},
			Out: &secp256k1fx.TransferOutput{
				Amt: units.Avax,
			},
		}},
	)
	if err != nil {
		w.log.Warn("failed to issue X-chain export transaction",
			zap.Error(err),
		)
		return
	}
	w.log.Info("created X-chain export transaction",
		zap.Stringer("txID", exportTx.ID()),
		zap.Duration("duration", time.Since(exportStartTime)),
	)

	var (
		xChainID        = xContext.BlockchainID
		importStartTime = time.Now()
	)
	importTx, err := pWallet.IssueImportTx(
		xChainID,
		&owner,
	)
	if err != nil {
		w.log.Warn("failed to issue P-chain import transaction",
			zap.Error(err),
		)
		return
	}
	w.log.Info("created P-chain import transaction",
		zap.Stringer("txID", importTx.ID()),
		zap.Duration("duration", time.Since(importStartTime)),
	)

	if err := w.confirmXChainTx(ctx, exportTx); err != nil {
		w.log.Warn("failed to confirm transaction",
			zap.String("chain", "X"),
			zap.String("txType", "export"),
			zap.Error(err),
		)
		return
	}

	w.verifyXChainTxConsumedUTXOs(ctx, exportTx)

	if err := w.confirmPChainTx(ctx, importTx); err != nil {
		w.log.Warn("failed to confirm transaction",
			zap.String("chain", "P"),
			zap.String("txType", "import"),
			zap.Error(err),
		)
		return
	}

	w.verifyPChainTxConsumedUTXOs(ctx, importTx)
}

func (w *workload) issuePToXTransfer(ctx context.Context) {
	var (
		xWallet  = w.wallet.X()
		pWallet  = w.wallet.P()
		xBuilder = xWallet.Builder()
		pBuilder = pWallet.Builder()
	)
	balances, err := pBuilder.GetBalance()
	if err != nil {
		w.log.Error("failed to fetch P-chain balances",
			zap.Error(err),
		)
		assert.Unreachable("failed to fetch P-chain balances", map[string]any{
			"worker": w.id,
			"err":    err,
		})
		return
	}

	var (
		xContext      = xBuilder.Context()
		pContext      = pBuilder.Context()
		avaxAssetID   = pContext.AVAXAssetID
		avaxBalance   = balances[avaxAssetID]
		txFees        = xContext.BaseTxFee
		neededBalance = txFees + units.Schmeckle
	)
	if avaxBalance < neededBalance {
		w.log.Info("skipping P-chain tx issuance due to insufficient balance",
			zap.Uint64("balance", avaxBalance),
			zap.Uint64("neededBalance", neededBalance),
		)
		return
	}

	var (
		xChainID        = xContext.BlockchainID
		owner           = w.makeOwner()
		exportStartTime = time.Now()
	)
	exportTx, err := pWallet.IssueExportTx(
		xChainID,
		[]*avax.TransferableOutput{{
			Asset: avax.Asset{
				ID: avaxAssetID,
			},
			Out: &secp256k1fx.TransferOutput{
				Amt: units.Schmeckle,
			},
		}},
	)
	if err != nil {
		w.log.Warn("failed to issue P-chain export transaction",
			zap.Error(err),
		)
		return
	}
	w.log.Info("created P-chain export transaction",
		zap.Stringer("txID", exportTx.ID()),
		zap.Duration("duration", time.Since(exportStartTime)),
	)

	importStartTime := time.Now()
	importTx, err := xWallet.IssueImportTx(
		constants.PlatformChainID,
		&owner,
	)
	if err != nil {
		w.log.Warn("failed to issue X-chain import transaction",
			zap.Error(err),
		)
		return
	}
	w.log.Info("created X-chain import transaction",
		zap.Stringer("txID", importTx.ID()),
		zap.Duration("duration", time.Since(importStartTime)),
	)

	if err := w.confirmPChainTx(ctx, exportTx); err != nil {
		w.log.Warn("failed to confirm transaction",
			zap.String("chain", "P"),
			zap.String("txType", "export"),
			zap.Error(err),
		)
		return
	}

	w.verifyPChainTxConsumedUTXOs(ctx, exportTx)

	if err := w.confirmXChainTx(ctx, importTx); err != nil {
		w.log.Warn("failed to confirm transaction",
			zap.String("chain", "X"),
			zap.String("txType", "import"),
			zap.Error(err),
		)
		return
	}

	w.verifyXChainTxConsumedUTXOs(ctx, importTx)
}

func (w *workload) makeOwner() secp256k1fx.OutputOwners {
	addr, _ := w.addrs.Peek()
	return secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			addr,
		},
	}
}

func (w *workload) confirmXChainTx(ctx context.Context, tx *xtxs.Tx) error {
	txID := tx.ID()
	for _, uri := range w.uris {
		client := avm.NewClient(uri, "X")
		if err := avm.AwaitTxAccepted(client, ctx, txID, 100*time.Millisecond); err != nil {
			return fmt.Errorf("failed to confirm X-chain transaction %s on %s: %w", txID, uri, err)
		}
		w.log.Info("confirmed X-chain transaction",
			zap.Stringer("txID", txID),
			zap.String("uri", uri),
		)
	}
	w.log.Info("confirmed X-chain transaction",
		zap.Stringer("txID", txID),
	)
	return nil
}

func (w *workload) confirmPChainTx(ctx context.Context, tx *ptxs.Tx) error {
	txID := tx.ID()
	for _, uri := range w.uris {
		client := platformvm.NewClient(uri)
		if err := platformvm.AwaitTxAccepted(client, ctx, txID, 100*time.Millisecond); err != nil {
			return fmt.Errorf("failed to confirm P-chain transaction %s on %s: %w", txID, uri, err)
		}
		w.log.Info("confirmed P-chain transaction",
			zap.Stringer("txID", txID),
			zap.String("uri", uri),
		)
	}
	w.log.Info("confirmed P-chain transaction",
		zap.Stringer("txID", txID),
	)
	return nil
}

func (w *workload) verifyXChainTxConsumedUTXOs(ctx context.Context, tx *xtxs.Tx) {
	txID := tx.ID()
	chainID := w.wallet.X().Builder().Context().BlockchainID
	for _, uri := range w.uris {
		client := avm.NewClient(uri, "X")

		utxos := common.NewUTXOs()
		err := primary.AddAllUTXOs(
			ctx,
			utxos,
			client,
			xbuilder.Parser.Codec(),
			chainID,
			chainID,
			w.addrs.List(),
		)
		if err != nil {
			w.log.Warn("failed to fetch X-chain UTXOs",
				zap.String("uri", uri),
				zap.Error(err),
			)
			return
		}

		inputs := tx.Unsigned.InputIDs()
		for input := range inputs {
			_, err := utxos.GetUTXO(ctx, chainID, chainID, input)
			if err != database.ErrNotFound {
				w.log.Error("failed to verify that X-chain UTXO was deleted",
					zap.String("uri", uri),
					zap.Stringer("txID", txID),
					zap.Stringer("utxoID", input),
					zap.Error(err),
				)
				assert.Unreachable("failed to verify that X-chain UTXO was deleted", map[string]any{
					"worker": w.id,
					"uri":    uri,
					"txID":   txID,
					"utxoID": input,
					"err":    err,
				})
				return
			}
		}
		w.log.Info("confirmed all X-chain UTXOs consumed by tx are not present on node",
			zap.Stringer("txID", txID),
			zap.String("uri", uri),
		)
	}
	w.log.Info("confirmed all X-chain UTXOs consumed by tx are not present on all nodes",
		zap.Stringer("txID", txID),
	)
}

func (w *workload) verifyPChainTxConsumedUTXOs(ctx context.Context, tx *ptxs.Tx) {
	txID := tx.ID()
	for _, uri := range w.uris {
		client := platformvm.NewClient(uri)

		utxos := common.NewUTXOs()
		err := primary.AddAllUTXOs(
			ctx,
			utxos,
			client,
			ptxs.Codec,
			constants.PlatformChainID,
			constants.PlatformChainID,
			w.addrs.List(),
		)
		if err != nil {
			w.log.Warn("failed to fetch P-chain UTXOs",
				zap.String("uri", uri),
				zap.Error(err),
			)
			return
		}

		inputs := tx.Unsigned.InputIDs()
		for input := range inputs {
			_, err := utxos.GetUTXO(ctx, constants.PlatformChainID, constants.PlatformChainID, input)
			if err != database.ErrNotFound {
				w.log.Error("failed to verify that P-chain UTXO was deleted",
					zap.String("uri", uri),
					zap.Stringer("txID", txID),
					zap.Stringer("utxoID", input),
					zap.Error(err),
				)
				assert.Unreachable("failed to verify that P-chain UTXO was deleted", map[string]any{
					"worker": w.id,
					"uri":    uri,
					"txID":   txID,
					"utxoID": input,
					"err":    err,
				})
				return
			}
		}
		w.log.Info("confirmed all P-chain UTXOs consumed by tx are not present on node",
			zap.Stringer("txID", txID),
			zap.String("uri", uri),
		)
	}
	w.log.Info("confirmed all P-chain UTXOs consumed by tx are not present on all nodes",
		zap.Stringer("txID", txID),
	)
}
