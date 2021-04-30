package chain

import (
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/eth/filters"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/common"
)

func TestSubscribeTransactionsTest(t *testing.T) {
	chain, newBlockChan, newTxPoolHeadChan, txSubmitCh := NewDefaultChain(t)

	ethBackend := chain.APIBackend()
	eventSystem := filters.NewEventSystem(ethBackend, true)

	acceptedTxsEventsChannel := make(chan []common.Hash)
	acceptedTxsEvents := eventSystem.SubscribeAcceptedTxs(acceptedTxsEventsChannel)

	pendingTxsEventsChannel := make(chan []common.Hash)
	pendingTxsEvents := eventSystem.SubscribePendingTxs(pendingTxsEventsChannel)

	// Override SetOnSealFinish set in NewDefaultChain, so that each sealed block
	// is set as the new preferred block within this test.
	chain.SetOnSealFinish(func(block *types.Block) error {
		if err := chain.SetPreference(block); err != nil {
			t.Fatal(err)
		}
		newBlockChan <- block
		return nil
	})

	chain.Start()
	defer chain.Stop()

	// *NOTE* this was pre-compiled for the test..
	/*
		pragma solidity >=0.6.0;

		contract Counter {
		    uint256 x;

		    event CounterEmit(uint256 indexed oldval, uint256 indexed newval);

		    constructor() public {
		        emit CounterEmit(0, 42);
		        x = 42;
		    }

		    function add(uint256 y) public returns (uint256) {
		        x = x + y;
		        emit CounterEmit(y, x);
		        return x;
		    }
		}
	*/
	// contracts, err := compiler.CompileSolidityString("", src)
	// checkError(err)
	// contract, _ := contracts[fmt.Sprintf("%s:%s", ".", "Counter")]
	// _ = contract

	// solc-linux-amd64-v0.6.12+commit.27d51765 --bin -o counter.bin counter.sol

	code := common.Hex2Bytes(
		"608060405234801561001057600080fd5b50602a60007f53564ba0be98bdbd40460eb78d2387edab91de6a842e1449053dae1f07439a3160405160405180910390a3602a60008190555060e9806100576000396000f3fe6080604052348015600f57600080fd5b506004361060285760003560e01c80631003e2d214602d575b600080fd5b605660048036036020811015604157600080fd5b8101908080359060200190929190505050606c565b6040518082815260200191505060405180910390f35b60008160005401600081905550600054827f53564ba0be98bdbd40460eb78d2387edab91de6a842e1449053dae1f07439a3160405160405180910390a3600054905091905056fea2646970667358221220dd9c84516cd903bf6a151cbdaef2f2514c28f2f422782a388a2774412b81f08864736f6c634300060c0033",
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
	txs := <-txSubmitCh
	chain.GenBlock()

	block := <-newBlockChan

	<-newTxPoolHeadChan

	select {
	case <-acceptedTxsEventsChannel:
		t.Fatal("Unexpected accepted tx head")
	default:
	}

	select {
	case pendingTx := <-pendingTxsEventsChannel:
		if len(pendingTx) != 1 {
			t.Fatal("Expected a new pending tx")
		}
		if pendingTx[0] != signedTx.Hash() {
			t.Fatalf("Expected a new pending tx for signed hash %s", signedTx.Hash().String())
		}
	default:
		t.Fatal("Expected to receive pending transaction from subscription")
	}

	if err := chain.Accept(block); err != nil {
		t.Fatal(err)
	}

	pendingTxsEventHash := <-pendingTxsEventsChannel

	acceptedTxsEventHash := <-acceptedTxsEventsChannel

	pendingTxsEvents.Unsubscribe()
	acceptedTxsEvents.Unsubscribe()

	if block.NumberU64() != uint64(1) {
		t.Fatalf("Expected to create a new block with height 1, but found %d", block.NumberU64())
	}

	if len(pendingTxsEventHash) != 1 {
		t.Fatal("Expected a new pending tx")
	}
	if pendingTxsEventHash[0] != signedTx.Hash() {
		t.Fatalf("Expected a new pending tx for signed hash %s", signedTx.Hash().String())
	}

	if len(acceptedTxsEventHash) != 1 {
		t.Fatal("Expected a new accepted tx")
	}
	if acceptedTxsEventHash[0] != signedTx.Hash() {
		t.Fatalf("Expected a new accepted tx for signed hash %s", signedTx.Hash().String())
	}

	if len(txs.Txs) != 1 {
		t.Fatal("Expected to create a new tx")
	}
	if txs.Txs[0].Hash() != signedTx.Hash() {
		t.Fatalf("Expected subscription for signed hash %s", signedTx.Hash().String())
	}
}
