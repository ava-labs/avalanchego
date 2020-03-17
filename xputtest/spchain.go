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
	"github.com/ava-labs/gecko/vms/platformvm"
	"github.com/ava-labs/gecko/vms/spchainvm"
	"github.com/ava-labs/gecko/xputtest/chainwallet"
)

// benchmark an instance of the sp chain
func (n *network) benchmarkSPChain(chain *platformvm.CreateChainTx) {
	genesisBytes := chain.GenesisData
	wallet := chainwallet.NewWallet(n.networkID, chain.ID())

	codec := spchainvm.Codec{}
	accounts, err := codec.UnmarshalGenesis(genesisBytes)
	n.log.AssertNoError(err)

	cb58 := formatting.CB58{}
	factory := crypto.FactorySECP256K1R{}
	for _, keyStr := range genesis.Keys {
		n.log.AssertNoError(cb58.FromString(keyStr))
		skGen, err := factory.ToPrivateKey(cb58.Bytes)
		n.log.AssertNoError(err)
		sk := skGen.(*crypto.PrivateKeySECP256K1R)
		wallet.ImportKey(sk)
	}

	for _, account := range accounts {
		wallet.AddAccount(account)
		break
	}

	n.log.AssertNoError(wallet.GenerateTxs(config.NumTxs))

	go n.log.RecoverAndPanic(func() { n.IssueSPChain(chain.ID(), wallet) })
}

func (n *network) IssueSPChain(chainID ids.ID, wallet *chainwallet.Wallet) {
	n.log.Debug("Issuing with %d", wallet.Balance())
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

		for numPending < config.MaxOutstandingTxs && wallet.Balance() > 0 && numAccepted+numPending < config.NumTxs {
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
