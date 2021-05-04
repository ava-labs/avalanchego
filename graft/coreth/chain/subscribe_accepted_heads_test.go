package chain

import (
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

func TestAcceptedHeadSubscriptions(t *testing.T) {
	chain, newBlockChan, newTxPoolHeadChan, txSubmitCh := NewDefaultChain(t)

	chain.SetOnSealFinish(func(block *types.Block) error {
		if err := chain.SetPreference(block); err != nil {
			t.Fatal(err)
		}
		newBlockChan <- block
		return nil
	})

	chain.Start()
	defer chain.Stop()

	ethBackend := chain.APIBackend()

	acceptedChainCh := make(chan core.ChainEvent, 1000)
	chainCh := make(chan core.ChainEvent, 1000)
	ethBackend.SubscribeChainAcceptedEvent(acceptedChainCh)
	ethBackend.SubscribeChainEvent(chainCh)

	// *NOTE* this was pre-compiled for the test..
	// src := `pragma solidity >=0.6.0;
	//
	// contract Counter {
	//     uint256 x;
	//
	//     constructor() public {
	//         x = 42;
	//     }
	//
	//     function add(uint256 y) public returns (uint256) {
	//         x = x + y;
	//         return x;
	//     }
	// }`
	// contracts, err := compiler.CompileSolidityString("", src)
	// checkError(err)
	// contract, _ := contracts[fmt.Sprintf("%s:%s", ".", "Counter")]
	// _ = contract

	// solc-linux-amd64-v0.6.12+commit.27d51765 --bin -o counter.bin counter.sol

	code := common.Hex2Bytes(
		"6080604052348015600f57600080fd5b50602a60008190555060b9806100266000396000f3fe6080604052348015600f57600080fd5b506004361060285760003560e01c80631003e2d214602d575b600080fd5b605660048036036020811015604157600080fd5b8101908080359060200190929190505050606c565b6040518082815260200191505060405180910390f35b60008160005401600081905550600054905091905056fea26469706673582212200dc7c76677426e8c621c6839348a7c8d60787c546a9b9c7fc91efa57f71d46a364736f6c634300060c0033",
		// contract.Code[2:],
	)
	tx := types.NewContractCreation(uint64(0), big.NewInt(0), uint64(gasLimit), gasPrice, code)
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

	chain.GenBlock()
	block := <-newBlockChan
	<-newTxPoolHeadChan
	log.Info("Generated block with new counter contract creation", "blkNumber", block.NumberU64())

	if block.NumberU64() != uint64(1) {
		t.Fatal(err)
	}

	select {
	case fb := <-chainCh:
		if fb.Block.NumberU64() != 1 {
			t.Fatalf("received block with unexpected block number %d via chain channel", fb.Block.NumberU64())
		}
		if fb.Block.Hash() != block.Hash() {
			t.Fatalf("Received block with unexpected hash %s via chain channel", fb.Block.Hash().String())
		}
	default:
		t.Fatal("failed to received block via chain channel")
	}

	select {
	case <-acceptedChainCh:
		t.Fatalf("Received unexpected chain accept event before accepting block")
	default:
	}

	if err := chain.Accept(block); err != nil {
		t.Fatal(err)
	}

	select {
	case fb := <-acceptedChainCh:
		if fb.Block.NumberU64() != 1 {
			t.Fatalf("received block with unexpected block number %d on accepted block channel", fb.Block.NumberU64())
		}
		if fb.Block.Hash() != block.Hash() {
			t.Fatalf("Received block with unexpected hash %s via accepted block channel", fb.Block.Hash().String())
		}
	default:
		t.Fatal("failed to received block via accepted block channel")
	}
}
