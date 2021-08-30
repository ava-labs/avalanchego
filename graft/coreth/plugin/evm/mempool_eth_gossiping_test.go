package evm

import (
	"crypto/ecdsa"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

func fundAddressByGenesis(addr common.Address) (string, error) {
	balance := big.NewInt(0xffffffffffffff)
	genesis := &core.Genesis{
		Difficulty: common.Big0,
		GasLimit:   uint64(5000000),
	}
	funds := make(map[common.Address]core.GenesisAccount)
	funds[addr] = core.GenesisAccount{
		Balance: balance,
	}
	genesis.Alloc = funds

	genesis.Config = &params.ChainConfig{
		ChainID: params.AvalancheLocalChainID,
	}

	bytes, err := json.Marshal(genesis)
	return string(bytes), err
}

func getEThValidTxs(key *ecdsa.PrivateKey) []*types.Transaction {
	res := make([]*types.Transaction, 0)

	nonce := uint64(0)
	to := common.Address{}
	amount := big.NewInt(10000)
	gaslimit := uint64(100000)
	gasprice := big.NewInt(1)

	tx_1, _ := types.SignTx(
		types.NewTransaction(nonce,
			to,
			amount,
			gaslimit,
			gasprice,
			nil),
		types.HomesteadSigner{}, key)
	res = append(res, tx_1)

	nonce++
	tx_2, _ := types.SignTx(
		types.NewTransaction(
			nonce,
			to,
			amount,
			gaslimit,
			gasprice,
			nil),
		types.HomesteadSigner{}, key)
	res = append(res, tx_2)
	return res
}

func TestMempool_EthTxs_AddedTxesGossipedAfterActivation(t *testing.T) {
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
	key, _ := crypto.GenerateKey()
	ethTxs := getEThValidTxs(key)
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

func TestMempool_EthTxs_AddedTxesNotGossipedBeforeActivation(t *testing.T) {
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
	key, _ := crypto.GenerateKey()
	ethTxs := getEThValidTxs(key)
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

	key, _ := crypto.GenerateKey()
	ethTxs := getEThValidTxs(key)
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

	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)

	cfgJson, err := fundAddressByGenesis(addr)
	if err != nil {
		t.Fatal("could not format genesis")
	}

	_, vm, _, _, sender := GenesisVM(t, true, cfgJson, "", "", nil)
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	vm.chain.GetTxPool().SetGasPrice(common.Big1)
	vm.chain.GetTxPool().SetMinFee(common.Big0)

	isTxRequested := false
	isRightNodeRequested := false
	nodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	sender.CantSendAppRequest = true
	sender.SendAppRequestF = func(nodes ids.ShortSet, reqID uint32, resp []byte) error {
		isTxRequested = true
		if nodes.Contains(nodeID) {
			isRightNodeRequested = true
		}

		return nil
	}

	// prepare a couple of txes
	ethTx := getEThValidTxs(key)[0]

	// show that unknown coreth hashes is requested
	unknownEthTxsBytes, err := vm.ethTxHashesToBytes([]common.Hash{ethTx.Hash()})
	if err != nil {
		log.Trace("Could not parse ethTxIDs. Understand what to do")
	}

	if err := vm.AppGossip(nodeID, unknownEthTxsBytes); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if !isTxRequested {
		t.Fatal("unknown txID should have been requested")
	}
	if !isRightNodeRequested {
		t.Fatal("unknown txID should have been requested to a different node")
	}

	// show that known coreth tx is not requested
	isTxRequested = false
	if err := vm.chain.GetTxPool().AddLocal(ethTx); err != nil {
		t.Fatal("could not add tx to mempool")
	}

	knownEthTxsBytes, err := vm.ethTxHashesToBytes([]common.Hash{ethTx.Hash()})
	if err != nil {
		log.Trace("Could not parse ethTxIDs. Understand what to do")
	}
	if err := vm.AppGossip(nodeID, knownEthTxsBytes); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if isTxRequested {
		t.Fatal("known txID should not be requested")
	}
}

func TestMempool_EthTxs_AppResponseHandling(t *testing.T) {
	// show that a tx discovered by a GossipResponse is re-gossiped
	// only if duly added to mempool

	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)

	cfgJson, err := fundAddressByGenesis(addr)
	if err != nil {
		t.Fatal("could not format genesis")
	}

	_, vm, _, _, sender := GenesisVM(t, true, cfgJson, "", "", nil)
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	vm.chain.GetTxPool().SetGasPrice(common.Big1)
	vm.chain.GetTxPool().SetMinFee(common.Big0)

	isTxReGossiped := false
	sender.CantSendAppGossip = true
	sender.SendAppGossipF = func([]byte) error {
		isTxReGossiped = true
		return nil
	}

	// prepare a couple of txes
	ethTxs := getEThValidTxs(key)
	responseBytes, err := rlp.EncodeToBytes(ethTxs)
	if err != nil {
		t.Fatal("could not encode coreth txs")
	}

	// responses with unknown requestID are rejected
	nodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	reqID := vm.IssueID()

	unknownReqID := reqID + 1
	if err := vm.AppResponse(nodeID, unknownReqID, responseBytes); err != nil {
		t.Fatal("responses with unknown requestID should be dropped")
	}

	if vm.chain.GetTxPool().Has(ethTxs[0].Hash()) {
		t.Fatal("responses with unknown requestID should not affect mempool")
	}
	if vm.chain.GetTxPool().Has(ethTxs[1].Hash()) {
		t.Fatal("responses with unknown requestID should not affect mempool")
	}
	if isTxReGossiped {
		t.Fatal("responses with unknown requestID should not result in gossiping")
	}

	// received tx and check it is accepted and re-gossiped
	if err := vm.AppResponse(nodeID, reqID, responseBytes); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	time.Sleep(10 * time.Second)
	if !vm.chain.GetTxPool().Has(ethTxs[0].Hash()) {
		t.Fatal("responses with unknown requestID should not affect mempool")
	}
	if !vm.chain.GetTxPool().Has(ethTxs[1].Hash()) {
		t.Fatal("responses with unknown requestID should not affect mempool")
	}
	if !isTxReGossiped {
		t.Fatal("tx accepted in mempool should have been re-gossiped")
	}
}

func TestMempool_EthTxs_AppRequestHandling(t *testing.T) {
	// show that a node answer to request with response
	// only if it has the requested tx

	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)

	cfgJson, err := fundAddressByGenesis(addr)
	if err != nil {
		t.Fatal("could not format genesis")
	}

	_, vm, _, _, sender := GenesisVM(t, true, cfgJson, "", "", nil)
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	isResponseIssued := false
	var respondedBytes []byte
	sender.CantSendAppResponse = true
	sender.SendAppResponseF = func(nodeID ids.ShortID, reqID uint32, resp []byte) error {
		isResponseIssued = true
		respondedBytes = resp
		return nil
	}

	// prepare a coreth tx
	vm.chain.GetTxPool().SetGasPrice(common.Big1)
	vm.chain.GetTxPool().SetMinFee(common.Big0)
	ethTx := getEThValidTxs(key)[0]
	ethHashBytes, err := vm.ethTxHashesToBytes([]common.Hash{ethTx.Hash()})
	if err != nil {
		t.Fatal("Could no duly encode eth tx hashes")
	}

	// show that there is no response if tx is unknown
	nodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	if err := vm.AppRequest(nodeID, vm.IssueID(), ethHashBytes); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if isResponseIssued {
		t.Fatal("there should be no response with unknown tx")
	}

	// show that there is response if tx is known
	if err := vm.chain.GetTxPool().AddLocal(ethTx); err != nil {
		t.Fatal("could not add tx to mempool")
	}

	if err := vm.AppRequest(nodeID, vm.IssueID(), ethHashBytes); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if !isResponseIssued {
		t.Fatal("there should be a response with known tx")
	}

	// show that responded bytes can be duly decoded
	if err := vm.AppResponse(nodeID, vm.IssueID(), respondedBytes); err != nil {
		t.Fatal("bytes sent in response of AppRequest cannot be decoded")
	}
}
