// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

// #include "salticidae/network.h"
// void onTerm(int sig, void *);
// void decidedTx(msg_t *, msgnetwork_conn_t *, void *);
import "C"

import (
	"fmt"
	"os"
	"path"
	"runtime"
	"runtime/pprof"
	"time"
	"unsafe"

	"github.com/ava-labs/salticidae-go"

	"github.com/ava-labs/gecko/genesis"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/networking"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/timer"
	"github.com/ava-labs/gecko/vms/platformvm"
	"github.com/ava-labs/gecko/vms/spchainvm"
	"github.com/ava-labs/gecko/vms/spdagvm"
	"github.com/ava-labs/gecko/xputtest/chainwallet"
	"github.com/ava-labs/gecko/xputtest/dagwallet"
)

// tp stores the persistent data needed when running the test.
type tp struct {
	ec    salticidae.EventContext
	build networking.Builder

	conn salticidae.MsgNetworkConn

	log     logging.Logger
	decided chan ids.ID

	networkID uint32
}

var t = tp{}

//export onTerm
func onTerm(C.int, unsafe.Pointer) {
	t.log.Info("Terminate signal received")
	t.ec.Stop()
}

// decidedTx handles the recept of a decidedTx message
//export decidedTx
func decidedTx(_msg *C.struct_msg_t, _conn *C.struct_msgnetwork_conn_t, _ unsafe.Pointer) {
	msg := salticidae.MsgFromC(salticidae.CMsg(_msg))

	pMsg, err := t.build.Parse(networking.DecidedTx, msg.GetPayloadByMove())
	if err != nil {
		t.log.Warn("Failed to parse DecidedTx message")
		return
	}

	txID, err := ids.ToID(pMsg.Get(networking.TxID).([]byte))
	t.log.AssertNoError(err) // Length is checked in message parsing

	t.log.Debug("Decided %s", txID)
	t.decided <- txID
}

func main() {
	if err != nil {
		fmt.Printf("Failed to parse arguments: %s\n", err)
	}

	config.LoggingConfig.Directory = path.Join(config.LoggingConfig.Directory, "client")
	log, err := logging.New(config.LoggingConfig)
	if err != nil {
		fmt.Printf("Failed to start the logger: %s\n", err)
		return
	}

	defer log.Stop()

	t.log = log
	crypto.EnableCrypto = config.EnableCrypto
	t.decided = make(chan ids.ID, config.MaxOutstandingTxs)

	if config.Key >= len(genesis.Keys) || config.Key < 0 {
		log.Fatal("Unknown key specified")
		return
	}

	t.ec = salticidae.NewEventContext()
	evInt := salticidae.NewSigEvent(t.ec, salticidae.SigEventCallback(C.onTerm), nil)
	evInt.Add(salticidae.SIGINT)
	evTerm := salticidae.NewSigEvent(t.ec, salticidae.SigEventCallback(C.onTerm), nil)
	evTerm.Add(salticidae.SIGTERM)

	serr := salticidae.NewError()
	netconfig := salticidae.NewMsgNetworkConfig()
	net := salticidae.NewMsgNetwork(t.ec, netconfig, &serr)
	if serr.GetCode() != 0 {
		log.Fatal("Sync error %s", salticidae.StrError(serr.GetCode()))
		return
	}

	net.RegHandler(networking.DecidedTx, salticidae.MsgNetworkMsgCallback(C.decidedTx), nil)

	net.Start()
	defer net.Stop()

	remoteIP := salticidae.NewNetAddrFromIPPortString(config.RemoteIP.String(), true, &serr)
	if code := serr.GetCode(); code != 0 {
		log.Fatal("Sync error %s", salticidae.StrError(serr.GetCode()))
		return
	}

	t.conn = net.ConnectSync(remoteIP, true, &serr)
	if serr.GetCode() != 0 {
		log.Fatal("Sync error %s", salticidae.StrError(serr.GetCode()))
		return
	}

	file, gErr := os.Create("cpu_client.profile")
	log.AssertNoError(gErr)
	gErr = pprof.StartCPUProfile(file)
	log.AssertNoError(gErr)
	runtime.SetMutexProfileFraction(1)

	defer file.Close()
	defer pprof.StopCPUProfile()

	t.networkID = config.NetworkID

	switch config.Chain {
	case ChainChain:
		t.benchmarkSnowman()
	case DagChain:
		t.benchmarkAvalanche()
	default:
		t.log.Fatal("did not specify whether to test dag or chain. Exiting")
		return
	}

	t.ec.Dispatch()
}

func (t *tp) benchmarkAvalanche() {
	platformGenesisBytes, _ := genesis.Genesis(t.networkID)
	genesisState := &platformvm.Genesis{}
	err := platformvm.Codec.Unmarshal(platformGenesisBytes, genesisState)
	t.log.AssertNoError(err)
	t.log.AssertNoError(genesisState.Initialize())

	spDAGChain := genesisState.Chains[2]
	if name := spDAGChain.ChainName; name != "Simple DAG Payments" {
		panic("Wrong chain name")
	}
	genesisBytes := spDAGChain.GenesisData

	wallet := dagwallet.NewWallet(t.networkID, spDAGChain.ID(), config.AvaTxFee)

	codec := spdagvm.Codec{}
	tx, err := codec.UnmarshalTx(genesisBytes)
	t.log.AssertNoError(err)

	cb58 := formatting.CB58{}
	keyStr := genesis.Keys[config.Key]
	t.log.AssertNoError(cb58.FromString(keyStr))
	factory := crypto.FactorySECP256K1R{}
	skGen, err := factory.ToPrivateKey(cb58.Bytes)
	t.log.AssertNoError(err)
	sk := skGen.(*crypto.PrivateKeySECP256K1R)
	wallet.ImportKey(sk)

	for _, utxo := range tx.UTXOs() {
		wallet.AddUtxo(utxo)
	}

	go t.log.RecoverAndPanic(func() { t.IssueAvalanche(spDAGChain.ID(), wallet) })
}

func (t *tp) IssueAvalanche(chainID ids.ID, wallet dagwallet.Wallet) {
	t.log.Info("starting avalanche benchmark")
	pending := make(map[[32]byte]*spdagvm.Tx)
	canAdd := []*spdagvm.Tx{}
	numAccepted := 0

	t.decided <- ids.ID{}
	meter := timer.TimedMeter{Duration: time.Second}
	for d := range t.decided {
		if numAccepted%1000 == 0 {
			t.log.Info("TPS: %d", meter.Ticks())
		}
		if !d.IsZero() {
			meter.Tick()
			key := d.Key()
			if tx := pending[key]; tx != nil {
				canAdd = append(canAdd, tx)

				t.log.Debug("Finalized %s", d)
				delete(pending, key)
				numAccepted++
			}
		}

		for len(pending) < config.MaxOutstandingTxs && (wallet.Balance() > 0 || len(canAdd) > 0) {
			if wallet.Balance() == 0 {
				tx := canAdd[0]
				canAdd = canAdd[1:]

				for _, utxo := range tx.UTXOs() {
					wallet.AddUtxo(utxo)
				}
			}

			tx := wallet.Send(1, 0, wallet.GetAddress())
			t.log.AssertTrue(tx != nil, "Tx creation failed")

			it, err := t.build.IssueTx(chainID, tx.Bytes())
			t.log.AssertNoError(err)
			ds := it.DataStream()
			ba := salticidae.NewByteArrayMovedFromDataStream(ds, false)
			newMsg := salticidae.NewMsgMovedFromByteArray(networking.IssueTx, ba, false)

			t.conn.GetNet().SendMsg(newMsg, t.conn)

			ds.Free()
			ba.Free()
			newMsg.Free()

			pending[tx.ID().Key()] = tx
			t.log.Debug("Sent tx, pending = %d, accepted = %d", len(pending), numAccepted)
		}
	}
}

func (t *tp) benchmarkSnowman() {
	platformGenesisBytes, _ := genesis.Genesis(t.networkID)
	genesisState := &platformvm.Genesis{}
	err := platformvm.Codec.Unmarshal(platformGenesisBytes, genesisState)
	t.log.AssertNoError(err)
	t.log.AssertNoError(genesisState.Initialize())

	spchainChain := genesisState.Chains[3]
	if name := spchainChain.ChainName; name != "Simple Chain Payments" {
		panic("Wrong chain name")
	}
	genesisBytes := spchainChain.GenesisData

	wallet := chainwallet.NewWallet(t.networkID, spchainChain.ID())

	codec := spchainvm.Codec{}
	accounts, err := codec.UnmarshalGenesis(genesisBytes)
	t.log.AssertNoError(err)

	cb58 := formatting.CB58{}
	factory := crypto.FactorySECP256K1R{}
	for _, keyStr := range genesis.Keys {
		t.log.AssertNoError(cb58.FromString(keyStr))
		skGen, err := factory.ToPrivateKey(cb58.Bytes)
		t.log.AssertNoError(err)
		sk := skGen.(*crypto.PrivateKeySECP256K1R)
		wallet.ImportKey(sk)
	}

	for _, account := range accounts {
		wallet.AddAccount(account)
		break
	}

	wallet.GenerateTxs()

	go t.log.RecoverAndPanic(func() { t.IssueSnowman(spchainChain.ID(), wallet) })
}

func (t *tp) IssueSnowman(chainID ids.ID, wallet chainwallet.Wallet) {
	t.log.Debug("Issuing with %d", wallet.Balance())
	numAccepted := 0
	numPending := 0

	t.decided <- ids.ID{}

	meter := timer.TimedMeter{Duration: time.Second}
	for d := range t.decided {
		if numAccepted%1000 == 0 {
			t.log.Info("TPS: %d", meter.Ticks())
		}
		if !d.IsZero() {
			meter.Tick()
			t.log.Debug("Finalized %s", d)
			numAccepted++
			numPending--
		}

		for numPending < config.MaxOutstandingTxs && wallet.Balance() > 0 && wallet.TxsSent < chainwallet.MaxNumTxs {
			tx := wallet.NextTx()
			t.log.AssertTrue(tx != nil, "Tx creation failed")

			it, err := t.build.IssueTx(chainID, tx.Bytes())
			t.log.AssertNoError(err)
			ds := it.DataStream()
			ba := salticidae.NewByteArrayMovedFromDataStream(ds, false)
			newMsg := salticidae.NewMsgMovedFromByteArray(networking.IssueTx, ba, false)

			t.conn.GetNet().SendMsg(newMsg, t.conn)

			ds.Free()
			ba.Free()
			newMsg.Free()

			numPending++
			t.log.Debug("Sent tx, pending = %d, accepted = %d", numPending, numAccepted)
		}
		if wallet.TxsSent >= chainwallet.MaxNumTxs {
			fmt.Println("done with test")
			return
		}
	}

}
