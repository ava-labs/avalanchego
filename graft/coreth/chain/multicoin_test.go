// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// NOTE from Ted: to compile from solidity source using the code, make sure
// your solc<0.8.0, as geth 1.9.21 does not support the JSON output from
// solc>=0.8.0:
// See:
// - https://github.com/ethereum/go-ethereum/issues/22041
// - https://github.com/ethereum/go-ethereum/pull/22092

package chain

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ava-labs/coreth/accounts/keystore"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/eth/ethconfig"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

// TestMulticoin tests multicoin low-level state management and regular
// transaction/smart contract transfer.
func TestMulticoin(t *testing.T) {
	// configure the chain
	config := ethconfig.NewDefaultConfig()

	// configure the genesis block
	genesisJSON := `{"config":{"chainId":1,"homesteadBlock":0,"daoForkBlock":0,"daoForkSupport":true,"eip150Block":0,"eip150Hash":"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0","eip155Block":0,"eip158Block":0,"byzantiumBlock":0,"constantinopleBlock":0,"petersburgBlock":0,"petersburgBlock":0,"istanbulBlock":0,"muirGlacierBlock":0},"nonce":"0x0","timestamp":"0x0","extraData":"0x00","gasLimit":"0x5f5e100","difficulty":"0x0","mixHash":"0x0000000000000000000000000000000000000000000000000000000000000000","coinbase":"0x0000000000000000000000000000000000000000","alloc":{"751a0b96e1042bee789452ecb20253fba40dbe85":{"balance":"0x1000000000000000", "mcbalance": {"0x0000000000000000000000000000000000000000000000000000000000000000": 1000000000000000000}}, "0100000000000000000000000000000000000000": {"code": "0x73000000000000000000000000000000000000000030146080604052600436106100405760003560e01c80631e01043914610045578063b6510bb314610087575b600080fd5b6100716004803603602081101561005b57600080fd5b81019080803590602001909291905050506100f6565b6040518082815260200191505060405180910390f35b81801561009357600080fd5b506100f4600480360360808110156100aa57600080fd5b81019080803573ffffffffffffffffffffffffffffffffffffffff169060200190929190803590602001909291908035906020019092919080359060200190929190505050610119565b005b60003073ffffffffffffffffffffffffffffffffffffffff168290cd9050919050565b8373ffffffffffffffffffffffffffffffffffffffff1681836108fc8690811502906040516000604051808303818888878c8acf95505050505050158015610165573d6000803e3d6000fd5b505050505056fea26469706673582212204ca02a58b31e59814fcb487b2bdc205149e01e9f695f02f5e73ae40c4f027c1e64736f6c634300060a0033", "balance": "0x0", "mcbalance": {}}},"number":"0x0","gasUsed":"0x0","parentHash":"0x0000000000000000000000000000000000000000000000000000000000000000"}`
	mcAbiJSON := `[{"inputs":[{"internalType":"uint256","name":"coinid","type":"uint256"}],"name":"getBalance","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address payable","name":"recipient","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"},{"internalType":"uint256","name":"coinid","type":"uint256"},{"internalType":"uint256","name":"amount2","type":"uint256"}],"name":"transfer","outputs":[],"stateMutability":"nonpayable","type":"function"}]`
	genesisKey := "0xabd71b35d559563fea757f0f5edbde286fb8c043105b15abb7cd57189306d7d1"

	bobKey, _ := keystore.NewKey(rand.Reader)
	genesisBlock := new(core.Genesis)
	if err := json.Unmarshal([]byte(genesisJSON), genesisBlock); err != nil {
		t.Fatal(err)
	}
	hk, _ := crypto.HexToECDSA(genesisKey[2:])
	genKey := keystore.NewKeyFromECDSA(hk)

	config.Genesis = genesisBlock

	newBlockChan := make(chan *types.Block)

	// NOTE: use precompiled `mc_test.sol` for portability, do not remove the
	// following code (for debug purpose)
	//
	//// compile the smart contract
	//gopath := os.Getenv("GOPATH")
	//if gopath == "" {
	//	gopath = build.Default.GOPATH
	//}
	//counterSrc, err := filepath.Abs(gopath + "/src/github.com/ava-labs/coreth/examples/multicoin/mc_test.sol")
	//if err != nil {
	// 	t.Fatal(err)
	// }
	//contracts, err := compiler.CompileSolidity("", counterSrc)
	//if err != nil {
	// 	t.Fatal(err)
	// }
	//contract, _ := contracts[fmt.Sprintf("%s:%s", counterSrc, "MCTest")]
	// abiStr, err := json.Marshal(contract.Info.AbiDefinition)
	// contractAbi, err := abi.JSON(strings.NewReader(string(abiStr)))
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// code := common.Hex2Bytes(contract.Code[2:])

	// see `mc_test.sol`
	contract := "608060405234801561001057600080fd5b50610426806100206000396000f3fe60806040526004361061002d5760003560e01c8063a41fe49f14610039578063ba7b37d41461008857610034565b3661003457005b600080fd5b34801561004557600080fd5b506100866004803603606081101561005c57600080fd5b810190808035906020019092919080359060200190929190803590602001909291905050506100c3565b005b34801561009457600080fd5b506100c1600480360360208110156100ab57600080fd5b810190808035906020019092919050505061025a565b005b600073010000000000000000000000000000000000000073ffffffffffffffffffffffffffffffffffffffff1633858585604051602401808573ffffffffffffffffffffffffffffffffffffffff1681526020018481526020018381526020018281526020019450505050506040516020818303038152906040527fb6510bb3000000000000000000000000000000000000000000000000000000007bffffffffffffffffffffffffffffffffffffffffffffffffffffffff19166020820180517bffffffffffffffffffffffffffffffffffffffffffffffffffffffff83818316178352505050506040518082805190602001908083835b602083106101df57805182526020820191506020810190506020830392506101bc565b6001836020036101000a0380198251168184511680821785525050505050509050019150506000604051808303816000865af19150503d8060008114610241576040519150601f19603f3d011682016040523d82523d6000602084013e610246565b606091505b505090508061025457600080fd5b50505050565b60008073010000000000000000000000000000000000000073ffffffffffffffffffffffffffffffffffffffff1683604051602401808281526020019150506040516020818303038152906040527f1e010439000000000000000000000000000000000000000000000000000000007bffffffffffffffffffffffffffffffffffffffffffffffffffffffff19166020820180517bffffffffffffffffffffffffffffffffffffffffffffffffffffffff83818316178352505050506040518082805190602001908083835b602083106103495780518252602082019150602081019050602083039250610326565b6001836020036101000a0380198251168184511680821785525050505050509050019150506000604051808303816000865af19150503d80600081146103ab576040519150601f19603f3d011682016040523d82523d6000602084013e6103b0565b606091505b5091509150816103bf57600080fd5b8080602001905160208110156103d457600080fd5b810190808051906020019092919050505060008190555050505056fea26469706673582212207931f8bf71bbaeaffac554cafb419604155328b1466fae52488964ccba082f5464736f6c63430007060033"
	contractAbi, err := abi.JSON(strings.NewReader(`[{"inputs":[],"stateMutability":"nonpayable","type":"constructor"},{"inputs":[{"internalType":"uint256","name":"coinid","type":"uint256"}],"name":"updateBalance","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"amount","type":"uint256"},{"internalType":"uint256","name":"coinid","type":"uint256"},{"internalType":"uint256","name":"amount2","type":"uint256"}],"name":"withdraw","outputs":[],"stateMutability":"nonpayable","type":"function"},{"stateMutability":"payable","type":"receive"}]`))
	if err != nil {
		t.Fatal(err)
	}
	code := common.Hex2Bytes(contract)

	chain := NewETHChain(&config, nil, rawdb.NewMemoryDatabase(), eth.DefaultSettings, true)

	if err := chain.Accept(chain.GetGenesisBlock()); err != nil {
		t.Fatal(err)
	}

	newTxPoolHeadChan := make(chan core.NewTxPoolHeadEvent, 1)
	log.Info(chain.GetGenesisBlock().Hash().Hex())

	mcAbi, err := abi.JSON(strings.NewReader(mcAbiJSON))
	if err != nil {
		t.Fatal(err)
	}

	chain.SetOnSealFinish(func(block *types.Block) error {
		if err := chain.SetPreference(block); err != nil {
			t.Fatal(err)
		}
		if err := chain.Accept(block); err != nil {
			t.Fatal(err)
		}
		newBlockChan <- block
		return nil
	})

	// start the chain
	chain.GetTxPool().SubscribeNewHeadEvent(newTxPoolHeadChan)
	txSubmitCh := chain.GetTxSubmitCh()
	chain.Start()

	nonce := uint64(0)
	tx := types.NewContractCreation(nonce, big.NewInt(0), uint64(gasLimit), gasPrice, code)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), genKey.PrivateKey)
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

	if txs := block.Transactions(); len(txs) != 1 {
		t.Fatalf("Expected new block to contain 1 transaction, but found %d", len(txs))
	}
	receipts := chain.GetReceiptsByHash(block.Hash())
	if len(receipts) != 1 {
		t.Fatalf("Expected length of receipts to be 1, but found %d", len(receipts))
	}
	contractAddr := receipts[0].ContractAddress

	// give Bob some initial balance
	tx = types.NewTransaction(nonce, bobKey.Address, big.NewInt(300000000000000000), uint64(gasLimit), gasPrice, nil)
	signedTx, err = types.SignTx(tx, types.NewEIP155Signer(chainID), genKey.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	chain.AddRemoteTxs([]*types.Transaction{signedTx})
	nonce++
	<-txSubmitCh
	chain.GenBlock()

	// Await block generation
	block = <-newBlockChan
	<-newTxPoolHeadChan
	if txs := block.Transactions(); len(txs) != 1 {
		t.Fatalf("Expected new block to contain 1 transaction, but found %d", len(txs))
	}

	bobTransferInput, err := mcAbi.Pack("transfer", bobKey.Address, big.NewInt(0), big.NewInt(0), big.NewInt(100000000000000000))
	if err != nil {
		t.Fatal(err)
	}
	contractTransferInput, err := mcAbi.Pack("transfer", contractAddr, big.NewInt(0), big.NewInt(0), big.NewInt(100000000000000000))
	if err != nil {
		t.Fatal(err)
	}

	// send 5 * 100000000000000000 to Bob
	// send 5 * 100000000000000000 to the contract
	for i := 0; i < 5; i++ {
		// transfer some coin0 balance to Bob
		tx1 := types.NewTransaction(nonce, vm.BuiltinAddr, big.NewInt(0), uint64(gasLimit), gasPrice, bobTransferInput)
		signedTx1, err := types.SignTx(tx1, types.NewEIP155Signer(chainID), genKey.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		nonce++

		// transfer some coin0 balance to the contract
		tx2 := types.NewTransaction(nonce, vm.BuiltinAddr, big.NewInt(0), uint64(gasLimit), gasPrice, contractTransferInput)
		signedTx2, err := types.SignTx(tx2, types.NewEIP155Signer(chainID), genKey.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		nonce++

		for _, err := range chain.AddRemoteTxs([]*types.Transaction{signedTx1, signedTx2}) {
			if err != nil {
				t.Fatal(err)
			}
		}

		<-txSubmitCh
		chain.GenBlock()

		block = <-newBlockChan
		<-newTxPoolHeadChan
		if txs := block.Transactions(); len(txs) != 2 {
			t.Fatalf("Expected block to contain 2 transactions, but found %d", len(txs))
		}
	}

	// test contract methods
	// withdraw 10000000000000000 from contract to Bob
	input, err := contractAbi.Pack("withdraw", big.NewInt(0), big.NewInt(0), big.NewInt(10000000000000000))
	if err != nil {
		t.Fatal(err)
	}
	withdrawTx := types.NewTransaction(nonce, contractAddr, big.NewInt(0), uint64(gasLimit), gasPrice, input)
	signedWithdrawTx, err := types.SignTx(withdrawTx, types.NewEIP155Signer(chainID), genKey.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	nonce++

	input, err = contractAbi.Pack("updateBalance", big.NewInt(0))
	if err != nil {
		t.Fatal(err)
	}
	updateBalanceTx := types.NewTransaction(nonce, contractAddr, big.NewInt(0), uint64(gasLimit), gasPrice, input)
	signedUpdateBalanceTx, err := types.SignTx(updateBalanceTx, types.NewEIP155Signer(chainID), genKey.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	chain.AddRemoteTxs([]*types.Transaction{signedWithdrawTx, signedUpdateBalanceTx})

	<-txSubmitCh
	chain.GenBlock()

	block = <-newBlockChan
	<-newTxPoolHeadChan

	if txs := block.Transactions(); len(txs) != 2 {
		t.Fatalf("Expected new block to contain 2 transaction, but found %d", len(txs))
	}

	coin0 := common.HexToHash("0x0")
	state, err := chain.CurrentState()
	if err != nil {
		t.Fatal(err)
	}

	genMCBalance := state.GetBalanceMultiCoin(genKey.Address, coin0)
	bobMCBalance := state.GetBalanceMultiCoin(bobKey.Address, coin0)
	contractMCBalance := state.GetBalanceMultiCoin(contractAddr, coin0)

	log.Info(fmt.Sprintf("genesis balance = %s", state.GetBalance(genKey.Address)))
	log.Info(fmt.Sprintf("genesis mcbalance(0) = %s", genMCBalance))
	log.Info(fmt.Sprintf("bob's balance = %s", state.GetBalance(bobKey.Address)))
	log.Info(fmt.Sprintf("bob's mcbalance(0) = %s", bobMCBalance))
	log.Info(fmt.Sprintf("contract mcbalance(0) = %s", contractMCBalance))
	log.Info(fmt.Sprintf("state = %s", state.Dump(true, false, true)))

	if genMCBalance.Cmp(big.NewInt(10000000000000000)) != 0 {
		t.Fatal("incorrect genesis MC balance")
	}
	if bobMCBalance.Cmp(big.NewInt(500000000000000000)) != 0 {
		t.Fatal("incorrect bob's MC balance")
	}
	if contractMCBalance.Cmp(big.NewInt(490000000000000000)) != 0 {
		t.Fatal("incorrect contract's MC balance")
	}
}
