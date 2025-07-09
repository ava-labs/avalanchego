// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/params"
)

func fundAddressByGenesis(addrs []common.Address) (string, error) {
	balance := big.NewInt(0xffffffffffffff)
	genesis := &core.Genesis{
		Difficulty: common.Big0,
		GasLimit:   params.GetExtra(params.TestChainConfig).FeeConfig.GasLimit.Uint64(),
	}
	funds := make(map[common.Address]types.Account)
	for _, addr := range addrs {
		funds[addr] = types.Account{
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
		tx.SetTime(time.Now().Add(-1 * time.Minute))
		res[i] = tx
	}
	return res
}

// show that a geth tx discovered from gossip is requested to the same node that
// gossiped it
func TestMempoolEthTxsAppGossipHandling(t *testing.T) {
	assert := assert.New(t)

	key, err := crypto.GenerateKey()
	assert.NoError(err)

	addr := crypto.PubkeyToAddress(key.PublicKey)

	genesisJSON, err := fundAddressByGenesis([]common.Address{addr})
	assert.NoError(err)

	vm, _, sender := GenesisVM(t, true, genesisJSON, "", "")
	defer func() {
		err := vm.Shutdown(context.Background())
		assert.NoError(err)
	}()
	vm.txPool.SetGasTip(common.Big1)
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
	sender.SendAppGossipF = func(context.Context, commonEng.SendConfig, []byte) error {
		wg.Done()
		return nil
	}

	// prepare a tx
	tx := getValidEthTxs(key, 1, common.Big1)[0]

	// Txs must be submitted over the API to be included in push gossip.
	// (i.e., txs received via p2p are not included in push gossip)
	err = vm.eth.APIBackend.SendTx(context.Background(), tx)
	assert.NoError(err)
	assert.False(txRequested, "tx should not be requested")

	// wait for transaction to be re-gossiped
	attemptAwait(t, &wg, 5*time.Second)
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
