package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/ava-labs/coreth"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/compiler"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"go/build"
	"math/big"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func checkError(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	// configure the chain
	config := eth.DefaultConfig
	config.ManualCanonical = true
	chainConfig := &params.ChainConfig{
		ChainID:             big.NewInt(1),
		HomesteadBlock:      big.NewInt(0),
		DAOForkBlock:        big.NewInt(0),
		DAOForkSupport:      true,
		EIP150Block:         big.NewInt(0),
		EIP150Hash:          common.HexToHash("0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0"),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       nil,
		Ethash:              nil,
	}

	// configure the genesis block
	genesisJSON := `{"config":{"chainId":1,"homesteadBlock":0,"daoForkBlock":0,"daoForkSupport":true,"eip150Block":0,"eip150Hash":"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0","eip155Block":0,"eip158Block":0,"byzantiumBlock":0,"constantinopleBlock":0,"petersburgBlock":0},"nonce":"0x0","timestamp":"0x0","extraData":"0x00","gasLimit":"0x5f5e100","difficulty":"0x0","mixHash":"0x0000000000000000000000000000000000000000000000000000000000000000","coinbase":"0x0000000000000000000000000000000000000000","alloc":{"751a0b96e1042bee789452ecb20253fba40dbe85":{"balance":"0x1000000000000000", "mcbalance": {"0x0000000000000000000000000000000000000000000000000000000000000000": 1000000000000000000}}, "0100000000000000000000000000000000000000": {"code": "0x730000000000000000000000000000000000000000301460806040526004361061004b5760003560e01c80631e01043914610050578063abb24ba014610092578063b6510bb3146100a9575b600080fd5b61007c6004803603602081101561006657600080fd5b8101908080359060200190929190505050610118565b6040518082815260200191505060405180910390f35b81801561009e57600080fd5b506100a761013b565b005b8180156100b557600080fd5b50610116600480360360808110156100cc57600080fd5b81019080803573ffffffffffffffffffffffffffffffffffffffff16906020019092919080359060200190929190803590602001909291908035906020019092919050505061013e565b005b60003073ffffffffffffffffffffffffffffffffffffffff1682905d9050919050565b5c565b8373ffffffffffffffffffffffffffffffffffffffff1681836108fc8690811502906040516000604051808303818888878c8af69550505050505015801561018a573d6000803e3d6000fd5b505050505056fea2646970667358221220ed2100d6623a884d196eceefabe5e03da4309a2562bb25262f3874f1acb31cd764736f6c634300060a0033", "balance": "0x0", "mcbalance": {}}},"number":"0x0","gasUsed":"0x0","parentHash":"0x0000000000000000000000000000000000000000000000000000000000000000"}`
	mcAbiJSON := `[{"inputs":[],"name":"enableMultiCoin","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"coinid","type":"uint256"}],"name":"getBalance","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address payable","name":"recipient","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"},{"internalType":"uint256","name":"coinid","type":"uint256"},{"internalType":"uint256","name":"amount2","type":"uint256"}],"name":"transfer","outputs":[],"stateMutability":"nonpayable","type":"function"}]`
	genesisKey := "0xabd71b35d559563fea757f0f5edbde286fb8c043105b15abb7cd57189306d7d1"

	bobKey, _ := coreth.NewKey(rand.Reader)
	genesisBlock := new(core.Genesis)
	err := json.Unmarshal([]byte(genesisJSON), genesisBlock)
	checkError(err)
	hk, _ := crypto.HexToECDSA(genesisKey[2:])
	genKey := coreth.NewKeyFromECDSA(hk)

	config.Genesis = genesisBlock
	// grab the control of block generation and disable auto uncle
	config.Miner.ManualMining = true
	config.Miner.ManualUncle = true

	// compile the smart contract
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = build.Default.GOPATH
	}
	counterSrc, err := filepath.Abs(gopath + "/src/github.com/ava-labs/coreth/examples/multicoin/mc_test.sol")
	checkError(err)
	contracts, err := compiler.CompileSolidity("", counterSrc)
	checkError(err)
	contract, _ := contracts[fmt.Sprintf("%s:%s", counterSrc, "MCTest")]

	// info required to generate a transaction
	chainID := chainConfig.ChainID
	nonce := uint64(0)
	gasLimit := 10000000
	gasPrice := big.NewInt(1000000000)

	blockCount := 0
	chain := coreth.NewETHChain(&config, nil, nil, nil)
	newTxPoolHeadChan := make(chan core.NewTxPoolHeadEvent, 1)
	log.Info(chain.GetGenesisBlock().Hash().Hex())

	mcAbi, err := abi.JSON(strings.NewReader(mcAbiJSON))
	checkError(err)
	enableCode, err := mcAbi.Pack("enableMultiCoin")
	checkError(err)

	abiStr, err := json.Marshal(contract.Info.AbiDefinition)
	contractAbi, err := abi.JSON(strings.NewReader(string(abiStr)))
	checkError(err)

	var contractAddr common.Address
	postGen := func(block *types.Block) bool {
		if blockCount == 15 {
			coin0 := common.HexToHash("0x0")
			state, err := chain.CurrentState()
			checkError(err)
			log.Info(fmt.Sprintf("genesis balance = %s", state.GetBalance(genKey.Address)))
			log.Info(fmt.Sprintf("genesis mcbalance(0) = %s", state.GetBalanceMultiCoin(genKey.Address, coin0)))
			log.Info(fmt.Sprintf("bob's balance = %s", state.GetBalance(bobKey.Address)))
			log.Info(fmt.Sprintf("bob's mcbalance(0) = %s", state.GetBalanceMultiCoin(bobKey.Address, coin0)))
			log.Info(fmt.Sprintf("contract mcbalance(0) = %s", state.GetBalanceMultiCoin(contractAddr, coin0)))
			log.Info(fmt.Sprintf("state = %s", state.Dump(true, false, true)))
			return true
		}
		if blockCount == 1 {
			receipts := chain.GetReceiptsByHash(block.Hash())
			if len(receipts) != 1 {
				panic(fmt.Sprintf("# receipts is %d != 1", len(receipts)))
			}
			contractAddr = receipts[0].ContractAddress
			log.Info(fmt.Sprintf("contract addr = %s", contractAddr.String()))

			go func() {
				// give Bob some initial balance
				tx := types.NewTransaction(nonce, bobKey.Address, big.NewInt(300000000000000000), uint64(gasLimit), gasPrice, nil)
				signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), genKey.PrivateKey)
				checkError(err)
				chain.AddRemoteTxs([]*types.Transaction{signedTx})
				nonce++
				time.Sleep(20 * time.Millisecond)

				// enable MC for Bob
				tx = types.NewTransaction(0, vm.BuiltinAddr, big.NewInt(0), uint64(gasLimit), gasPrice, enableCode)
				signedTx, err = types.SignTx(tx, types.NewEIP155Signer(chainID), bobKey.PrivateKey)
				checkError(err)
				chain.AddRemoteTxs([]*types.Transaction{signedTx})

				time.Sleep(20 * time.Millisecond)

				bobTransferInput, err := mcAbi.Pack("transfer", bobKey.Address, big.NewInt(0), big.NewInt(0), big.NewInt(100000000000000000))
				checkError(err)
				contractTransferInput, err := mcAbi.Pack("transfer", contractAddr, big.NewInt(0), big.NewInt(0), big.NewInt(100000000000000000))
				checkError(err)

				for i := 0; i < 5; i++ {
					// transfer some coin0 balance to Bob
					tx := types.NewTransaction(nonce, vm.BuiltinAddr, big.NewInt(0), uint64(gasLimit), gasPrice, bobTransferInput)
					signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), genKey.PrivateKey)
					checkError(err)
					chain.AddRemoteTxs([]*types.Transaction{signedTx})
					nonce++

					// transfer some coin0 balance to the contract
					tx = types.NewTransaction(nonce, vm.BuiltinAddr, big.NewInt(0), uint64(gasLimit), gasPrice, contractTransferInput)
					signedTx, err = types.SignTx(tx, types.NewEIP155Signer(chainID), genKey.PrivateKey)
					checkError(err)
					chain.AddRemoteTxs([]*types.Transaction{signedTx})
					nonce++
				}

				// test contract methods

				input, err := contractAbi.Pack("withdraw", big.NewInt(0), big.NewInt(0), big.NewInt(10000000000000000))
				tx = types.NewTransaction(nonce, contractAddr, big.NewInt(0), uint64(gasLimit), gasPrice, input)
				signedTx, err = types.SignTx(tx, types.NewEIP155Signer(chainID), genKey.PrivateKey)
				checkError(err)
				chain.AddRemoteTxs([]*types.Transaction{signedTx})
				nonce++

				input, err = contractAbi.Pack("updateBalance", big.NewInt(0))
				tx = types.NewTransaction(nonce, contractAddr, big.NewInt(0), uint64(gasLimit), gasPrice, input)
				signedTx, err = types.SignTx(tx, types.NewEIP155Signer(chainID), genKey.PrivateKey)
				checkError(err)
				chain.AddRemoteTxs([]*types.Transaction{signedTx})
				nonce++
			}()
		}
		return false
	}
	chain.SetOnHeaderNew(func(header *types.Header) {
		hid := make([]byte, 32)
		_, err := rand.Read(hid)
		if err != nil {
			panic("cannot generate hid")
		}
		header.Extra = append(header.Extra, hid...)
	})
	chain.SetOnSealFinish(func(block *types.Block) error {
		blockCount++
		if postGen(block) {
			return nil
		}
		go func() {
			<-newTxPoolHeadChan
			time.Sleep(10 * time.Millisecond)
			chain.GenBlock()
		}()
		return nil
	})

	// start the chain
	chain.GetTxPool().SubscribeNewHeadEvent(newTxPoolHeadChan)
	chain.Start()
	code := common.Hex2Bytes(contract.Code[2:])

	tx := types.NewContractCreation(nonce, big.NewInt(0), uint64(gasLimit), gasPrice, code)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), genKey.PrivateKey)
	checkError(err)
	chain.AddRemoteTxs([]*types.Transaction{signedTx})
	nonce++
	chain.GenBlock()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	signal.Notify(c, os.Interrupt, syscall.SIGINT)
	<-c
	chain.Stop()
}
