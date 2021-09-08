// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"crypto/ecdsa"
	"encoding/json"
	// "fmt"
	"math/big"
	// "sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	// "github.com/ethereum/go-ethereum/rlp"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/message"
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

func getValidEthTxs(key *ecdsa.PrivateKey) []*types.Transaction {
	res := make([]*types.Transaction, 0)

	nonce := uint64(0)
	to := common.Address{}
	amount := big.NewInt(10000)
	gaslimit := uint64(100000)
	gasprice := big.NewInt(1)

	tx1, _ := types.SignTx(
		types.NewTransaction(nonce,
			to,
			amount,
			gaslimit,
			gasprice,
			nil),
		types.HomesteadSigner{}, key)
	res = append(res, tx1)

	nonce++
	tx2, _ := types.SignTx(
		types.NewTransaction(
			nonce,
			to,
			amount,
			gaslimit,
			gasprice,
			nil,
		),
		types.HomesteadSigner{}, key)
	res = append(res, tx2)
	return res
}

// // show that locally issued eth txs are gossiped
// // Note: channel through which coreth mempool push txs to vm is injected here
// // to ease up UT, which target only VM behaviors in response to coreth mempool
// // signals
// func TestMempoolEthTxsAddedTxsGossipedAfterActivation(t *testing.T) {
// 	assert := assert.New(t)
//
// 	key, err := crypto.GenerateKey()
// 	assert.NoError(err)
//
// 	addr := crypto.PubkeyToAddress(key.PublicKey)
//
// 	cfgJson, err := fundAddressByGenesis(addr)
// 	assert.NoError(err)
//
// 	_, vm, _, _, sender := GenesisVM(t, true, cfgJson, "", "")
// 	defer func() {
// 		err := vm.Shutdown()
// 		assert.NoError(err)
// 	}()
// 	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
// 	vm.chain.GetTxPool().SetGasPrice(common.Big1)
// 	vm.chain.GetTxPool().SetMinFee(common.Big0)
//
// 	var wg sync.WaitGroup
//
// 	wg.Add(1)
// 	sender.SendAppGossipF = func([]byte) error {
// 		wg.Done()
// 		return nil
// 	}
//
// 	// create eth txes and notify VM about them
// 	ethTxs := getValidEthTxs(key)
// 	errs := vm.chain.GetTxPool().AddRemotesSync(ethTxs)
// 	for _, err := range errs {
// 		assert.NoError(err, "failed adding coreth tx to mempool")
// 	}
//
// 	fmt.Println("waiting for tx")
// 	wg.Wait()
// }

// show that a geth tx discovered from gossip is requested to the same node that
// gossiped it
func TestMempoolEthTxsAppGossipHandling(t *testing.T) {
	assert := assert.New(t)

	key, err := crypto.GenerateKey()
	assert.NoError(err)

	addr := crypto.PubkeyToAddress(key.PublicKey)

	cfgJson, err := fundAddressByGenesis(addr)
	assert.NoError(err)

	_, vm, _, _, sender := GenesisVM(t, true, cfgJson, "", "")
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	vm.chain.GetTxPool().SetGasPrice(common.Big1)
	vm.chain.GetTxPool().SetMinFee(common.Big0)

	nodeID := ids.GenerateTestShortID()

	var (
		txRequested         bool
		txRequestedFromNode bool
	)
	sender.SendAppRequestF = func(nodes ids.ShortSet, _ uint32, _ []byte) error {
		txRequested = true
		if nodes.Contains(nodeID) {
			txRequestedFromNode = true
		}
		return nil
	}

	// prepare a tx
	tx := getValidEthTxs(key)[0]
	txSender, err := types.Sender(types.LatestSigner(vm.chainConfig), tx)
	assert.NoError(err, "could not retrieve sender")

	// show that unknown coreth hashes is requested
	msg := message.EthTxsNotify{
		Txs: []message.EthTxNotify{{
			Hash:   tx.Hash(),
			Sender: txSender,
			Nonce:  tx.Nonce(),
		}},
	}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err)

	err = vm.AppGossip(nodeID, msgBytes)
	assert.NoError(err)
	assert.True(txRequested, "unknown txID should have been requested")
	assert.True(txRequestedFromNode, "unknown txID should have been requested to the same node")

	// show that known coreth tx is not requested
	txRequested = false
	err = vm.chain.GetTxPool().AddLocal(tx)
	assert.NoError(err)

	err = vm.AppGossip(nodeID, msgBytes)
	assert.NoError(err)
	assert.False(txRequested, "known txID should not be requested")
}

// // show that a tx discovered by a GossipResponse is re-gossiped if it is added
// // to the mempool
// func TestMempoolEthTxsAppResponseHandling(t *testing.T) {
// 	assert := assert.New(t)
//
// 	key, err := crypto.GenerateKey()
// 	assert.NoError(err)
//
// 	addr := crypto.PubkeyToAddress(key.PublicKey)
//
// 	cfgJson, err := fundAddressByGenesis(addr)
// 	assert.NoError(err)
//
// 	_, vm, _, _, sender := GenesisVM(t, true, cfgJson, "", "")
// 	defer func() {
// 		err := vm.Shutdown()
// 		assert.NoError(err)
// 	}()
// 	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
// 	vm.chain.GetTxPool().SetGasPrice(common.Big1)
// 	vm.chain.GetTxPool().SetMinFee(common.Big0)
//
// 	var (
// 		txGossiped bool
// 		wg         sync.WaitGroup
// 	)
//
// 	wg.Add(1)
// 	sender.SendAppGossipF = func([]byte) error {
// 		txGossiped = true
// 		wg.Done()
// 		return nil
// 	}
//
// 	// prepare a couple of txes
// 	txs := getValidEthTxs(key)
//
// 	txBytes, err := rlp.EncodeToBytes(txs)
// 	assert.NoError(err)
//
// 	msg := message.EthTxs{
// 		TxsBytes: txBytes,
// 	}
// 	msgBytes, err := message.Build(&msg)
// 	assert.NoError(err)
//
// 	// responses with unknown requestID are rejected
// 	nodeID := ids.GenerateTestShortID()
// 	err = vm.AppResponse(nodeID, 0, msgBytes)
// 	assert.NoError(err)
//
// 	pool := vm.chain.GetTxPool()
//
// 	has := pool.Has(txs[0].Hash())
// 	assert.False(has, "responses with unknown requestID should not affect mempool")
//
// 	has = pool.Has(txs[1].Hash())
// 	assert.False(has, "responses with unknown requestID should not affect mempool")
//
// 	assert.False(txGossiped, "responses with unknown requestID should not result in gossiping")
//
// 	// received tx and check it is accepted and re-gossiped
// 	reqs := map[common.Hash]struct{}{
// 		txs[0].Hash(): {},
// 	}
// 	vm.requestsEthContent[0] = reqs
// 	err = vm.AppResponse(nodeID, 0, msgBytes)
// 	assert.NoError(err)
//
// 	fmt.Println("waiting for tx")
// 	wg.Wait()
//
// 	has = pool.Has(txs[0].Hash())
// 	assert.True(has, "responses with known requestID should be added to the mempool")
//
// 	has = pool.Has(txs[1].Hash())
// 	assert.False(has, "responses with unknown hash should be added to the mempool")
//
// 	assert.True(txGossiped, "txs added to the mempool should have been re-gossiped")
// }
//
// // show that a node answers to a request with a response if it has the requested
// // tx
// func TestMempoolEthTxsAppRequestHandling(t *testing.T) {
// 	assert := assert.New(t)
//
// 	key, err := crypto.GenerateKey()
// 	assert.NoError(err)
//
// 	addr := crypto.PubkeyToAddress(key.PublicKey)
//
// 	cfgJson, err := fundAddressByGenesis(addr)
// 	assert.NoError(err)
//
// 	_, vm, _, _, sender := GenesisVM(t, true, cfgJson, "", "")
// 	defer func() {
// 		err := vm.Shutdown()
// 		assert.NoError(err)
// 	}()
// 	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
// 	vm.chain.GetTxPool().SetGasPrice(common.Big1)
// 	vm.chain.GetTxPool().SetMinFee(common.Big0)
//
// 	var responded bool
// 	sender.SendAppResponseF = func(nodeID ids.ShortID, reqID uint32, resp []byte) error {
// 		responded = true
// 		return nil
// 	}
//
// 	// prepare a coreth tx
// 	tx := getValidEthTxs(key)[0]
//
// 	txSender, err := types.Sender(types.LatestSigner(vm.chainConfig), tx)
// 	if err != nil {
// 		t.Fatal("could not retrieve tx address")
// 	}
// 	msg := message.EthTxsNotify{
// 		Txs: []message.EthTxNotify{{
// 			Hash:   tx.Hash(),
// 			Sender: txSender,
// 			Nonce:  tx.Nonce(),
// 		}},
// 	}
// 	msgBytes, err := message.Build(&msg)
// 	assert.NoError(err)
//
// 	// show that there is no response if tx is unknown
// 	nodeID := ids.GenerateTestShortID()
// 	err = vm.AppRequest(nodeID, 0, msgBytes)
// 	assert.NoError(err)
// 	assert.False(responded, "there should be no response with an unknown tx")
//
// 	// show that there is response if tx is known
// 	err = vm.chain.GetTxPool().AddLocal(tx)
// 	assert.NoError(err)
//
// 	err = vm.AppRequest(nodeID, 0, msgBytes)
// 	assert.NoError(err)
// 	assert.True(responded, "there should be a response with a known tx")
// }
