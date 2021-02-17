// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"

	"github.com/ava-labs/coreth"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

func checkError(err error) {
	if err != nil {
		panic(err)
	}
}

type TestChain struct {
	hasBlock    map[common.Hash]struct{}
	blocks      []common.Hash
	blkCount    uint32
	chain       *coreth.ETHChain
	parentBlock common.Hash
	outBlockCh  chan<- []byte
	blockWait   sync.WaitGroup
}

func (tc *TestChain) insertBlock(block *types.Block) {
	if _, ok := tc.hasBlock[block.Hash()]; !ok {
		tc.hasBlock[block.Hash()] = struct{}{}
		tc.blocks = append(tc.blocks, block.Hash())
	}
}

func NewTestChain(name string, config *eth.Config,
	inBlockCh <-chan []byte, outBlockCh chan<- []byte,
	inAckCh <-chan struct{}, outAckCh chan<- struct{}) *TestChain {
	tc := &TestChain{
		hasBlock:   make(map[common.Hash]struct{}),
		blocks:     make([]common.Hash, 0),
		blkCount:   0,
		chain:      coreth.NewETHChain(config, nil, nil, nil, eth.DefaultSettings),
		outBlockCh: outBlockCh,
	}
	tc.insertBlock(tc.chain.GetGenesisBlock())
	tc.chain.SetOnHeaderNew(func(header *types.Header) {
		hid := make([]byte, 32)
		_, err := rand.Read(hid)
		if err != nil {
			panic("cannot generate hid")
		}
		header.Extra = append(header.Extra, hid...)
	})
	tc.chain.SetOnSealFinish(func(block *types.Block) error {
		blkID := tc.blkCount
		tc.blkCount++
		if len(block.Uncles()) != 0 {
			panic("#uncles should be zero")
		}
		if tc.parentBlock != block.ParentHash() {
			panic("mismatching parent hash")
		}
		log.Info(fmt.Sprintf("%s: create %s <= (%d = %s)",
			name, tc.parentBlock.Hex(), blkID, block.Hash().Hex()))
		tc.insertBlock(block)
		if tc.outBlockCh != nil {
			serialized, err := rlp.EncodeToBytes(block)
			if err != nil {
				panic(err)
			}
			tc.outBlockCh <- serialized
			<-inAckCh
			log.Info(fmt.Sprintf("%s: got ack", name))
		}
		tc.blockWait.Done()
		return nil
	})
	go func() {
		for {
			select {
			case serialized := <-inBlockCh:
				block := new(types.Block)
				err := rlp.DecodeBytes(serialized, block)
				if err != nil {
					panic(err)
				}
				if !tc.chain.VerifyBlock(block) {
					panic("invalid block")
				}
				tc.chain.InsertChain([]*types.Block{block})
				tc.insertBlock(block)
				log.Info(fmt.Sprintf("%s: got block %s, sending ack", name, block.Hash().Hex()))
				outAckCh <- struct{}{}
			}
		}
	}()
	return tc
}

func (tc *TestChain) Start() {
	tc.chain.Start()
}

func (tc *TestChain) Stop() {
	tc.chain.Stop()
}

func (tc *TestChain) GenRandomTree(n int, max int) {
	for i := 0; i < n; i++ {
		nblocks := len(tc.blocks)
		m := max
		if m < 0 || nblocks < m {
			m = nblocks
		}
		pb, _ := rand.Int(rand.Reader, big.NewInt((int64)(m)))
		pn := pb.Int64()
		tc.parentBlock = tc.blocks[nblocks-1-(int)(pn)]
		tc.chain.SetTail(tc.parentBlock)
		tc.blockWait.Add(1)
		tc.chain.GenBlock()
		tc.blockWait.Wait()
	}
}

func run(config *eth.Config, a1, a2, b1, b2 int) {
	aliceBlk := make(chan []byte)
	bobBlk := make(chan []byte)
	aliceAck := make(chan struct{})
	bobAck := make(chan struct{})
	alice := NewTestChain("alice", config, bobBlk, aliceBlk, bobAck, aliceAck)
	bob := NewTestChain("bob", config, aliceBlk, bobBlk, aliceAck, bobAck)
	alice.Start()
	bob.Start()
	log.Info("alice genesis", "block", alice.chain.GetGenesisBlock().Hash().Hex())
	log.Info("bob genesis", "block", bob.chain.GetGenesisBlock().Hash().Hex())
	alice.GenRandomTree(a1, a2)
	log.Info("alice finished generating the tree")
	//time.Sleep(1 * time.Second)
	bob.outBlockCh = nil
	bob.GenRandomTree(b1, b2)
	//mrand.Shuffle(len(bob.blocks),
	//	func(i, j int) { bob.blocks[i], bob.blocks[j] = bob.blocks[j], bob.blocks[i] })
	log.Info("bob finished generating the tree")
	//time.Sleep(1 * time.Second)
	log.Info("bob sends out all its blocks")
	for i := range bob.blocks {
		serialized, err := rlp.EncodeToBytes(bob.chain.GetBlockByHash(bob.blocks[i]))
		if err != nil {
			panic(err)
		}
		bobBlk <- serialized
		<-aliceAck
		log.Info(fmt.Sprintf("bob: got ack"))
	}
	log.Info("bob finished generating the tree")
	//time.Sleep(1 * time.Second)
	log.Info("comparing two trees")
	if len(alice.blocks) != len(bob.blocks) {
		panic(fmt.Sprintf("mismatching tree size %d != %d", len(alice.blocks), len(bob.blocks)))
	}
	gn := big.NewInt(0)
	for i := range alice.blocks {
		ablk := alice.chain.GetBlockByHash(alice.blocks[i])
		bblk := bob.chain.GetBlockByHash(alice.blocks[i])
		for ablk.Number().Cmp(gn) > 0 && bblk.Number().Cmp(gn) > 0 {
			result := ablk.Hash() == bblk.Hash()
			opsign := "=="
			if !result {
				opsign = "!="
			}
			log.Info(fmt.Sprintf("alice(%d = %s) %s bob (%d = %s)",
				ablk.Number(), ablk.Hash().Hex(),
				opsign,
				bblk.Number(), bblk.Hash().Hex()))
			if !result {
				panic("mismatching path")
			}
			ablk = alice.chain.GetBlockByHash(ablk.ParentHash())
			bblk = bob.chain.GetBlockByHash(bblk.ParentHash())
		}
		log.Info(fmt.Sprintf("%s ok", alice.blocks[i].Hex()))
	}
	alice.Stop()
	bob.Stop()
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

	run(&config, 60, 1, 60, 1)
	run(&config, 500, 10, 500, 5)
}
