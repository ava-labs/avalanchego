package evm

import (
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/common"
)

func getEThValidTxs(vm *VM, t *testing.T) []*types.Transaction {
	res := make([]*types.Transaction, 0)

	testAddr := common.HexToAddress("b94f5374fce5edbc8e2a8697c15331677e6ebf0b")

	emptyEip2718Tx := types.NewTx(&types.AccessListTx{
		ChainID:  big.NewInt(1),
		Nonce:    3,
		To:       &testAddr,
		Value:    big.NewInt(10),
		Gas:      25000,
		GasPrice: big.NewInt(1),
		Data:     common.FromHex("5544"),
	})

	signedEip2718Tx, _ := emptyEip2718Tx.WithSignature(
		types.NewEIP2930Signer(big.NewInt(1)),
		common.Hex2Bytes("c9519f4f2b30335884581971573fadf60c6204f59a911df35ee8a540456b266032f1e8e2c5dd761f9e4f88f41c8310aeaba26a8bfcdacfedfa12ec3862d3752101"),
	)

	res = append(res, signedEip2718Tx)
	return res
}

func TestMempool_LocalEthTxsAreGossiped(t *testing.T) {
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

	// add a tx to it
	ethTxs := getEThValidTxs(vm, t)
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

func TestMempool_LocalEthTxsAreNotGossipedBeforeActivation(t *testing.T) {
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

	// add a tx to it
	ethTxs := getEThValidTxs(vm, t)
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
