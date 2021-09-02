// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/accounts/keystore"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/eth/ethconfig"
	"github.com/ava-labs/coreth/node"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

var (
	basicTxGasLimit       = 21000
	fundedKey, bob, alice *keystore.Key
	initialBalance        = big.NewInt(1000000000000000000)
	chainID               = big.NewInt(1)
	value                 = big.NewInt(1000000000000)
	gasLimit              = 1000000
	gasPrice              = big.NewInt(params.LaunchMinGasPrice)
)

func init() {
	genKey, err := keystore.NewKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	fundedKey = genKey
	genKey, err = keystore.NewKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	bob = genKey
	genKey, err = keystore.NewKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	alice = genKey
}

func NewDefaultChain(t *testing.T) (*ETHChain, chan core.NewTxPoolHeadEvent, <-chan core.NewTxsEvent) {
	// configure the chain
	config := ethconfig.NewDefaultConfig()
	chainConfig := &params.ChainConfig{
		ChainID:             chainID,
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
	}

	config.Genesis = &core.Genesis{
		Config:     chainConfig,
		Nonce:      0,
		Number:     0,
		ExtraData:  hexutil.MustDecode("0x00"),
		GasLimit:   100000000,
		Difficulty: big.NewInt(0),
		Alloc:      core.GenesisAlloc{fundedKey.Address: {Balance: initialBalance}},
	}

	var (
		chain *ETHChain
		err   error
	)
	chain, err = NewETHChain(
		&config,
		&node.Config{},
		rawdb.NewMemoryDatabase(),
		eth.DefaultSettings,
		new(dummy.ConsensusCallbacks),
		common.Hash{},
	)
	if err != nil {
		t.Fatal(err)
	}

	newTxPoolHeadChan := make(chan core.NewTxPoolHeadEvent, 1)
	chain.GetTxPool().SubscribeNewHeadEvent(newTxPoolHeadChan)

	txSubmitCh := chain.GetTxSubmitCh()
	return chain, newTxPoolHeadChan, txSubmitCh
}

// insertAndAccept inserts [block] into [chain], sets the chains preference to it
// and then Accepts it.
func insertAndAccept(t *testing.T, chain *ETHChain, block *types.Block) {
	if err := chain.InsertBlock(block); err != nil {
		t.Fatal(err)
	}
	if err := chain.SetPreference(block); err != nil {
		t.Fatal(err)
	}
	if err := chain.Accept(block); err != nil {
		t.Fatal(err)
	}
}

func insertAndSetPreference(t *testing.T, chain *ETHChain, block *types.Block) {
	if err := chain.InsertBlock(block); err != nil {
		t.Fatal(err)
	}
	if err := chain.SetPreference(block); err != nil {
		t.Fatal(err)
	}
}
