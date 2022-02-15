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

	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"
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
		ChainID:            params.TestChainConfig.ChainID,
		SubnetEVMTimestamp: big.NewInt(0),
	}

	bytes, err := json.Marshal(genesis)
	return string(bytes), err
}

func getValidTxs(key *ecdsa.PrivateKey, count int, gasPrice *big.Int) []*types.Transaction {
	res := make([]*types.Transaction, count)

	to := common.Address{}
	amount := big.NewInt(10000)
	gasLimit := uint64(100000)

	for i := 0; i < count; i++ {
		tx, _ := types.SignTx(
			types.NewTransaction(
				uint64(i),
				to,
				amount,
				gasLimit,
				gasPrice,
				[]byte(strings.Repeat("aaaaaaaaaa", 100))),
			types.HomesteadSigner{}, key)
		tx.SetFirstSeen(time.Now().Add(-1 * time.Minute))
		res[i] = tx
	}
	return res
}

// show that locally issued eth txs are gossiped
// Note: channel through which subnet-evm mempool push txs to vm is injected here
// to ease up UT, which target only VM behaviors in response to subnet-evm mempool
// signals
func TestMempoolTxsAddedTxsGossipedAfterActivation(t *testing.T) {
	assert := assert.New(t)

	key, err := crypto.GenerateKey()
	assert.NoError(err)

	addr := crypto.PubkeyToAddress(key.PublicKey)
	cfgJson, err := fundAddressByGenesis([]common.Address{addr})
	assert.NoError(err)

	_, vm, _, sender := GenesisVM(t, true, cfgJson, "", "")
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
	}()
	vm.chain.GetTxPool().SetGasPrice(common.Big1)
	vm.chain.GetTxPool().SetMinFee(common.Big0)

	// create eth txes
	ethTxs := getValidTxs(key, 3, common.Big1)

	var wg sync.WaitGroup
	var wg2 sync.WaitGroup
	wg.Add(2)
	wg2.Add(1)
	sender.CantSendAppGossip = false
	seen := 0
	sender.SendAppGossipF = func(gossipedBytes []byte) error {
		if seen == 0 {
			notifyMsgIntf, err := message.ParseMessage(vm.networkCodec, gossipedBytes)
			assert.NoError(err)

			requestMsg, ok := notifyMsgIntf.(*message.Txs)
			assert.True(ok)
			assert.NotEmpty(requestMsg.Txs)

			txs := make([]*types.Transaction, 0)
			assert.NoError(rlp.DecodeBytes(requestMsg.Txs, &txs))
			assert.Len(txs, 2)
			assert.ElementsMatch(
				[]common.Hash{ethTxs[0].Hash(), txs[1].Hash()},
				[]common.Hash{txs[0].Hash(), txs[1].Hash()},
			)
			seen++
			wg2.Done()
		} else if seen == 1 {
			notifyMsgIntf, err := message.ParseMessage(vm.networkCodec, gossipedBytes)
			assert.NoError(err)

			requestMsg, ok := notifyMsgIntf.(*message.Txs)
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
		assert.NoError(err, "failed adding subnet-evm tx to mempool")
	}

	// Gossip txs again (shouldn't gossip hashes)
	attemptAwait(t, &wg2, 5*time.Second) // wait until reorg processed
	assert.NoError(vm.gossiper.GossipTxs(ethTxs[:2]))

	errs = vm.chain.GetTxPool().AddRemotesSync(ethTxs)
	assert.Contains(errs[0].Error(), "already known")
	assert.Contains(errs[1].Error(), "already known")
	assert.NoError(errs[2], "failed adding subnet-evm tx to mempool")

	attemptAwait(t, &wg, 5*time.Second)
}

// show that locally issued eth txs are chunked correctly
func TestMempoolTxsAddedTxsGossipedAfterActivationChunking(t *testing.T) {
	assert := assert.New(t)

	key, err := crypto.GenerateKey()
	assert.NoError(err)

	addr := crypto.PubkeyToAddress(key.PublicKey)

	cfgJson, err := fundAddressByGenesis([]common.Address{addr})
	assert.NoError(err)

	_, vm, _, sender := GenesisVM(t, true, cfgJson, "", "")
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
	}()
	vm.chain.GetTxPool().SetGasPrice(common.Big1)
	vm.chain.GetTxPool().SetMinFee(common.Big0)

	// create eth txes
	txs := getValidTxs(key, 100, common.Big1)

	var wg sync.WaitGroup
	wg.Add(2)
	sender.CantSendAppGossip = false
	seen := map[common.Hash]struct{}{}
	sender.SendAppGossipF = func(gossipedBytes []byte) error {
		notifyMsgIntf, err := message.ParseMessage(vm.networkCodec, gossipedBytes)
		assert.NoError(err)

		requestMsg, ok := notifyMsgIntf.(*message.Txs)
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
	errs := vm.chain.GetTxPool().AddRemotesSync(txs)
	for _, err := range errs {
		assert.NoError(err, "failed adding subnet-evm tx to mempool")
	}

	attemptAwait(t, &wg, 5*time.Second)

	for _, tx := range txs {
		_, ok := seen[tx.Hash()]
		assert.True(ok, "missing hash: %v", tx.Hash())
	}
}

// show that a geth tx discovered from gossip is requested to the same node that
// gossiped it
func TestMempoolTxsAppGossipHandling(t *testing.T) {
	assert := assert.New(t)

	key, err := crypto.GenerateKey()
	assert.NoError(err)

	addr := crypto.PubkeyToAddress(key.PublicKey)

	cfgJson, err := fundAddressByGenesis([]common.Address{addr})
	assert.NoError(err)

	_, vm, _, sender := GenesisVM(t, true, cfgJson, "", "")
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
	tx := getValidTxs(key, 1, common.Big1)[0]

	// show that unknown subnet-evm hashes is requested
	txBytes, err := rlp.EncodeToBytes([]*types.Transaction{tx})
	assert.NoError(err)
	msg := message.Txs{
		Txs: txBytes,
	}
	msgBytes, err := message.BuildMessage(vm.networkCodec, &msg)
	assert.NoError(err)

	nodeID := ids.GenerateTestShortID()
	err = vm.AppGossip(nodeID, msgBytes)
	assert.NoError(err)
	assert.False(txRequested, "tx should not be requested")

	// wait for transaction to be re-gossiped
	attemptAwait(t, &wg, 5*time.Second)
}

func TestMempoolTxsRegossipSingleAccount(t *testing.T) {
	assert := assert.New(t)

	key, err := crypto.GenerateKey()
	assert.NoError(err)

	addr := crypto.PubkeyToAddress(key.PublicKey)

	cfgJson, err := fundAddressByGenesis([]common.Address{addr})
	assert.NoError(err)

	_, vm, _, _ := GenesisVM(t, true, cfgJson, `{"local-txs-enabled":true}`, "")
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
	}()
	vm.chain.GetTxPool().SetGasPrice(common.Big1)
	vm.chain.GetTxPool().SetMinFee(common.Big0)

	// create eth txes
	txs := getValidTxs(key, 10, big.NewInt(226*params.GWei))

	// Notify VM about eth txs
	errs := vm.chain.GetTxPool().AddRemotesSync(txs)
	for _, err := range errs {
		assert.NoError(err, "failed adding subnet-evm tx to remote mempool")
	}

	// Only 1 transaction will be regossiped for an address (should be lowest
	// nonce)
	pushNetwork := vm.gossiper.(*pushGossiper)
	queued := pushNetwork.queueRegossipTxs()
	assert.Len(queued, 1, "unexpected length of queued txs")
	assert.Equal(txs[0].Hash(), queued[0].Hash())
}

func TestMempoolTxsRegossip(t *testing.T) {
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

	_, vm, _, _ := GenesisVM(t, true, cfgJson, `{"local-txs-enabled":true}`, "")
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
	}()
	vm.chain.GetTxPool().SetGasPrice(common.Big1)
	vm.chain.GetTxPool().SetMinFee(common.Big0)

	// create eth txes
	ethTxs := make([]*types.Transaction, 20)
	ethTxHashes := make([]common.Hash, 20)
	for i := 0; i < 20; i++ {
		txs := getValidTxs(keys[i], 1, big.NewInt(226*params.GWei))
		tx := txs[0]
		ethTxs[i] = tx
		ethTxHashes[i] = tx.Hash()
	}

	// Notify VM about eth txs
	errs := vm.chain.GetTxPool().AddRemotesSync(ethTxs[:10])
	for _, err := range errs {
		assert.NoError(err, "failed adding subnet-evm tx to remote mempool")
	}
	errs = vm.chain.GetTxPool().AddLocals(ethTxs[10:])
	for _, err := range errs {
		assert.NoError(err, "failed adding subnet-evm tx to local mempool")
	}

	// We expect 15 transactions (the default max number of transactions to
	// regossip) comprised of 10 local txs and 5 remote txs (we prioritize local
	// txs over remote).
	pushNetwork := vm.gossiper.(*pushGossiper)
	queued := pushNetwork.queueRegossipTxs()
	assert.Len(queued, 15, "unexpected length of queued txs")

	// Confirm queued transactions (should be ordered based on
	// timestamp submitted, with local priorized over remote)
	queuedTxHashes := make([]common.Hash, 15)
	for i, tx := range queued {
		queuedTxHashes[i] = tx.Hash()
	}
	assert.ElementsMatch(queuedTxHashes[:10], ethTxHashes[10:], "missing local transactions")

	// NOTE: We don't care which remote transactions are included in this test
	// (due to the non-deterministic way pending transactions are surfaced, this can be difficult
	// to assert as well).
}
