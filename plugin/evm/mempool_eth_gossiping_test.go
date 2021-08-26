package evm

import (
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

func getEThValidTxs() []*types.Transaction {
	res := make([]*types.Transaction, 0)

	testAddr := common.HexToAddress("b94f5374fce5edbc8e2a8697c15331677e6ebf0b")

	// generate two transactions
	for nonce := uint64(1); nonce <= 2; nonce++ {
		emptyEip2718Tx := types.NewTx(&types.AccessListTx{
			ChainID:  big.NewInt(1),
			Nonce:    nonce,
			To:       &testAddr,
			Value:    big.NewInt(10),
			Gas:      25000,
			GasPrice: big.NewInt(1),
			Data:     common.FromHex("5544"),
		})

		signedEip2718Tx_1, _ := emptyEip2718Tx.WithSignature(
			types.NewEIP2930Signer(big.NewInt(1)),
			common.Hex2Bytes("c9519f4f2b30335884581971573fadf60c6204f59a911df35ee8a540456b266032f1e8e2c5dd761f9e4f88f41c8310aeaba26a8bfcdacfedfa12ec3862d3752101"),
		)

		res = append(res, signedEip2718Tx_1)
	}

	return res
}

func TestMempool_EthTxsAreGossipedAfterActivation(t *testing.T) {
	// show that locally generated eth txes are gossiped
	// Note: channel through which coreth mempool push txes to vm is injected here
	// to ease up UT, which target only VM behavious in response to coreth mempool signals

	fakeTxSubmitChan := make(chan core.NewTxsEvent)
	_, vm, _, _, sender := GenesisVM(t, true, genesisJSONApricotPhase0, "", "", fakeTxSubmitChan)
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping

	gossipedBytes := make([]byte, 0)
	sender.CantSendAppGossip = true
	sender.SendAppGossipF = func(b []byte) error {
		gossipedBytes = b
		return nil
	}

	// create eth txes and notify VM about them
	ethTxs := getEThValidTxs()
	go func() {
		evt := core.NewTxsEvent{Txs: ethTxs}
		fakeTxSubmitChan <- evt
		close(fakeTxSubmitChan)
	}()

	time.Sleep(10 * time.Second) // TODO: cleanup this to avoid sleep

	if len(gossipedBytes) == 0 {
		t.Fatal("expected call to SendAppGossip not issued")
	}
}

func TestMempool_EthTxsAreNotGossipedBeforeActivation(t *testing.T) {
	// show that locally generated eth txes are gossiped
	// Note: channel through which coreth mempool push txes to vm is injected here
	// to ease up UT, which target only VM behavious in response to coreth mempool signals

	fakeTxSubmitChan := make(chan core.NewTxsEvent)
	_, vm, _, _, sender := GenesisVM(t, true, genesisJSONApricotPhase0, "", "", fakeTxSubmitChan)
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()
	vm.gossipActivationTime = timer.MaxTime // disable mempool gossiping

	gossipedBytes := make([]byte, 0)
	sender.CantSendAppGossip = true
	sender.SendAppGossipF = func(b []byte) error {
		gossipedBytes = b
		return nil
	}

	// create eth txes and notify VM about them
	ethTxs := getEThValidTxs()
	go func() {
		evt := core.NewTxsEvent{Txs: ethTxs}
		fakeTxSubmitChan <- evt
		close(fakeTxSubmitChan)
	}()

	time.Sleep(10 * time.Second) // TODO: cleanup this to avoid sleep

	if len(gossipedBytes) != 0 {
		t.Fatal("unexpected call to SendAppGossip issued")
	}
}

func TestMempool_EthTxs_EncodeDecodeBytes(t *testing.T) {
	vm := VM{
		codec: Codec,
	}

	ethTxs := getEThValidTxs()
	ethHashes := make([]common.Hash, len(ethTxs))
	for idx, ethTx := range ethTxs {
		ethHashes[idx] = ethTx.Hash()
	}
	bytes, err := vm.ethTxHashesToBytes(ethHashes)
	if err != nil {
		t.Fatal("Could no duly encode eth tx hashes")
	}

	hashList, err := vm.bytesToEthTxHashes(bytes)
	if err != nil {
		t.Fatal("Could no duly decode eth tx hashes")
	} else if len(hashList) != 2 {
		t.Fatal("decoded hashes list has unexpected length")
	}

	if ethTxs[0].Hash() != hashList[0] {
		t.Fatal("first decoded hash is unexpected")
	}
	if ethTxs[1].Hash() != hashList[1] {
		t.Fatal("second decoded hash is unexpected")
	}
}

func TestMempool_EthTxs_AppGossipHandling(t *testing.T) {
	// show that a coreth hashes discovered from gossip is requested to the same node
	// only if the coreth hash is unknown

	_, vm, _, _, sender := GenesisVM(t, true, genesisJSONApricotPhase0, "", "", nil)
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping

	isTxRequested := false
	nodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	IsRightNodeRequested := false
	sender.CantSendAppRequest = true
	sender.SendAppRequestF = func(nodes ids.ShortSet, reqID uint32, resp []byte) error {
		isTxRequested = true
		if nodes.Contains(nodeID) {
			IsRightNodeRequested = true
		}

		return nil
	}

	ethTxs := getEThValidTxs()
	ethHash := make([]common.Hash, 1)
	ethHash = append(ethHash, ethTxs[0].Hash())
	unknownEthTxsBytes, err := vm.ethTxHashesToBytes(ethHash)
	if err != nil {
		log.Trace("Could not parse ethTxIDs. Understand what to do")
	}

	// show that unknown coreth hashes is requested
	if err := vm.AppGossip(nodeID, unknownEthTxsBytes); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if !isTxRequested {
		t.Fatal("unknown txID should have been requested")
	}
	if !IsRightNodeRequested {
		t.Fatal("unknown txID should have been requested to a different node")
	}

	// // show that requested bytes can be duly decoded
	// if err := vm.AppRequest(nodeID, vm.IssueID(), requestedBytes); err != nil {
	// 	t.Fatal("requested bytes following gossiping cannot be decoded")
	// }

	// TODO: find a way to add transactions to the pool
	// // show that known coreth hashes is not requested
	// isTxRequested = false
	// if err := vm.chain.GetTxPool().AddLocal(ethTxs[1]); err != nil {
	// 	t.Fatal("could not add tx to mempool")
	// }

	// ethHash[0] = ethTxs[1].Hash()
	// knownEthTxsBytes, err := vm.ethTxHashesToBytes(ethHash)
	// if err != nil {
	// 	log.Trace("Could not parse ethTxIDs. Understand what to do")
	// }
	// if err := vm.AppGossip(nodeID, knownEthTxsBytes); err != nil {
	// 	t.Fatal("error in reception of gossiped tx")
	// }
	// if isTxRequested {
	// 	t.Fatal("known txID should not be requested")
	// }
}
