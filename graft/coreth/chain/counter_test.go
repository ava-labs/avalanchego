// (c) 2019-2020, Ava Labs, Inc. All rights reserved.

// NOTE from Ted: to compile from solidity source using the code, make sure
// your solc<0.8.0, as geth 1.9.21 does not support the JSON output from
// solc>=0.8.0:
// See:
// - https://github.com/ethereum/go-ethereum/issues/22041
// - https://github.com/ethereum/go-ethereum/pull/22092

package chain

import (
	"fmt"
	"math/big"

	"testing"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/common"

	"github.com/ethereum/go-ethereum/log"
)

func TestCounter(t *testing.T) {
	chain, newBlockChan, newTxPoolHeadChan, txSubmitCh := NewDefaultChain(t)

	// Mark the genesis block as accepted and start the chain
	chain.Start()
	defer chain.Stop()

	// NOTE: use precompiled `counter.sol` for portability, do not remove the
	// following code (for debug purpose)
	//counterSrc, err := filepath.Abs(gopath + "/src/github.com/ava-labs/coreth/examples/counter/counter.sol")
	// if err != nil {
	// 	t.Fatal(err)
	// }
	//contracts, err := compiler.CompileSolidity("", counterSrc)
	// if err != nil {
	// 	t.Fatal(err)
	// }
	//contract, _ := contracts[fmt.Sprintf("%s:%s", counterSrc, "Counter")]
	//code := common.Hex2Bytes(contract.Code[2:])
	contract := "6080604052348015600f57600080fd5b50602a60008190555060b9806100266000396000f3fe6080604052348015600f57600080fd5b506004361060285760003560e01c80631003e2d214602d575b600080fd5b605660048036036020811015604157600080fd5b8101908080359060200190929190505050606c565b6040518082815260200191505060405180910390f35b60008160005401600081905550600054905091905056fea264697066735822122066dad7255aac3ea41858c2a0fe986696876ac85b2bb4e929d2062504c244054964736f6c63430007060033"
	code := common.Hex2Bytes(contract)

	nonce := uint64(0)
	tx := types.NewContractCreation(nonce, big.NewInt(0), uint64(gasLimit), gasPrice, code)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), fundedKey.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range chain.AddRemoteTxs([]*types.Transaction{signedTx}) {
		if err != nil {
			t.Fatal(err)
		}
	}
	<-txSubmitCh
	nonce++
	chain.GenBlock()

	block := <-newBlockChan
	<-newTxPoolHeadChan
	log.Info("Generated block with new counter contract creation", "blkNumber", block.NumberU64())

	receipts := chain.GetReceiptsByHash(block.Hash())
	if len(receipts) != 1 {
		t.Fatalf("Expected length of receipts to be 1, but found %d", len(receipts))
	}
	contractAddr := receipts[0].ContractAddress

	call := common.Hex2Bytes("1003e2d20000000000000000000000000000000000000000000000000000000000000001")
	txs := make([]*types.Transaction, 0)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(nonce, contractAddr, big.NewInt(0), uint64(gasLimit), gasPrice, call)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), fundedKey.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		txs = append(txs, signedTx)
		nonce++
	}
	for _, err := range chain.AddRemoteTxs(txs) {
		if err != nil {
			t.Fatal(err)
		}
	}
	<-txSubmitCh
	// Generate block
	chain.GenBlock()

	block = <-newBlockChan
	<-newTxPoolHeadChan
	log.Info("Generated block with counter contract interactions", "blkNumber", block.NumberU64())
	receipts = chain.GetReceiptsByHash(block.Hash())
	if len(receipts) != 10 {
		t.Fatalf("Expected 10 receipts, but found %d", len(receipts))
	}

	state, err := chain.CurrentState()
	if err != nil {
		t.Fatal(err)
	}
	xState := state.GetState(contractAddr, common.BigToHash(big.NewInt(0)))

	log.Info(fmt.Sprintf("genesis balance = %s", state.GetBalance(fundedKey.Address)))
	log.Info(fmt.Sprintf("contract balance = %s", state.GetBalance(contractAddr)))
	log.Info(fmt.Sprintf("state = %s", state.Dump(true, false, true)))
	log.Info(fmt.Sprintf("x = %s", xState.String()))
	if xState.Big().Cmp(big.NewInt(52)) != 0 {
		t.Fatal("incorrect state value")
	}
}
