// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"time"

	"github.com/ava-labs/salticidae-go"

	"github.com/ava-labs/gecko/genesis"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/networking"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/timer"
	"github.com/ava-labs/gecko/vms/avm"
	"github.com/ava-labs/gecko/vms/platformvm"
	"github.com/ava-labs/gecko/xputtest/avmwallet"
)

func (n *network) benchmarkAVM(genesisState *platformvm.Genesis) {
	avmChain := genesisState.Chains[0]
	n.log.AssertTrue(avmChain.ChainName == "AVM", "wrong chain name")
	genesisBytes := avmChain.GenesisData

	wallet, err := avmwallet.NewWallet(n.networkID, avmChain.ID(), config.AvaTxFee)
	n.log.AssertNoError(err)

	cb58 := formatting.CB58{}
	keyStr := genesis.Keys[config.Key]
	n.log.AssertNoError(cb58.FromString(keyStr))
	factory := crypto.FactorySECP256K1R{}
	sk, err := factory.ToPrivateKey(cb58.Bytes)
	n.log.AssertNoError(err)
	wallet.ImportKey(sk.(*crypto.PrivateKeySECP256K1R))

	codec := wallet.Codec()

	genesis := avm.Genesis{}
	n.log.AssertNoError(codec.Unmarshal(genesisBytes, &genesis))

	genesisTx := genesis.Txs[0]
	tx := avm.Tx{
		UnsignedTx: &genesisTx.CreateAssetTx,
	}
	txBytes, err := codec.Marshal(&tx)
	n.log.AssertNoError(err)
	tx.Initialize(txBytes)

	for _, utxo := range tx.UTXOs() {
		wallet.AddUTXO(utxo)
	}

	assetID := genesisTx.ID()

	n.log.AssertNoError(wallet.GenerateTxs(config.NumTxs, assetID))

	go n.log.RecoverAndPanic(func() { n.IssueAVM(avmChain.ID(), assetID, wallet) })
}

func (n *network) IssueAVM(chainID ids.ID, assetID ids.ID, wallet *avmwallet.Wallet) {
	n.log.Debug("Issuing with %d", wallet.Balance(assetID))
	numAccepted := 0
	numPending := 0

	n.decided <- ids.ID{}

	meter := timer.TimedMeter{Duration: time.Second}
	for d := range n.decided {
		if numAccepted%1000 == 0 {
			n.log.Info("TPS: %d", meter.Ticks())
		}
		if !d.IsZero() {
			meter.Tick()
			n.log.Debug("Finalized %s", d)
			numAccepted++
			numPending--
		}

		for numPending < config.MaxOutstandingTxs && wallet.Balance(assetID) > 0 && numAccepted+numPending < config.NumTxs {
			tx := wallet.NextTx()
			n.log.AssertTrue(tx != nil, "Tx creation failed")

			it, err := n.build.IssueTx(chainID, tx.Bytes())
			n.log.AssertNoError(err)
			ds := it.DataStream()
			ba := salticidae.NewByteArrayMovedFromDataStream(ds, false)
			newMsg := salticidae.NewMsgMovedFromByteArray(networking.IssueTx, ba, false)

			n.conn.GetNet().SendMsg(newMsg, n.conn)

			ds.Free()
			ba.Free()
			newMsg.Free()

			numPending++
			n.log.Debug("Sent tx, pending = %d, accepted = %d", numPending, numAccepted)
		}
		if numAccepted+numPending >= config.NumTxs {
			n.log.Info("done with test")
			return
		}
	}
}
