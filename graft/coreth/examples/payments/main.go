// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"crypto/rand"
	"fmt"
	"math/big"

	"github.com/ava-labs/coreth"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
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
		IstanbulBlock:       big.NewInt(0),
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
	config.Miner.DisableUncle = true

	// info required to generate a transaction
	chainID := chainConfig.ChainID
	nonce := uint64(0)
	value := big.NewInt(1000000000000)
	gasLimit := 21000
	gasPrice := big.NewInt(1000000000)
	bob, err := coreth.NewKey(rand.Reader)
	checkError(err)

	chain := coreth.NewETHChain(&config, nil, nil, nil, eth.DefaultSettings)
	showBalance := func() {
		state, err := chain.CurrentState()
		checkError(err)
		log.Info(fmt.Sprintf("genesis balance = %s", state.GetBalance(genKey.Address)))
		log.Info(fmt.Sprintf("bob's balance = %s", state.GetBalance(bob.Address)))
	}
	chain.SetOnHeaderNew(func(header *types.Header) {
		hid := make([]byte, 32)
		_, err := rand.Read(hid)
		if err != nil {
			panic("cannot generate hid")
		}
		header.Extra = append(header.Extra, hid...)
	})
	newBlockChan := make(chan *types.Block)
	newTxPoolHeadChan := make(chan core.NewTxPoolHeadEvent, 1)
	chain.SetOnSealFinish(func(block *types.Block) error {
		newBlockChan <- block
		return nil
	})

	chain.GetTxPool().SubscribeNewHeadEvent(newTxPoolHeadChan)
	// start the chain
	chain.Start()
	for i := 0; i < 42; i++ {
		tx := types.NewTransaction(nonce, bob.Address, value, uint64(gasLimit), gasPrice, nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), genKey.PrivateKey)
		checkError(err)
		chain.AddRemoteTxs([]*types.Transaction{signedTx})
		nonce++
		chain.GenBlock()
		block := <-newBlockChan
		<-newTxPoolHeadChan
		log.Info("finished generating block, starting the next iteration", "height", block.Number())
	}
	showBalance()
	chain.Stop()
}
