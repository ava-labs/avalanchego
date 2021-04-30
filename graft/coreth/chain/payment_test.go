// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/log"
)

// TestPayment tests basic payment (balance, not multi-coin)
func TestPayment(t *testing.T) {
	chain, newBlockChan, newTxPoolHeadChan, txSubmitCh := NewDefaultChain(t)

	// Mark the genesis block as accepted and start the chain
	chain.Start()
	defer chain.Stop()

	nonce := uint64(0)
	numTxs := 10
	txs := make([]*types.Transaction, 0)
	for i := 0; i < numTxs; i++ {
		tx := types.NewTransaction(nonce, bob.Address, value, uint64(basicTxGasLimit), gasPrice, nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), fundedKey.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		txs = append(txs, signedTx)
		nonce++
	}
	for _, err := range chain.AddRemoteTxs(txs) {
		if err != nil {
			t.Fatalf("Failed to add remote transactions due to %s", err)
		}
	}
	<-txSubmitCh

	chain.GenBlock()
	block := <-newBlockChan
	<-newTxPoolHeadChan

	if txs := block.Transactions(); len(txs) != numTxs {
		t.Fatalf("Expected block to contain %d transactions, but found %d transactions", numTxs, len(txs))
	}
	log.Info("Generated block", "BlockHash", block.Hash(), "BlockNumber", block.NumberU64())
	currentBlock := chain.BlockChain().CurrentBlock()
	if currentBlock.Hash() != block.Hash() {
		t.Fatalf("Found unexpected current block (%s, %d), expected (%s, %d)", currentBlock.Hash(), currentBlock.NumberU64(), block.Hash(), block.NumberU64())
	}

	// state, err := chain.BlockState(currentBlock)
	state, err := chain.CurrentState()
	if err != nil {
		t.Fatal(err)
	}
	genBalance := state.GetBalance(fundedKey.Address)
	bobBalance := state.GetBalance(bob.Address)
	expectedBalance := new(big.Int).Mul(value, big.NewInt(int64(numTxs)))
	if bobBalance.Cmp(expectedBalance) != 0 {
		t.Fatalf("Found incorrect balance %d for bob's address, expected %d", bobBalance, expectedBalance)
	}

	totalBalance := bobBalance.Add(bobBalance, genBalance)
	totalFees := new(big.Int).Mul(big.NewInt(int64(numTxs*basicTxGasLimit)), gasPrice)
	expectedTotalBalance := new(big.Int).Sub(initialBalance, totalFees)
	if totalBalance.Cmp(expectedTotalBalance) != 0 {
		t.Fatalf("Found incorrect total balance %d, expected total balance of %d", totalBalance, expectedTotalBalance)
	}
}
