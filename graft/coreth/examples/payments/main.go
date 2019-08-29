package main

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"os/signal"
	"syscall"
	"time"
	//"encoding/hex"
	"github.com/ava-labs/coreth"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/go-ethereum/common"
	"github.com/ava-labs/go-ethereum/common/hexutil"
	"github.com/ava-labs/go-ethereum/core"
	"github.com/ava-labs/go-ethereum/core/types"
	"github.com/ava-labs/go-ethereum/log"
	"github.com/ava-labs/go-ethereum/params"
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
		IstanbulBlock:       nil,
		Ethash:              nil,
	}

	// configure the genesis block
	genBalance := big.NewInt(100000000000000000)
	genKey, _ := coreth.NewKey(rand.Reader)

	config.Genesis = &core.Genesis{
		Config:     chainConfig,
		Nonce:      0,
		Number:     0,
		ExtraData:  hexutil.MustDecode("0x00"),
		GasLimit:   100000000,
		Difficulty: big.NewInt(0),
		Alloc:      core.GenesisAlloc{genKey.Address: {Balance: genBalance}},
	}

	// grab the control of block generation and disable auto uncle
	config.Miner.ManualMining = true
	config.Miner.ManualUncle = true

	// info required to generate a transaction
	chainID := chainConfig.ChainID
	nonce := uint64(0)
	value := big.NewInt(1000000000000)
	gasLimit := 21000
	gasPrice := big.NewInt(1000000000)
	bob, err := coreth.NewKey(rand.Reader)
	checkError(err)

	blockCount := 0
	chain := coreth.NewETHChain(&config, nil)
	showBalance := func() {
		state, err := chain.CurrentState()
		checkError(err)
		log.Info(fmt.Sprintf("genesis balance = %s", state.GetBalance(genKey.Address)))
		log.Info(fmt.Sprintf("bob's balance = %s", state.GetBalance(bob.Address)))
	}
	chain.SetOnSeal(func(block *types.Block) error {
		go func() {
			// the minimum time gap is 1s
			time.Sleep(1000 * time.Millisecond)
			// generate 15 blocks
			blockCount++
			if blockCount == 15 {
				showBalance()
				return
			}
			chain.GenBlock()
		}()
		return nil
	})

	// start the chain
	chain.Start()
	chain.GenBlock()
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(nonce, bob.Address, value, uint64(gasLimit), gasPrice, nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), genKey.PrivateKey)
		checkError(err)
		_ = signedTx
		chain.AddRemoteTxs([]*types.Transaction{signedTx})
		time.Sleep(1000 * time.Millisecond)
		nonce++
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	signal.Notify(c, os.Interrupt, syscall.SIGINT)
	<-c
	chain.Stop()
}
