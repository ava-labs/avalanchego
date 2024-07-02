// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"crypto/rand"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/antithesishq/antithesis-sdk-go/lifecycle"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/antithesis"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/api"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/cmd/issue/status"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/cmd/issue/transfer"
)

const (
	NumKeys         = 5
	PollingInterval = 50 * time.Millisecond
)

func main() {
	c, err := antithesis.NewConfig(os.Args)
	if err != nil {
		log.Fatalf("invalid config: %s", err)
	}

	ctx := context.Background()
	if err := antithesis.AwaitHealthyNodes(ctx, c.URIs); err != nil {
		log.Fatalf("failed to await healthy nodes: %s", err)
	}

	if len(c.ChainIDs) != 1 {
		log.Fatalf("expected 1 chainID, saw %d", len(c.ChainIDs))
	}
	chainID, err := ids.FromString(c.ChainIDs[0])
	if err != nil {
		log.Fatalf("failed to parse chainID: %s", err)
	}

	genesisWorkload := &workload{
		id:      0,
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
		if err != nil {
			log.Fatalf("failed to generate key: %s", err)
		}

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
		if err != nil {
			log.Fatalf("failed to issue initial funding transfer: %s", err)
		}
		log.Printf("issued initial funding transfer %s in %s", transferTxStatus.TxID, time.Since(baseStartTime))

		genesisWorkload.confirmTransferTx(ctx, transferTxStatus)

		workloads[i] = &workload{
			id:      i,
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
	chainID ids.ID
	key     *secp256k1.PrivateKey
	addrs   set.Set[ids.ShortID]
	uris    []string
}

func (w *workload) run(ctx context.Context) {
	timer := time.NewTimer(0)
	if !timer.Stop() {
		<-timer.C
	}

	uri := w.uris[w.id%len(w.uris)]

	client := api.NewClient(uri, w.chainID.String())
	balance, err := client.Balance(ctx, w.key.Address(), w.chainID)
	if err != nil {
		log.Fatalf("failed to fetch balance: %s", err)
	}
	log.Printf("worker %d starting with a balance of %d", w.id, balance)
	assert.Reachable("worker starting", map[string]any{
		"worker":  w.id,
		"balance": balance,
	})

	for {
		log.Printf("worker %d executing transfer", w.id)
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
			log.Printf("worker %d failed to issue transfer: %s", w.id, err)
		} else {
			log.Printf("worker %d issued transfer %s in %s", w.id, txStatus.TxID, time.Since(txStatus.StartTime))
			w.confirmTransferTx(ctx, txStatus)
		}

		val, err := rand.Int(rand.Reader, big.NewInt(int64(time.Second)))
		if err != nil {
			log.Fatalf("failed to read randomness: %s", err)
		}

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
			log.Printf("worker %d failed to confirm transaction %s on %s: %s", w.id, tx.TxID, uri, err)
			return
		}
	}
	log.Printf("worker %d confirmed transaction %s on all nodes", w.id, tx.TxID)
}
