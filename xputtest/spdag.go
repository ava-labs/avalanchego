// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"time"

	"github.com/ava-labs/salticidae-go"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/networking"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/timer"
	"github.com/ava-labs/gecko/vms/platformvm"
	"github.com/ava-labs/gecko/vms/spdagvm"
	"github.com/ava-labs/gecko/xputtest/dagwallet"
)

// benchmark an instance of the sp dag
func (n *network) benchmarkSPDAG(chain *platformvm.CreateChainTx) {
	genesisBytes := chain.GenesisData
	wallet := dagwallet.NewWallet(n.networkID, chain.ID(), config.AvaTxFee)

	codec := spdagvm.Codec{}
	tx, err := codec.UnmarshalTx(genesisBytes)
	n.log.AssertNoError(err)

	factory := crypto.FactorySECP256K1R{}
	skGen, err := factory.ToPrivateKey(config.Key)
	n.log.AssertNoError(err)
	sk := skGen.(*crypto.PrivateKeySECP256K1R)
	wallet.ImportKey(sk)

	for _, utxo := range tx.UTXOs() {
		wallet.AddUTXO(utxo)
	}

	go n.log.RecoverAndPanic(func() { n.IssueSPDAG(chain.ID(), wallet) })
}

// issue transactions to the instance of the spdag funded by the provided wallet
func (n *network) IssueSPDAG(chainID ids.ID, wallet *dagwallet.Wallet) {
	n.log.Info("starting avalanche benchmark")
	pending := make(map[[32]byte]*spdagvm.Tx)
	canAdd := []*spdagvm.Tx{}
	numAccepted := 0

	n.decided <- ids.ID{}
	meter := timer.TimedMeter{Duration: time.Second}
	for d := range n.decided {
		if numAccepted%1000 == 0 {
			n.log.Info("TPS: %d", meter.Ticks())
		}
		if !d.IsZero() {
			meter.Tick()
			key := d.Key()
			if tx := pending[key]; tx != nil {
				canAdd = append(canAdd, tx)

				n.log.Debug("Finalized %s", d)
				delete(pending, key)
				numAccepted++
			}
		}

		for len(pending) < config.MaxOutstandingTxs && (wallet.Balance() > 0 || len(canAdd) > 0) {
			if wallet.Balance() == 0 {
				tx := canAdd[0]
				canAdd = canAdd[1:]

				for _, utxo := range tx.UTXOs() {
					wallet.AddUTXO(utxo)
				}
			}

			tx := wallet.Send(1, 0, wallet.GetAddress())
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

			pending[tx.ID().Key()] = tx
			n.log.Debug("Sent tx, pending = %d, accepted = %d", len(pending), numAccepted)
		}
	}
}
