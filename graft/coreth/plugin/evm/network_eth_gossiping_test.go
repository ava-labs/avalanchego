// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"crypto/ecdsa"
	"encoding/json"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/message"
)

func fundAddressByGenesis(addrs []common.Address) (string, error) {
	balance := big.NewInt(0xffffffffffffff)
	genesis := &core.Genesis{
		Difficulty: common.Big0,
		GasLimit:   uint64(5000000),
	}
	funds := make(map[common.Address]core.GenesisAccount)
	for _, addr := range addrs {
		funds[addr] = core.GenesisAccount{
			Balance: balance,
		}
	}
	genesis.Alloc = funds

	genesis.Config = &params.ChainConfig{
		ChainID:                     params.AvalancheLocalChainID,
		ApricotPhase1BlockTimestamp: big.NewInt(0),
		ApricotPhase2BlockTimestamp: big.NewInt(0),
		ApricotPhase3BlockTimestamp: big.NewInt(0),
		ApricotPhase4BlockTimestamp: big.NewInt(0),
	}

	bytes, err := json.Marshal(genesis)
	return string(bytes), err
}

func getValidEthTxs(key *ecdsa.PrivateKey, count int, gasprice *big.Int) []*types.Transaction {
	res := make([]*types.Transaction, count)

	to := common.Address{}
	amount := big.NewInt(10000)
	gaslimit := uint64(100000)

	for i := 0; i < count; i++ {
		tx, _ := types.SignTx(
			types.NewTransaction(
				uint64(i),
				to,
				amount,
				gaslimit,
				gasprice,
				[]byte(strings.Repeat("aaaaaaaaaa", 100))),
			types.HomesteadSigner{}, key)
		res[i] = tx
	}
	return res
}

// show that locally issued eth txs are gossiped
// Note: channel through which coreth mempool push txs to vm is injected here
// to ease up UT, which target only VM behaviors in response to coreth mempool
// signals
func TestMempoolEthTxsAddedTxsGossipedAfterActivation(t *testing.T) {
	assert := assert.New(t)

	key, err := crypto.GenerateKey()
	assert.NoError(err)

	addr := crypto.PubkeyToAddress(key.PublicKey)

	cfgJson, err := fundAddressByGenesis([]common.Address{addr})
	assert.NoError(err)

	_, vm, _, _, sender := GenesisVM(t, true, cfgJson, "", "")
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
	}()
	vm.chain.GetTxPool().SetGasPrice(common.Big1)
	vm.chain.GetTxPool().SetMinFee(common.Big0)

	// create eth txes
	ethTxs := getValidEthTxs(key, 3, common.Big1)

	var wg sync.WaitGroup
	wg.Add(2)
	sender.CantSendAppGossip = false
	signal1 := make(chan struct{})
	seen := 0
	sender.SendAppGossipF = func(gossipedBytes []byte) error {
		if seen == 0 {
			notifyMsgIntf, err := message.Parse(gossipedBytes)
			assert.NoError(err)

			requestMsg, ok := notifyMsgIntf.(*message.EthTxs)
			assert.True(ok)
			assert.NotEmpty(requestMsg.Txs)

			txs := make([]*types.Transaction, 0)
			assert.NoError(rlp.DecodeBytes(requestMsg.Txs, &txs))
			assert.Len(txs, 2)
			assert.ElementsMatch(
				[]common.Hash{ethTxs[0].Hash(), ethTxs[1].Hash()},
				[]common.Hash{txs[0].Hash(), txs[1].Hash()},
			)
			seen++
			close(signal1)
		} else if seen == 1 {
			notifyMsgIntf, err := message.Parse(gossipedBytes)
			assert.NoError(err)

			requestMsg, ok := notifyMsgIntf.(*message.EthTxs)
			assert.True(ok)
			assert.NotEmpty(requestMsg.Txs)

			txs := make([]*types.Transaction, 0)
			assert.NoError(rlp.DecodeBytes(requestMsg.Txs, &txs))
			assert.Len(txs, 1)
			assert.Equal(ethTxs[2].Hash(), txs[0].Hash())

			seen++
		} else {
			t.Fatal("should not be seen 3 times")
		}
		wg.Done()
		return nil
	}

	// Notify VM about eth txs
	errs := vm.chain.GetTxPool().AddRemotesSync(ethTxs[:2])
	for _, err := range errs {
		assert.NoError(err, "failed adding coreth tx to mempool")
	}

	// Gossip txs again (shouldn't gossip hashes)
	<-signal1 // wait until reorg processed
	assert.NoError(vm.network.GossipEthTxs(ethTxs[:2]))

	errs = vm.chain.GetTxPool().AddRemotesSync(ethTxs)
	assert.Contains(errs[0].Error(), "already known")
	assert.Contains(errs[1].Error(), "already known")
	assert.NoError(errs[2], "failed adding coreth tx to mempool")

	attemptAwait(t, &wg, 5*time.Second)
}

// show that locally issued eth txs are chunked correctly
func TestMempoolEthTxsAddedTxsGossipedAfterActivationChunking(t *testing.T) {
	assert := assert.New(t)

	key, err := crypto.GenerateKey()
	assert.NoError(err)

	addr := crypto.PubkeyToAddress(key.PublicKey)

	cfgJson, err := fundAddressByGenesis([]common.Address{addr})
	assert.NoError(err)

	_, vm, _, _, sender := GenesisVM(t, true, cfgJson, "", "")
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
	}()
	vm.chain.GetTxPool().SetGasPrice(common.Big1)
	vm.chain.GetTxPool().SetMinFee(common.Big0)

	// create eth txes
	ethTxs := getValidEthTxs(key, 100, common.Big1)

	var wg sync.WaitGroup
	wg.Add(2)
	sender.CantSendAppGossip = false
	seen := map[common.Hash]struct{}{}
	sender.SendAppGossipF = func(gossipedBytes []byte) error {
		notifyMsgIntf, err := message.Parse(gossipedBytes)
		assert.NoError(err)

		requestMsg, ok := notifyMsgIntf.(*message.EthTxs)
		assert.True(ok)
		assert.NotEmpty(requestMsg.Txs)

		txs := make([]*types.Transaction, 0)
		assert.NoError(rlp.DecodeBytes(requestMsg.Txs, &txs))
		for _, tx := range txs {
			seen[tx.Hash()] = struct{}{}
		}
		wg.Done()
		return nil
	}

	// Notify VM about eth txs
	errs := vm.chain.GetTxPool().AddRemotesSync(ethTxs)
	for _, err := range errs {
		assert.NoError(err, "failed adding coreth tx to mempool")
	}

	attemptAwait(t, &wg, 5*time.Second)

	for _, tx := range ethTxs {
		_, ok := seen[tx.Hash()]
		assert.True(ok, "missing hash: %v", tx.Hash())
	}
}

// show that a geth tx discovered from gossip is requested to the same node that
// gossiped it
func TestMempoolEthTxsAppGossipHandling(t *testing.T) {
	assert := assert.New(t)

	key, err := crypto.GenerateKey()
	assert.NoError(err)

	addr := crypto.PubkeyToAddress(key.PublicKey)

	cfgJson, err := fundAddressByGenesis([]common.Address{addr})
	assert.NoError(err)

	_, vm, _, _, sender := GenesisVM(t, true, cfgJson, "", "")
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
	}()
	vm.chain.GetTxPool().SetGasPrice(common.Big1)
	vm.chain.GetTxPool().SetMinFee(common.Big0)

	var (
		wg          sync.WaitGroup
		txRequested bool
	)
	sender.CantSendAppGossip = false
	sender.SendAppRequestF = func(_ ids.ShortSet, _ uint32, _ []byte) error {
		txRequested = true
		return nil
	}
	wg.Add(1)
	sender.SendAppGossipF = func(_ []byte) error {
		wg.Done()
		return nil
	}

	// prepare a tx
	tx := getValidEthTxs(key, 1, common.Big1)[0]

	// show that unknown coreth hashes is requested
	txBytes, err := rlp.EncodeToBytes([]*types.Transaction{tx})
	assert.NoError(err)
	msg := message.EthTxs{
		Txs: txBytes,
	}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err)

	nodeID := ids.GenerateTestShortID()
	err = vm.AppGossip(nodeID, msgBytes)
	assert.NoError(err)
	assert.False(txRequested, "tx should not be requested")

	// wait for transaction to be re-gossiped
	attemptAwait(t, &wg, 5*time.Second)
}

func TestMempoolEthTxsRegossipSingleAccount(t *testing.T) {
	assert := assert.New(t)

	key, err := crypto.GenerateKey()
	assert.NoError(err)

	addr := crypto.PubkeyToAddress(key.PublicKey)

	cfgJson, err := fundAddressByGenesis([]common.Address{addr})
	assert.NoError(err)

	_, vm, _, _, _ := GenesisVM(t, true, cfgJson, `{"local-txs-enabled":true}`, "")
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
	}()
	vm.chain.GetTxPool().SetGasPrice(common.Big1)
	vm.chain.GetTxPool().SetMinFee(common.Big0)

	// create eth txes
	ethTxs := getValidEthTxs(key, 10, big.NewInt(226*params.GWei))

	// Notify VM about eth txs
	errs := vm.chain.GetTxPool().AddRemotesSync(ethTxs[:5])
	for _, err := range errs {
		assert.NoError(err, "failed adding coreth tx to remote mempool")
	}
	errs = vm.chain.GetTxPool().AddLocals(ethTxs[5:])
	for _, err := range errs {
		assert.NoError(err, "failed adding coreth tx to local mempool")
	}

	// Only transaction for regossip will be first remote (considered local)
	pushNetwork := vm.network.(*pushNetwork)
	queued := pushNetwork.queueRegossipTxs()
	assert.Len(queued, 1, "unexpected length of queued txs")
	assert.Equal(ethTxs[0].Hash(), queued[0].Hash())
}

func TestMempoolEthTxsRegossip(t *testing.T) {
	assert := assert.New(t)

	keys := make([]*ecdsa.PrivateKey, 20)
	addrs := make([]common.Address, 20)
	for i := 0; i < 20; i++ {
		key, err := crypto.GenerateKey()
		assert.NoError(err)
		keys[i] = key
		addrs[i] = crypto.PubkeyToAddress(key.PublicKey)
	}

	cfgJson, err := fundAddressByGenesis(addrs)
	assert.NoError(err)

	_, vm, _, _, _ := GenesisVM(t, true, cfgJson, `{"local-txs-enabled":true}`, "")
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
	}()
	vm.chain.GetTxPool().SetGasPrice(common.Big1)
	vm.chain.GetTxPool().SetMinFee(common.Big0)

	// create eth txes
	ethTxs := []*types.Transaction{}
	for i := 0; i < 20; i++ {
		txs := getValidEthTxs(keys[i], 1, big.NewInt(226*params.GWei))
		ethTxs = append(ethTxs, txs[0])
	}

	// Notify VM about eth txs
	errs := vm.chain.GetTxPool().AddRemotesSync(ethTxs[:10])
	for _, err := range errs {
		assert.NoError(err, "failed adding coreth tx to remote mempool")
	}
	errs = vm.chain.GetTxPool().AddLocals(ethTxs[10:])
	for _, err := range errs {
		assert.NoError(err, "failed adding coreth tx to local mempool")
	}

	// Queued txs will be full because all from different addresses
	pushNetwork := vm.network.(*pushNetwork)
	queued := pushNetwork.queueRegossipTxs()
	assert.Len(queued, 15, "unexpected length of queued txs")

	// Confirm locals
	for i, tx := range ethTxs[10:] {
		assert.Equal(tx.Hash(), queued[i].Hash())
	}

	// Confirm remotes
	for i, tx := range ethTxs[0:5] {
		assert.Equal(tx.Hash(), queued[i+10].Hash())
	}
}
