// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

// import (
// 	"crypto/ecdsa"
// 	"encoding/json"
// 	"math/big"
// 	"strings"
// 	"sync"
// 	"testing"
// 	"time"
//
// 	"github.com/ava-labs/avalanchego/ids"
//
// 	"github.com/ethereum/go-ethereum/common"
// 	"github.com/ethereum/go-ethereum/crypto"
// 	"github.com/ethereum/go-ethereum/rlp"
//
// 	"github.com/stretchr/testify/assert"
//
// 	"github.com/ava-labs/coreth/core"
// 	"github.com/ava-labs/coreth/core/types"
// 	"github.com/ava-labs/coreth/params"
// 	"github.com/ava-labs/coreth/plugin/evm/message"
// )
//
// func fundAddressByGenesis(addr common.Address) (string, error) {
// 	balance := big.NewInt(0xffffffffffffff)
// 	genesis := &core.Genesis{
// 		Difficulty: common.Big0,
// 		GasLimit:   uint64(5000000),
// 	}
// 	funds := make(map[common.Address]core.GenesisAccount)
// 	funds[addr] = core.GenesisAccount{
// 		Balance: balance,
// 	}
// 	genesis.Alloc = funds
//
// 	genesis.Config = &params.ChainConfig{
// 		ChainID:                     params.AvalancheLocalChainID,
// 		ApricotPhase1BlockTimestamp: big.NewInt(0),
// 		ApricotPhase2BlockTimestamp: big.NewInt(0),
// 		ApricotPhase3BlockTimestamp: big.NewInt(0),
// 		ApricotPhase4BlockTimestamp: big.NewInt(0),
// 	}
//
// 	bytes, err := json.Marshal(genesis)
// 	return string(bytes), err
// }
//
// func getValidEthTxs(key *ecdsa.PrivateKey, count int) []*types.Transaction {
// 	res := make([]*types.Transaction, count)
//
// 	to := common.Address{}
// 	amount := big.NewInt(10000)
// 	gaslimit := uint64(100000)
// 	gasprice := big.NewInt(1)
//
// 	for i := 0; i < count; i++ {
// 		tx, _ := types.SignTx(
// 			types.NewTransaction(
// 				uint64(i),
// 				to,
// 				amount,
// 				gaslimit,
// 				gasprice,
// 				[]byte(strings.Repeat("aaaaaaaaaa", 100))),
// 			types.HomesteadSigner{}, key)
// 		res[i] = tx
// 	}
// 	return res
// }
//
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
// 	vm.chain.GetTxPool().SetGasPrice(common.Big1)
// 	vm.chain.GetTxPool().SetMinFee(common.Big0)
//
// 	// create eth txes
// 	ethTxs := getValidEthTxs(key, 3)
//
// 	var wg sync.WaitGroup
// 	wg.Add(3)
// 	sender.CantSendAppGossip = false
// 	signal1 := make(chan struct{})
// 	signal2 := make(chan struct{})
// 	seen := 0
// 	sender.SendAppGossipF = func(gossipedBytes []byte) error {
// 		if seen == 0 {
// 			notifyMsgIntf, err := message.Parse(gossipedBytes)
// 			assert.NoError(err)
//
// 			requestMsg, ok := notifyMsgIntf.(*message.EthTxsNotify)
// 			assert.True(ok)
// 			assert.Empty(requestMsg.Txs)
// 			assert.NotEmpty(requestMsg.TxsBytes)
//
// 			txs := make([]*types.Transaction, 0)
// 			assert.NoError(rlp.DecodeBytes(requestMsg.TxsBytes, &txs))
// 			assert.Len(txs, 2)
// 			assert.EqualValues(
// 				[]common.Hash{ethTxs[0].Hash(), ethTxs[1].Hash()},
// 				[]common.Hash{txs[0].Hash(), txs[1].Hash()},
// 			)
// 			seen++
// 			close(signal1)
// 		} else if seen == 1 {
// 			notifyMsgIntf, err := message.Parse(gossipedBytes)
// 			assert.NoError(err)
//
// 			requestMsg, ok := notifyMsgIntf.(*message.EthTxsNotify)
// 			assert.True(ok)
// 			assert.Empty(requestMsg.TxsBytes)
// 			assert.Len(requestMsg.Txs, 2)
//
// 			assert.EqualValues(
// 				[]common.Hash{ethTxs[0].Hash(), ethTxs[1].Hash()},
// 				[]common.Hash{requestMsg.Txs[0].Hash, requestMsg.Txs[1].Hash},
// 			)
// 			seen++
// 			close(signal2)
// 		} else {
// 			notifyMsgIntf, err := message.Parse(gossipedBytes)
// 			assert.NoError(err)
//
// 			requestMsg, ok := notifyMsgIntf.(*message.EthTxsNotify)
// 			assert.True(ok)
// 			assert.Empty(requestMsg.Txs)
// 			assert.NotEmpty(requestMsg.TxsBytes)
// 			txs := make([]*types.Transaction, 0)
// 			assert.NoError(rlp.DecodeBytes(requestMsg.TxsBytes, &txs))
// 			assert.Len(txs, 1)
// 			assert.Equal(ethTxs[2].Hash(), txs[0].Hash())
// 		}
// 		wg.Done()
// 		return nil
// 	}
//
// 	// Notify VM about eth txs
// 	errs := vm.chain.GetTxPool().AddRemotesSync(ethTxs[:2])
// 	for _, err := range errs {
// 		assert.NoError(err, "failed adding coreth tx to mempool")
// 	}
//
// 	// Gossip txs again (should gossip hashes)
// 	<-signal1 // wait until reorg processed
// 	assert.NoError(vm.network.GossipEthTxs(ethTxs[:2]))
//
// 	<-signal2 // wait until second gossip processed
// 	errs = vm.chain.GetTxPool().AddRemotesSync(ethTxs)
// 	assert.Contains(errs[0].Error(), "already known")
// 	assert.Contains(errs[1].Error(), "already known")
// 	assert.NoError(errs[2], "failed adding coreth tx to mempool")
//
// 	attemptAwait(t, &wg, 5*time.Second)
// }
//
// // show that locally issued eth txs are chunked correctly
// func TestMempoolEthTxsAddedTxsGossipedAfterActivationChunking(t *testing.T) {
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
// 	vm.chain.GetTxPool().SetGasPrice(common.Big1)
// 	vm.chain.GetTxPool().SetMinFee(common.Big0)
//
// 	// create eth txes
// 	ethTxs := getValidEthTxs(key, 100)
//
// 	var wg sync.WaitGroup
// 	wg.Add(2)
// 	sender.CantSendAppGossip = false
// 	seen := 0
// 	signal := make(chan struct{})
// 	sender.SendAppGossipF = func(gossipedBytes []byte) error {
// 		if seen == 0 {
// 			notifyMsgIntf, err := message.Parse(gossipedBytes)
// 			assert.NoError(err)
//
// 			requestMsg, ok := notifyMsgIntf.(*message.EthTxsNotify)
// 			assert.True(ok)
// 			assert.Empty(requestMsg.Txs)
// 			assert.NotEmpty(requestMsg.TxsBytes)
//
// 			txs := make([]*types.Transaction, 0)
// 			assert.NoError(rlp.DecodeBytes(requestMsg.TxsBytes, &txs))
// 			assert.Len(txs, 59)
// 			for i, tx := range txs {
// 				assert.Equal(ethTxs[i].Hash(), tx.Hash())
// 			}
// 			seen++
// 		} else {
// 			notifyMsgIntf, err := message.Parse(gossipedBytes)
// 			assert.NoError(err)
//
// 			requestMsg, ok := notifyMsgIntf.(*message.EthTxsNotify)
// 			assert.True(ok)
// 			assert.Empty(requestMsg.Txs)
// 			assert.NotEmpty(requestMsg.TxsBytes)
//
// 			txs := make([]*types.Transaction, 0)
// 			assert.NoError(rlp.DecodeBytes(requestMsg.TxsBytes, &txs))
// 			assert.Len(txs, 41)
// 			for i, tx := range txs {
// 				assert.Equal(ethTxs[i+59].Hash(), tx.Hash())
// 			}
// 			close(signal)
// 		}
// 		wg.Done()
// 		return nil
// 	}
//
// 	// Notify VM about eth txs
// 	errs := vm.chain.GetTxPool().AddRemotesSync(ethTxs)
// 	for _, err := range errs {
// 		assert.NoError(err, "failed adding coreth tx to mempool")
// 	}
//
// 	// Test chunking of hashes
// 	<-signal
// 	seen = 0
// 	wg.Add(4)
// 	sender.SendAppGossipF = func(gossipedBytes []byte) error {
// 		notifyMsgIntf, err := message.Parse(gossipedBytes)
// 		assert.NoError(err)
//
// 		requestMsg, ok := notifyMsgIntf.(*message.EthTxsNotify)
// 		assert.True(ok)
// 		assert.Empty(requestMsg.TxsBytes)
// 		var offset int
// 		if seen < 3 {
// 			assert.Len(requestMsg.Txs, 32)
// 			offset = 32 * seen
// 		} else {
// 			assert.Len(requestMsg.Txs, 4)
// 			offset = 96
// 		}
// 		for i, tx := range requestMsg.Txs {
// 			assert.Equal(ethTxs[i+offset].Hash(), tx.Hash)
// 		}
// 		seen++
// 		wg.Done()
// 		return nil
// 	}
// 	assert.NoError(vm.GossipEthTxs(ethTxs))
//
// 	attemptAwait(t, &wg, 5*time.Second)
// }
//
// // show that a geth tx discovered from gossip is requested to the same node that
// // gossiped it
// func TestMempoolEthTxsAppGossipHandling(t *testing.T) {
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
// 	vm.chain.GetTxPool().SetGasPrice(common.Big1)
// 	vm.chain.GetTxPool().SetMinFee(common.Big0)
//
// 	nodeID := ids.GenerateTestShortID()
//
// 	var (
// 		txRequested         bool
// 		txRequestedFromNode bool
// 	)
// 	sender.CantSendAppGossip = false
// 	sender.SendAppRequestF = func(nodes ids.ShortSet, _ uint32, _ []byte) error {
// 		txRequested = true
// 		if nodes.Contains(nodeID) {
// 			txRequestedFromNode = true
// 		}
// 		return nil
// 	}
//
// 	// prepare a tx
// 	tx := getValidEthTxs(key, 2)[0]
// 	txSender, err := types.Sender(types.LatestSigner(vm.chainConfig), tx)
// 	assert.NoError(err, "could not retrieve sender")
//
// 	// show that unknown coreth hashes is requested
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
// 	err = vm.AppGossip(nodeID, msgBytes)
// 	assert.NoError(err)
// 	assert.True(txRequested, "unknown txID should have been requested")
// 	assert.True(txRequestedFromNode, "unknown txID should have been requested to the same node")
//
// 	// show that known coreth tx is not requested
// 	txRequested = false
// 	err = vm.chain.GetTxPool().AddLocal(tx)
// 	assert.NoError(err)
//
// 	err = vm.AppGossip(nodeID, msgBytes)
// 	assert.NoError(err)
// 	assert.False(txRequested, "known txID should not be requested")
// }
//
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
// 	vm.chain.GetTxPool().SetGasPrice(common.Big1)
// 	vm.chain.GetTxPool().SetMinFee(common.Big0)
//
// 	var (
// 		txGossiped bool
// 		wg         sync.WaitGroup
// 	)
//
// 	wg.Add(1)
// 	sender.CantSendAppGossip = false
// 	sender.SendAppGossipF = func([]byte) error {
// 		txGossiped = true
// 		wg.Done()
// 		return nil
// 	}
//
// 	// prepare a couple of txes
// 	txs := getValidEthTxs(key, 2)
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
// 	attemptAwait(t, &wg, 5*time.Second)
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
// 	vm.chain.GetTxPool().SetGasPrice(common.Big1)
// 	vm.chain.GetTxPool().SetMinFee(common.Big0)
//
// 	var responded bool
// 	sender.CantSendAppGossip = false
// 	sender.SendAppResponseF = func(nodeID ids.ShortID, reqID uint32, resp []byte) error {
// 		responded = true
// 		return nil
// 	}
//
// 	// prepare a coreth tx
// 	tx := getValidEthTxs(key, 2)[0]
//
// 	txSender, err := types.Sender(types.LatestSigner(vm.chainConfig), tx)
// 	if err != nil {
// 		t.Fatal("could not retrieve tx address")
// 	}
// 	msg := message.EthTxsRequest{
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
