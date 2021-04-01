// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// NOTE from Ted: make sure your solc<0.8.0, as geth 1.9.21 does not support
// the JSON output from solc>=0.8.0:
// See:
// - https://github.com/ethereum/go-ethereum/issues/22041
// - https://github.com/ethereum/go-ethereum/pull/22092

package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/build"
	"math/big"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ava-labs/coreth/accounts/keystore"

	"github.com/ava-labs/coreth"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/compiler"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

func checkError(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	// configure the chain
	config := eth.DefaultConfig
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
		IstanbulBlock:       big.NewInt(0),
		Ethash:              nil,
	}

	// configure the genesis block
	//genBalance := big.NewInt(100000000000000000)
	genKey, _ := keystore.NewKey(rand.Reader)

	g := new(core.Genesis)
	b := `{"config":{"chainId":1,"homesteadBlock":0,"daoForkBlock":0,"daoForkSupport":true,"eip150Block":0,"eip150Hash":"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0","eip155Block":0,"eip158Block":0,"byzantiumBlock":0,"constantinopleBlock":0,"petersburgBlock":0},"nonce":"0x0","timestamp":"0x0","extraData":"0x00","gasLimit":"0x5f5e100","difficulty":"0x0","mixHash":"0x0000000000000000000000000000000000000000000000000000000000000000","coinbase":"0x0000000000000000000000000000000000000000","alloc":{"751a0b96e1042bee789452ecb20253fba40dbe85":{"balance":"0x16345785d8a0000"}},"number":"0x0","gasUsed":"0x0","parentHash":"0x0000000000000000000000000000000000000000000000000000000000000000"}`
	k := "0xabd71b35d559563fea757f0f5edbde286fb8c043105b15abb7cd57189306d7d1"
	err := json.Unmarshal([]byte(b), g)
	checkError(err)
	config.Genesis = g
	hk, _ := crypto.HexToECDSA(k[2:])
	genKey = keystore.NewKeyFromECDSA(hk)
	//config.Genesis = &core.Genesis{
	//	Config:     chainConfig,
	//	Nonce:      0,
	//	Number:     0,
	//	ExtraData:  hexutil.MustDecode("0x00"),
	//	GasLimit:   100000000,
	//	Difficulty: big.NewInt(0),
	//	Alloc:      core.GenesisAlloc{genKey.Address: {Balance: genBalance}},
	//}

	// grab the control of block generation
	config.Miner.ManualMining = true

	// compile the smart contract
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = build.Default.GOPATH
	}
	counterSrc, err := filepath.Abs(gopath + "/src/github.com/ava-labs/coreth/examples/counter/counter.sol")
	checkError(err)
	contracts, err := compiler.CompileSolidity("", counterSrc)
	checkError(err)
	contract, _ := contracts[fmt.Sprintf("%s:%s", counterSrc, "Counter")]

	// info required to generate a transaction
	chainID := chainConfig.ChainID
	nonce := uint64(0)
	gasLimit := 10000000
	gasPrice := big.NewInt(1000000000)

	blockCount := 0
	chain := coreth.NewETHChain(&config, nil, nil, nil, eth.DefaultSettings)
	newTxPoolHeadChan := make(chan core.NewTxPoolHeadEvent, 1)
	log.Info(chain.GetGenesisBlock().Hash().Hex())
	firstBlock := false
	var contractAddr common.Address
	postGen := func(block *types.Block) bool {
		if blockCount == 15 {
			state, err := chain.CurrentState()
			checkError(err)
			log.Info(fmt.Sprintf("genesis balance = %s", state.GetBalance(genKey.Address)))
			log.Info(fmt.Sprintf("contract balance = %s", state.GetBalance(contractAddr)))
			log.Info(fmt.Sprintf("state = %s", state.Dump(true, false, true)))
			log.Info(fmt.Sprintf("x = %s", state.GetState(contractAddr, common.BigToHash(big.NewInt(0))).String()))
			return true
		}
		if !firstBlock {
			firstBlock = true
			receipts := chain.GetReceiptsByHash(block.Hash())
			if len(receipts) != 1 {
				panic("# receipts is not 1")
			}
			contractAddr = receipts[0].ContractAddress
			txHash := receipts[0].TxHash
			log.Info(fmt.Sprintf("deploy tx = %s", txHash.String()))
			log.Info(fmt.Sprintf("contract addr = %s", contractAddr.String()))
			call := common.Hex2Bytes("1003e2d20000000000000000000000000000000000000000000000000000000000000001")
			state, _ := chain.CurrentState()
			log.Info(fmt.Sprintf("code = %s", hex.EncodeToString(state.GetCode(contractAddr))))
			go func() {
				for i := 0; i < 10; i++ {
					tx := types.NewTransaction(nonce, contractAddr, big.NewInt(0), uint64(gasLimit), gasPrice, call)
					signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), genKey.PrivateKey)
					checkError(err)
					chain.AddRemoteTxs([]*types.Transaction{signedTx})
					nonce++
				}
			}()
		}
		return false
	}
	chain.SetOnSealFinish(func(block *types.Block) error {
		chain.SetPreference(block)
		blockCount++
		if postGen(block) {
			return nil
		}
		go func() {
			<-newTxPoolHeadChan
			chain.GenBlock()
		}()
		return nil
	})

	// start the chain
	chain.GetTxPool().SubscribeNewHeadEvent(newTxPoolHeadChan)
	chain.Start()
	chain.BlockChain().UnlockIndexing()
	chain.SetPreference(chain.GetGenesisBlock())

	_ = contract
	code := common.Hex2Bytes(contract.Code[2:])
	tx := types.NewContractCreation(nonce, big.NewInt(0), uint64(gasLimit), gasPrice, code)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), genKey.PrivateKey)
	checkError(err)
	chain.AddRemoteTxs([]*types.Transaction{signedTx})
	time.Sleep(1000 * time.Millisecond)
	nonce++

	chain.GenBlock()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	signal.Notify(c, os.Interrupt, syscall.SIGINT)
	<-c
	chain.Stop()
}
