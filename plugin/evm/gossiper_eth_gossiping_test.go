// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"

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
		GasLimit:   params.TestChainConfig.FeeConfig.GasLimit.Uint64(),
	}
	funds := make(map[common.Address]core.GenesisAccount)
	for _, addr := range addrs {
		funds[addr] = core.GenesisAccount{
			Balance: balance,
		}
	}
	genesis.Alloc = funds
	genesis.Config = params.TestChainConfig

	bytes, err := json.Marshal(genesis)
	return string(bytes), err
}

func getValidEthTxs(key *ecdsa.PrivateKey, count int, gasPrice *big.Int) []*types.Transaction {
	res := make([]*types.Transaction, count)

	to := common.Address{}
	amount := big.NewInt(0)
	gasLimit := uint64(37000)

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
func TestMempoolEthTxsAddedTxsGossipedAfterActivation(t *testing.T) {
	if os.Getenv("RUN_FLAKY_TESTS") != "true" {
		t.Skip("FLAKY")
	}
	assert := assert.New(t)

	key, err := crypto.GenerateKey()
	assert.NoError(err)

	addr := crypto.PubkeyToAddress(key.PublicKey)

	genesisJSON, err := fundAddressByGenesis([]common.Address{addr})
	assert.NoError(err)

	_, vm, _, sender := GenesisVM(t, true, genesisJSON, "", "")
	defer func() {
		err := vm.Shutdown(context.Background())
		assert.NoError(err)
	}()
	vm.txPool.SetGasPrice(common.Big1)
	vm.txPool.SetMinFee(common.Big0)

	// create eth txes
	ethTxs := getValidEthTxs(key, 3, common.Big1)

	var wg sync.WaitGroup
	wg.Add(2)
	sender.CantSendAppGossip = false
	signal1 := make(chan struct{})
	seen := 0
	sender.SendAppGossipF = func(_ context.Context, gossipedBytes []byte) error {
		if seen == 0 {
			notifyMsgIntf, err := message.ParseGossipMessage(vm.networkCodec, gossipedBytes)
			assert.NoError(err)

			requestMsg, ok := notifyMsgIntf.(message.EthTxsGossip)
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
			notifyMsgIntf, err := message.ParseGossipMessage(vm.networkCodec, gossipedBytes)
			assert.NoError(err)

			requestMsg, ok := notifyMsgIntf.(message.EthTxsGossip)
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
	errs := vm.txPool.AddRemotesSync(ethTxs[:2])
	for _, err := range errs {
		assert.NoError(err, "failed adding subnet-evm tx to mempool")
	}

	// Gossip txs again (shouldn't gossip hashes)
	<-signal1 // wait until reorg processed
	assert.NoError(vm.gossiper.GossipEthTxs(ethTxs[:2]))

	errs = vm.txPool.AddRemotesSync(ethTxs)
	assert.Contains(errs[0].Error(), "already known")
	assert.Contains(errs[1].Error(), "already known")
	assert.NoError(errs[2], "failed adding subnet-evm tx to mempool")

	attemptAwait(t, &wg, 5*time.Second)
}

// show that locally issued eth txs are chunked correctly
func TestMempoolEthTxsAddedTxsGossipedAfterActivationChunking(t *testing.T) {
	if os.Getenv("RUN_FLAKY_TESTS") != "true" {
		t.Skip("FLAKY")
	}
	assert := assert.New(t)

	key, err := crypto.GenerateKey()
	assert.NoError(err)

	addr := crypto.PubkeyToAddress(key.PublicKey)

	genesisJSON, err := fundAddressByGenesis([]common.Address{addr})
	assert.NoError(err)

	_, vm, _, sender := GenesisVM(t, true, genesisJSON, "", "")
	defer func() {
		err := vm.Shutdown(context.Background())
		assert.NoError(err)
	}()
	vm.txPool.SetGasPrice(common.Big1)
	vm.txPool.SetMinFee(common.Big0)

	// create eth txes
	ethTxs := getValidEthTxs(key, 100, common.Big1)

	var wg sync.WaitGroup
	wg.Add(2)
	sender.CantSendAppGossip = false
	seen := map[common.Hash]struct{}{}
	sender.SendAppGossipF = func(_ context.Context, gossipedBytes []byte) error {
		notifyMsgIntf, err := message.ParseGossipMessage(vm.networkCodec, gossipedBytes)
		assert.NoError(err)

		requestMsg, ok := notifyMsgIntf.(message.EthTxsGossip)
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
	errs := vm.txPool.AddRemotesSync(ethTxs)
	for _, err := range errs {
		assert.NoError(err, "failed adding subnet-evm tx to mempool")
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
	if os.Getenv("RUN_FLAKY_TESTS") != "true" {
		t.Skip("FLAKY")
	}
	assert := assert.New(t)

	key, err := crypto.GenerateKey()
	assert.NoError(err)

	addr := crypto.PubkeyToAddress(key.PublicKey)

	genesisJSON, err := fundAddressByGenesis([]common.Address{addr})
	assert.NoError(err)

	_, vm, _, sender := GenesisVM(t, true, genesisJSON, "", "")
	defer func() {
		err := vm.Shutdown(context.Background())
		assert.NoError(err)
	}()
	vm.txPool.SetGasPrice(common.Big1)
	vm.txPool.SetMinFee(common.Big0)

	var (
		wg          sync.WaitGroup
		txRequested bool
	)
	sender.CantSendAppGossip = false
	sender.SendAppRequestF = func(context.Context, set.Set[ids.NodeID], uint32, []byte) error {
		txRequested = true
		return nil
	}
	wg.Add(1)
	sender.SendAppGossipF = func(context.Context, []byte) error {
		wg.Done()
		return nil
	}

	// prepare a tx
	tx := getValidEthTxs(key, 1, common.Big1)[0]

	// show that unknown subnet-evm hashes is requested
	txBytes, err := rlp.EncodeToBytes([]*types.Transaction{tx})
	assert.NoError(err)
	msg := message.EthTxsGossip{
		Txs: txBytes,
	}
	msgBytes, err := message.BuildGossipMessage(vm.networkCodec, msg)
	assert.NoError(err)

	nodeID := ids.GenerateTestNodeID()
	err = vm.AppGossip(context.Background(), nodeID, msgBytes)
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

	genesisJSON, err := fundAddressByGenesis([]common.Address{addr})
	assert.NoError(err)

	_, vm, _, _ := GenesisVM(t, true, genesisJSON, `{"local-txs-enabled":true}`, "")
	defer func() {
		err := vm.Shutdown(context.Background())
		assert.NoError(err)
	}()
	vm.txPool.SetGasPrice(common.Big1)
	vm.txPool.SetMinFee(common.Big0)

	// create eth txes
	ethTxs := getValidEthTxs(key, 10, big.NewInt(226*params.GWei))

	// Notify VM about eth txs
	errs := vm.txPool.AddRemotesSync(ethTxs)
	for _, err := range errs {
		assert.NoError(err, "failed adding subnet-evm tx to remote mempool")
	}

	// Only 1 transaction will be regossiped for an address (should be lowest
	// nonce)
	pushNetwork := vm.gossiper.(*pushGossiper)
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

	genesisJSON, err := fundAddressByGenesis(addrs)
	assert.NoError(err)

	_, vm, _, _ := GenesisVM(t, true, genesisJSON, `{"local-txs-enabled":true}`, "")
	defer func() {
		err := vm.Shutdown(context.Background())
		assert.NoError(err)
	}()
	vm.txPool.SetGasPrice(common.Big1)
	vm.txPool.SetMinFee(common.Big0)

	// create eth txes
	ethTxs := make([]*types.Transaction, 20)
	ethTxHashes := make([]common.Hash, 20)
	for i := 0; i < 20; i++ {
		txs := getValidEthTxs(keys[i], 1, big.NewInt(226*params.GWei))
		tx := txs[0]
		ethTxs[i] = tx
		ethTxHashes[i] = tx.Hash()
	}

	// Notify VM about eth txs
	errs := vm.txPool.AddRemotesSync(ethTxs[:10])
	for _, err := range errs {
		assert.NoError(err, "failed adding subnet-evm tx to remote mempool")
	}
	errs = vm.txPool.AddLocals(ethTxs[10:])
	for _, err := range errs {
		assert.NoError(err, "failed adding subnet-evm tx to local mempool")
	}

	// We expect 16 transactions (the default max number of transactions to
	// regossip) comprised of 10 local txs and 5 remote txs (we prioritize local
	// txs over remote).
	pushNetwork := vm.gossiper.(*pushGossiper)
	queued := pushNetwork.queueRegossipTxs()
	assert.Len(queued, 16, "unexpected length of queued txs")

	// Confirm queued transactions (should be ordered based on
	// timestamp submitted, with local priorized over remote)
	queuedTxHashes := make([]common.Hash, 16)
	for i, tx := range queued {
		queuedTxHashes[i] = tx.Hash()
	}
	assert.ElementsMatch(queuedTxHashes[:10], ethTxHashes[10:], "missing local transactions")

	// NOTE: We don't care which remote transactions are included in this test
	// (due to the non-deterministic way pending transactions are surfaced, this can be difficult
	// to assert as well).
}

func TestMempoolTxsPriorityRegossip(t *testing.T) {
	assert := assert.New(t)

	key, err := crypto.GenerateKey()
	assert.NoError(err)
	addr := crypto.PubkeyToAddress(key.PublicKey)

	key2, err := crypto.GenerateKey()
	assert.NoError(err)
	addr2 := crypto.PubkeyToAddress(key2.PublicKey)

	cfgJson, err := fundAddressByGenesis([]common.Address{addr, addr2})
	assert.NoError(err)

	cfg := fmt.Sprintf(`{"local-txs-enabled":true,"priority-regossip-addresses":["%s"]}`, addr)
	_, vm, _, _ := GenesisVM(t, true, cfgJson, cfg, "")
	defer func() {
		err := vm.Shutdown(context.Background())
		assert.NoError(err)
	}()
	vm.txPool.SetGasPrice(common.Big1)
	vm.txPool.SetMinFee(common.Big0)

	// create eth txes
	txs := getValidEthTxs(key, 10, big.NewInt(226*params.GWei))
	txs2 := getValidEthTxs(key2, 10, big.NewInt(226*params.GWei))

	// Notify VM about eth txs
	for _, err := range vm.txPool.AddRemotesSync(txs) {
		assert.NoError(err, "failed adding subnet-evm tx to remote mempool")
	}
	for _, err := range vm.txPool.AddRemotesSync(txs2) {
		assert.NoError(err, "failed adding subnet-evm tx 2 to remote mempool")
	}

	// 10 transactions will be regossiped for a priority address (others ignored)
	pushNetwork := vm.gossiper.(*pushGossiper)
	queued := pushNetwork.queuePriorityRegossipTxs()
	assert.Len(queued, 10, "unexpected length of queued txs")
	assert.ElementsMatch(txs, queued)
}

func attemptAwait(t *testing.T, wg *sync.WaitGroup, delay time.Duration) {
	ticker := make(chan struct{})

	// Wait for [wg] and then close [ticket] to indicate that
	// the wait group has finished.
	go func() {
		wg.Wait()
		close(ticker)
	}()

	select {
	case <-time.After(delay):
		t.Fatal("Timed out waiting for wait group to complete")
	case <-ticker:
		// The wait group completed without issue
	}
}
