// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	cryptorand "crypto/rand"
	"math/big"
	"math/rand"
	"testing"

	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/coreth/accounts/keystore"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/eth/ethconfig"
	"github.com/ava-labs/coreth/node"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

type testChain struct {
	t          *testing.T
	name       string
	hasBlock   map[common.Hash]struct{}
	blocks     []common.Hash
	blkCount   uint32
	chain      *ETHChain
	outBlockCh chan<- []byte
	inAckCh    <-chan struct{}
}

func (tc *testChain) insertBlock(block *types.Block) {
	if _, ok := tc.hasBlock[block.Hash()]; !ok {
		tc.hasBlock[block.Hash()] = struct{}{}
		tc.blocks = append(tc.blocks, block.Hash())
	}
}

func newTestChain(name string, config *eth.Config,
	inBlockCh <-chan []byte, outBlockCh chan<- []byte,
	inAckCh <-chan struct{}, outAckCh chan<- struct{},
	t *testing.T) *testChain {
	chain, err := NewETHChain(
		config,
		&node.Config{},
		rawdb.NewMemoryDatabase(),
		eth.DefaultSettings,
		&dummy.ConsensusCallbacks{
			OnFinalizeAndAssemble: func(head *types.Header, _ *state.StateDB, _ []*types.Transaction) ([]byte, *big.Int, *big.Int, error) {
				randData := make([]byte, 32)
				_, err := rand.Read(randData)
				if err != nil {
					t.Fatal(err)
				}
				return randData, nil, nil, nil
			},
		},
		common.Hash{},
		&mockable.Clock{},
	)
	if err != nil {
		t.Fatal(err)
	}
	tc := &testChain{
		t:          t,
		name:       name,
		hasBlock:   make(map[common.Hash]struct{}),
		blocks:     make([]common.Hash, 0),
		blkCount:   0,
		chain:      chain,
		outBlockCh: outBlockCh,
		inAckCh:    inAckCh,
	}
	tc.insertBlock(tc.chain.GetGenesisBlock())
	// Start a goroutine to deserialize and insert each block received from [inBlockCh]
	// and send an acknowledgement via [outAckCh]
	go func() {
		for serialized := range inBlockCh {
			block := new(types.Block)
			err := rlp.DecodeBytes(serialized, block)
			if err != nil {
				panic(err)
			}
			if block.Hash() != tc.chain.GetGenesisBlock().Hash() {
				if err = tc.chain.InsertBlock(block); err != nil {
					tc.t.Fatalf("Failed to insert block for chain %q: %s", name, err)
				}
			}
			tc.insertBlock(block)
			outAckCh <- struct{}{}
		}
	}()
	return tc
}

func (tc *testChain) start() {
	tc.chain.Start()
}

func (tc *testChain) stop() {
	tc.chain.Stop()
}

func (tc *testChain) GenRandomTree(genBlocks int) {
	for i := 0; i < genBlocks; i++ {
		numBlocks := len(tc.blocks)
		parentIndex := rand.Intn(numBlocks)
		parentBlockHash := tc.blocks[parentIndex]
		parentBlock := tc.chain.GetBlockByHash(parentBlockHash)
		if parentBlock == nil {
			tc.t.Fatalf("Failed to get parent block by hash %s, %d", parentBlockHash, numBlocks)
		}
		if err := tc.chain.SetPreference(parentBlock); err != nil {
			tc.t.Fatal(err)
		}
		block, err := tc.chain.GenerateBlock()
		if err != nil {
			tc.t.Fatalf("chain %s failed to generate block: %s", tc.name, err)
		}
		if err := tc.chain.InsertBlock(block); err != nil {
			tc.t.Fatal(err)
		}

		tc.blkCount++
		if len(block.Uncles()) != 0 {
			tc.t.Fatal("#uncles should be zero")
		}
		tc.insertBlock(block)
		if tc.outBlockCh != nil {
			serialized, err := rlp.EncodeToBytes(block)
			if err != nil {
				tc.t.Fatal(err)
			}
			tc.outBlockCh <- serialized
			<-tc.inAckCh
		}
	}
}

func run(config *eth.Config, size1, size2 int, t *testing.T) {
	aliceBlk := make(chan []byte)
	bobBlk := make(chan []byte)
	aliceAck := make(chan struct{})
	bobAck := make(chan struct{})
	alice := newTestChain("alice", config, bobBlk, aliceBlk, bobAck, aliceAck, t)
	bob := newTestChain("bob", config, aliceBlk, bobBlk, aliceAck, bobAck, t)
	alice.start()
	bob.start()
	log.Info("alice genesis", "block", alice.chain.GetGenesisBlock().Hash().Hex())
	log.Info("bob genesis", "block", bob.chain.GetGenesisBlock().Hash().Hex())
	alice.GenRandomTree(size1)
	log.Info("alice finished generating the tree")

	bob.outBlockCh = nil
	bob.GenRandomTree(size2)
	for i := range bob.blocks {
		serialized, err := rlp.EncodeToBytes(bob.chain.GetBlockByHash(bob.blocks[i]))
		if err != nil {
			t.Fatal(err)
		}
		bobBlk <- serialized
		<-aliceAck
	}
	log.Info("bob finished generating the tree")

	log.Info("comparing two trees")
	if len(alice.blocks) != len(bob.blocks) {
		t.Fatalf("mismatching tree size %d != %d", len(alice.blocks), len(bob.blocks))
	}
	gn := big.NewInt(0)
	for i := range alice.blocks {
		ablk := alice.chain.GetBlockByHash(alice.blocks[i])
		bblk := bob.chain.GetBlockByHash(alice.blocks[i])
		for ablk.Number().Cmp(gn) > 0 && bblk.Number().Cmp(gn) > 0 {
			result := ablk.Hash() == bblk.Hash()
			if !result {
				t.Fatal("mismatching path")
			}
			ablk = alice.chain.GetBlockByHash(ablk.ParentHash())
			bblk = bob.chain.GetBlockByHash(bblk.ParentHash())
		}
	}
	alice.stop()
	bob.stop()
}

// TestChain randomly generates a chain (tree of blocks) on each of two
// entities ("Alice" and "Bob") and lets them exchange each other's blocks via
// a go channel and finally checks if they have the identical chain structure.
func TestChain(t *testing.T) {
	// configure the chain
	config := ethconfig.DefaultConfig
	chainConfig := &params.ChainConfig{
		ChainID:                     big.NewInt(1),
		HomesteadBlock:              big.NewInt(0),
		DAOForkBlock:                big.NewInt(0),
		DAOForkSupport:              true,
		EIP150Block:                 big.NewInt(0),
		EIP150Hash:                  common.HexToHash("0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0"),
		EIP155Block:                 big.NewInt(0),
		EIP158Block:                 big.NewInt(0),
		ByzantiumBlock:              big.NewInt(0),
		ConstantinopleBlock:         big.NewInt(0),
		PetersburgBlock:             big.NewInt(0),
		IstanbulBlock:               big.NewInt(0),
		ApricotPhase1BlockTimestamp: big.NewInt(0),
	}

	// configure the genesis block
	genBalance := big.NewInt(100000000000000000)
	genKey, _ := keystore.NewKey(cryptorand.Reader)

	config.Genesis = &core.Genesis{
		Config:     chainConfig,
		Nonce:      0,
		Number:     0,
		ExtraData:  hexutil.MustDecode("0x00"),
		GasLimit:   100000000,
		Difficulty: big.NewInt(0),
		Alloc:      core.GenesisAlloc{genKey.Address: {Balance: genBalance}},
	}

	run(&config, 20, 20, t)
}
