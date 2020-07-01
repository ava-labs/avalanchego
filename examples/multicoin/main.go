package main

import (
	"crypto/rand"
	//"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ava-labs/coreth"
	"github.com/ava-labs/coreth/accounts/abi"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/go-ethereum/common"
	//"github.com/ava-labs/go-ethereum/common/compiler"
	"github.com/ava-labs/go-ethereum/crypto"
	"github.com/ava-labs/go-ethereum/log"
	"go/build"
	"math/big"
	"os"
	"os/signal"
	//"path/filepath"
	"strings"
	"syscall"
	"time"
)

var (
	mcLibCode       = "6101c7610026600b82828239805160001a60731461001957fe5b30600052607381538281f3fe730000000000000000000000000000000000000000301460806040526004361061004b5760003560e01c80631e01043914610050578063abb24ba014610092578063b6510bb3146100a9575b600080fd5b61007c6004803603602081101561006657600080fd5b8101908080359060200190929190505050610118565b6040518082815260200191505060405180910390f35b81801561009e57600080fd5b506100a761013b565b005b8180156100b557600080fd5b50610116600480360360808110156100cc57600080fd5b81019080803573ffffffffffffffffffffffffffffffffffffffff16906020019092919080359060200190929190803590602001909291908035906020019092919050505061013e565b005b60003373ffffffffffffffffffffffffffffffffffffffff1682905d9050919050565b5c565b8373ffffffffffffffffffffffffffffffffffffffff1681836108fc8690811502906040516000604051808303818888878c8af69550505050505015801561018a573d6000803e3d6000fd5b505050505056fea2646970667358221220477c0e55c25708d36d55abae8a51496c16bc5d28bc1ee6a9963c304afbacdc3464736f6c634300060a0033"
	mcLibAddr       = "0xDCb165540502E5d32F89382175150013D6644A69"
	testContract    = "608060405234801561001057600080fd5b5073dcb165540502e5d32f89382175150013d6644a6963abb24ba06040518163ffffffff1660e01b815260040160006040518083038186803b15801561005557600080fd5b505af4158015610069573d6000803e3d6000fd5b505050506101528061007c6000396000f3fe6080604052600436106100295760003560e01c80631e0104391461002e578063d0e30db01461007d575b600080fd5b34801561003a57600080fd5b506100676004803603602081101561005157600080fd5b8101908080359060200190929190505050610087565b6040518082815260200191505060405180910390f35b61008561011a565b005b600073dcb165540502e5d32f89382175150013d6644a69631e010439836040518263ffffffff1660e01b81526004018082815260200191505060206040518083038186803b1580156100d857600080fd5b505af41580156100ec573d6000803e3d6000fd5b505050506040513d602081101561010257600080fd5b81019080805190602001909291905050509050919050565b56fea26469706673582212204e41dc04c9409d8c06804429d6a03f2d6bca46de6073522300ea08f8ed4a90ac64736f6c634300060a0033"
	testContractABI = `[{"inputs":[],"stateMutability":"nonpayable","type":"constructor"},{"inputs":[],"name":"deposit","outputs":[],"stateMutability":"payable","type":"function"},{"inputs":[{"internalType":"uint256","name":"coinid","type":"uint256"}],"name":"getBalance","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`
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
	//genBalance := big.NewInt(100000000000000000)
	genKey, _ := coreth.NewKey(rand.Reader)
	bob, _ := coreth.NewKey(rand.Reader)

	g := new(core.Genesis)
	b := `{"config":{"chainId":1,"homesteadBlock":0,"daoForkBlock":0,"daoForkSupport":true,"eip150Block":0,"eip150Hash":"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0","eip155Block":0,"eip158Block":0,"byzantiumBlock":0,"constantinopleBlock":0,"petersburgBlock":0},"nonce":"0x0","timestamp":"0x0","extraData":"0x00","gasLimit":"0x5f5e100","difficulty":"0x0","mixHash":"0x0000000000000000000000000000000000000000000000000000000000000000","coinbase":"0x0000000000000000000000000000000000000000","alloc":{"751a0b96e1042bee789452ecb20253fba40dbe85":{"balance":"0x1000000000000000", "mcbalance": {"0x0000000000000000000000000000000000000000000000000000000000000000": 1000000000000000000}}},"number":"0x0","gasUsed":"0x0","parentHash":"0x0000000000000000000000000000000000000000000000000000000000000000"}`
	k := "0xabd71b35d559563fea757f0f5edbde286fb8c043105b15abb7cd57189306d7d1"
	err := json.Unmarshal([]byte(b), g)
	checkError(err)
	config.Genesis = g
	hk, _ := crypto.HexToECDSA(k[2:])
	genKey = coreth.NewKeyFromECDSA(hk)
	//config.Genesis = &core.Genesis{
	//	Config:     chainConfig,
	//	Nonce:      0,
	//	Number:     0,
	//	ExtraData:  hexutil.MustDecode("0x00"),
	//	GasLimit:   100000000,
	//	Difficulty: big.NewInt(0),
	//	Alloc:      core.GenesisAlloc{genKey.Address: {Balance: genBalance}},
	//}

	// grab the control of block generation and disable auto uncle
	config.Miner.ManualMining = true
	config.Miner.ManualUncle = true

	// compile the smart contract
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = build.Default.GOPATH
	}

	// info required to generate a transaction
	chainID := chainConfig.ChainID
	nonce := uint64(0)
	gasLimit := 10000000
	gasPrice := big.NewInt(1000000000)

	blockCount := 0
	chain := coreth.NewETHChain(&config, nil, nil, nil)
	newTxPoolHeadChan := make(chan core.NewTxPoolHeadEvent, 1)
	log.Info(chain.GetGenesisBlock().Hash().Hex())
	var contractAddr common.Address
	var contractAddr1 common.Address
	coin0 := common.HexToHash("0x0")
	postGen := func(block *types.Block) bool {
		if blockCount == 15 {
			state, err := chain.CurrentState()
			checkError(err)
			log.Info(fmt.Sprintf("genesis balance = %s", state.GetBalance(genKey.Address)))
			log.Info(fmt.Sprintf("genesis balance2 = %s", state.GetBalanceMultiCoin(genKey.Address, coin0)))
			log.Info(fmt.Sprintf("contract balance2 = %s", state.GetBalanceMultiCoin(contractAddr1, coin0)))
			log.Info(fmt.Sprintf("bob's balance = %s", state.GetBalance(bob.Address)))
			log.Info(fmt.Sprintf("bob's balance2 = %s", state.GetBalanceMultiCoin(bob.Address, coin0)))
			log.Info(fmt.Sprintf("state = %s", state.Dump(true, false, true)))
			log.Info(fmt.Sprintf("x = %s", state.GetState(contractAddr, common.BigToHash(big.NewInt(0))).String()))
			return true
		}
		if blockCount == 1 {
			receipts := chain.GetReceiptsByHash(block.Hash())
			if len(receipts) != 1 {
				panic(fmt.Sprintf("# receipts is %d != 1", len(receipts)))
			}
			contractAddr = receipts[0].ContractAddress
			txHash := receipts[0].TxHash
			log.Info(fmt.Sprintf("deploy tx = %s", txHash.String()))
			log.Info(fmt.Sprintf("contract addr = %s", contractAddr.String()))
			code := common.Hex2Bytes(testContract)
			tx := types.NewContractCreation(nonce, big.NewInt(0), uint64(gasLimit), gasPrice, code)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), genKey.PrivateKey)
			checkError(err)
			chain.AddRemoteTxs([]*types.Transaction{signedTx})
			nonce++
		} else if blockCount == 2 {
			receipts := chain.GetReceiptsByHash(block.Hash())
			if len(receipts) != 1 {
				panic(fmt.Sprintf("# receipts is %d != 1", len(receipts)))
			}
			contractAddr1 = receipts[0].ContractAddress
			fmt.Println("contract", contractAddr1.Hex())

			//call := common.Hex2Bytes("1003e2d20000000000000000000000000000000000000000000000000000000000000001")
			//log.Info(fmt.Sprintf("code = %s", hex.EncodeToString(state.GetCode(contractAddr))))
			go func() {
				tx := types.NewTransaction(nonce, bob.Address, big.NewInt(300000000000000000), uint64(gasLimit), gasPrice, nil)
				signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), genKey.PrivateKey)
				checkError(err)
				chain.AddRemoteTxs([]*types.Transaction{signedTx})
				nonce++
				time.Sleep(20 * time.Millisecond)

				// self-loop tx to enable MC
				tx = types.NewTransaction(0, bob.Address, big.NewInt(1), uint64(gasLimit), gasPrice, nil)
				tx.SetMultiCoinValue(&coin0, big.NewInt(0))
				signedTx, err = types.SignTx(tx, types.NewEIP155Signer(chainID), bob.PrivateKey)
				checkError(err)
				chain.AddRemoteTxs([]*types.Transaction{signedTx})
				time.Sleep(20 * time.Millisecond)

				var code []byte
				abi, err := abi.JSON(strings.NewReader(testContractABI))
				checkError(err)
				code, err = abi.Pack("deposit")
				checkError(err)

				for i := 0; i < 5; i++ {
					tx := types.NewTransaction(nonce, bob.Address, big.NewInt(0), uint64(gasLimit), gasPrice, nil)
					tx.SetMultiCoinValue(&coin0, big.NewInt(100000000000000000))
					signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), genKey.PrivateKey)
					checkError(err)
					chain.AddRemoteTxs([]*types.Transaction{signedTx})
					nonce++

					tx = types.NewTransaction(nonce, contractAddr1, big.NewInt(0), uint64(gasLimit), gasPrice, code)
					tx.SetMultiCoinValue(&coin0, big.NewInt(100000000000000000))
					signedTx, err = types.SignTx(tx, types.NewEIP155Signer(chainID), genKey.PrivateKey)
					checkError(err)
					chain.AddRemoteTxs([]*types.Transaction{signedTx})
					nonce++
				}
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

	//cc, err := abi.Pack("enableMultiCoin")
	//checkError(err)
	//calls = append(calls, cc)
	//cc, err = abi.Pack("getBalance", big.NewInt(0))
	//checkError(err)
	//calls = append(calls, cc)

	code := common.Hex2Bytes(mcLibCode)
	tx := types.NewContractCreation(nonce, big.NewInt(0), uint64(gasLimit), gasPrice, code)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), genKey.PrivateKey)
	checkError(err)
	chain.AddRemoteTxs([]*types.Transaction{signedTx})
	nonce++
	//counterSrc, err := filepath.Abs(gopath + "/src/github.com/ava-labs/coreth/examples/counter/counter.sol")
	//checkError(err)
	//contracts, err := compiler.CompileSolidity("", counterSrc)
	//checkError(err)
	//contract, _ := contracts[fmt.Sprintf("%s:%s", counterSrc, "Counter")]
	//code := common.Hex2Bytes(contract.Code[2:])

	time.Sleep(10 * time.Millisecond)
	chain.GenBlock()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	signal.Notify(c, os.Interrupt, syscall.SIGINT)
	<-c
	chain.Stop()
}
