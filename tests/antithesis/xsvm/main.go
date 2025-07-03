// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/antithesis"
	"github.com/ava-labs/avalanchego/tests/fixture/subnet"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/api"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/cmd/issue/status"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/cmd/issue/transfer"

	timerpkg "github.com/ava-labs/avalanchego/utils/timer"
)

const (
	NumKeys         = 5
	PollingInterval = 50 * time.Millisecond
)

func main() {
	// TODO(marun) Support choosing the log format
	tc := antithesis.NewInstrumentedTestContext(tests.NewDefaultLogger(""))
	defer tc.RecoverAndExit()
	require := require.New(tc)

	c := antithesis.NewConfigWithSubnets(
		tc,
		&tmpnet.Network{
			Owner: "antithesis-xsvm",
		},
		func(nodes ...*tmpnet.Node) []*tmpnet.Subnet {
			return []*tmpnet.Subnet{
				subnet.NewXSVMOrPanic("xsvm", genesis.VMRQKey, nodes...),
			}
		},
	)
	ctx := tests.DefaultNotifyContext(c.Duration, tc.DeferCleanup)
	// Ensure contexts sourced from the test context use the notify context as their parent
	tc.SetDefaultContextParent(ctx)

	require.Len(c.ChainIDs, 1)
	tc.Log().Debug("raw chain ID",
		zap.String("chainID", c.ChainIDs[0]),
	)
	chainID, err := ids.FromString(c.ChainIDs[0])
	require.NoError(err, "failed to parse chainID")
	tc.Log().Info("node and chain configuration",
		zap.Stringer("chainID", chainID),
		zap.Strings("uris", c.URIs),
	)

	genesisWorkload := &workload{
		id:      0,
		log:     tests.NewDefaultLogger(fmt.Sprintf("worker %d", 0)),
		chainID: chainID,
		key:     genesis.VMRQKey,
		addrs:   set.Of(genesis.VMRQKey.Address()),
		uris:    c.URIs,
	}

	workloads := make([]*workload, NumKeys)
	workloads[0] = genesisWorkload

	initialAmount := 100 * units.KiloAvax
	for i := 1; i < NumKeys; i++ {
		key, err := secp256k1.NewPrivateKey()
		require.NoError(err, "failed to generate key")

		var (
			addr          = key.Address()
			baseStartTime = time.Now()
		)
		transferTxStatus, err := transfer.Transfer(
			ctx,
			&transfer.Config{
				URI:        c.URIs[0],
				ChainID:    chainID,
				AssetID:    chainID,
				Amount:     initialAmount,
				To:         addr,
				PrivateKey: genesisWorkload.key,
			},
		)
		require.NoError(err, "failed to issue initial funding transfer")
		tc.Log().Info("issued initial funding transfer",
			zap.Stringer("txID", transferTxStatus.TxID),
			zap.Duration("duration", time.Since(baseStartTime)),
		)

		genesisWorkload.confirmTransferTx(ctx, transferTxStatus)

		workloads[i] = &workload{
			id:      i,
			log:     tests.NewDefaultLogger(fmt.Sprintf("worker %d", i)),
			chainID: chainID,
			key:     key,
			addrs:   set.Of(addr),
			uris:    c.URIs,
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
	id      int
	log     logging.Logger
	chainID ids.ID
	key     *secp256k1.PrivateKey
	addrs   set.Set[ids.ShortID]
	uris    []string
}

func (w *workload) run(ctx context.Context) {
	timer := timerpkg.StoppedTimer()

	tc := antithesis.NewInstrumentedTestContextWithArgs(
		ctx,
		w.log,
		map[string]any{
			"worker": w.id,
		},
	)
	// Any assertion failure from this test context will result in process exit due to the
	// panic being rethrown. This ensures that failures in test setup are fatal.
	defer tc.RecoverAndRethrow()
	require := require.New(tc)

	uri := w.uris[w.id%len(w.uris)]

	client := api.NewClient(uri, w.chainID.String())
	balance, err := client.Balance(ctx, w.key.Address(), w.chainID)
	require.NoError(err, "failed to fetch balance")
	w.log.Info("worker starting",
		zap.Int("worker", w.id),
		zap.Uint64("balance", balance),
	)
	assert.Reachable("worker starting", map[string]any{
		"worker":  w.id,
		"balance": balance,
	})

	for {
		w.log.Info("executing transfer",
			zap.Int("worker", w.id),
		)
		destAddress, _ := w.addrs.Peek()
		txStatus, err := transfer.Transfer(
			ctx,
			&transfer.Config{
				URI:        uri,
				ChainID:    w.chainID,
				AssetID:    w.chainID,
				Amount:     units.Schmeckle,
				To:         destAddress,
				PrivateKey: w.key,
			},
		)
		if err != nil {
			w.log.Warn("failed to issue transfer",
				zap.Int("worker", w.id),
				zap.Error(err),
			)
		} else {
			w.log.Info("issued transfer",
				zap.Int("worker", w.id),
				zap.Stringer("txID", txStatus.TxID),
				zap.Duration("duration", time.Since(txStatus.StartTime)),
			)
			w.confirmTransferTx(ctx, txStatus)
		}

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

func (w *workload) confirmTransferTx(ctx context.Context, tx *status.TxIssuance) {
	for _, uri := range w.uris {
		client := api.NewClient(uri, w.chainID.String())
		if err := api.AwaitTxAccepted(ctx, client, w.key.Address(), tx.Nonce, PollingInterval); err != nil {
			w.log.Warn("failed to confirm transaction",
				zap.Int("worker", w.id),
				zap.Stringer("txID", tx.TxID),
				zap.String("uri", uri),
				zap.Error(err),
			)
			return
		}
	}
	w.log.Info("confirmed transaction on all nodes",
		zap.Int("worker", w.id),
		zap.Stringer("txID", tx.TxID),
	)
}
