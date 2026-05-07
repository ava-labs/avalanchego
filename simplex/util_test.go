// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"testing"
	"time"

	"github.com/ava-labs/simplex"
	"github.com/ava-labs/simplex/wal"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
	"github.com/ava-labs/avalanchego/snow/networking/sender/sendermock"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/logging"

	simplexparams "github.com/ava-labs/avalanchego/snow/consensus/simplex"
)

var (
	cachedBLSKey       *localsigner.LocalSigner
	cachedCompressedPK []byte
)

type keyReuseOption bool

const (
	noKeyReuse keyReuseOption = false
	reuseKeys  keyReuseOption = true
)

func init() {
	var err error
	cachedBLSKey, err = localsigner.New()
	if err != nil {
		panic("failed to generate cached BLS key: " + err.Error())
	}
	cachedCompressedPK = cachedBLSKey.PublicKey().Compress()
}

type testNodeConfig struct {
	reuseKeys keyReuseOption
}

type testNodeConfigOption func(*testNodeConfig)

func testNodeConfigWithKeyReuse(cfg *testNodeConfig) {
	cfg.reuseKeys = reuseKeys
}

type newBlockConfig struct {
	// If prev is nil, newBlock will create the genesis block
	prev *Block
	// If round is 0, it will be set to one higher than the prev's round
	round uint64
	// genesis nodes
	numNodes uint64
}

func newTestBlock(t *testing.T, config newBlockConfig) *Block {
	if config.prev == nil {
		vm := newTestVM()
		block := &Block{
			blacklist: simplex.NewBlacklist(uint16(config.numNodes)),
			vmBlock: &wrappedBlock{
				Block: snowmantest.Genesis,
				vm:    vm,
			},
			metadata: genesisMetadata,
		}
		bytes, err := block.Bytes()
		require.NoError(t, err)

		digest := computeDigest(bytes)
		block.digest = digest

		bt := newBlockTracker(vm)
		bt.init(block)
		block.blockTracker = bt
		return block
	}
	if config.round == 0 {
		config.round = config.prev.metadata.Round + 1
	}

	vmBlock := snowmantest.BuildChild(config.prev.vmBlock.(*wrappedBlock).Block)
	block := &Block{
		vmBlock: &wrappedBlock{
			Block: vmBlock,
			vm:    config.prev.vmBlock.(*wrappedBlock).vm,
		},
		blockTracker: config.prev.blockTracker,
		metadata: simplex.ProtocolMetadata{
			Version: 1,
			Epoch:   1,
			Round:   config.round,
			Seq:     vmBlock.Height(),
			Prev:    config.prev.digest,
		},
	}

	bytes, err := block.Bytes()
	require.NoError(t, err)

	digest := computeDigest(bytes)
	block.digest = digest
	return block
}

func newEngineConfig(t *testing.T, numNodes uint64) *Config {
	return newNetworkConfigs(t, numNodes)[0]
}

type testNode struct {
	simplexparams.ValidatorInfo
	signFunc SignFunc
}

// newConfigsForQC builds a minimal set of configs sufficient for building a QC
// (signers + validator membership). It avoids the heavier per-config setup
// (mocks, WAL, message creator) so it can be called from f.Fuzz outer scope.
func newConfigsForQC(t testing.TB) []*Config {
	numNodes := 4
	require.Positive(t, numNodes)

	chainID := ids.GenerateTestID()
	testNodes := generateTestNodes(t, uint64(numNodes), testNodeConfigWithKeyReuse)
	chainParameters := newSimplexChainParams(testNodes)

	configs := make([]*Config, 0, numNodes)
	for _, node := range testNodes {
		configs = append(configs, &Config{
			Ctx: SimplexChainContext{
				NodeID:    node.NodeID,
				ChainID:   chainID,
				NetworkID: constants.UnitTestID,
			},
			SignBLS: node.signFunc,
			Params:  chainParameters,
		})
	}
	return configs
}

// newNetworkConfigs creates a slice of Configs for testing purposes,
// initialized with a common chainID and a set of validators each having
// a freshly generated BLS key.
func newNetworkConfigs(t *testing.T, numNodes uint64) []*Config {
	return newNetworkConfigsWithKeyReuse(t, numNodes, noKeyReuse)
}

// newNetworkConfigsWithKeyReuse is like newNetworkConfigs but allows the
// caller to opt into reusing a single cached BLS key across all validators
// (intended for fuzz tests where BLS key generation dominates setup cost).
func newNetworkConfigsWithKeyReuse(t *testing.T, numNodes uint64, reuseKeys keyReuseOption) []*Config {
	require.Positive(t, numNodes)

	chainID := ids.GenerateTestID()

	testNodes := generateTestNodes(t, numNodes, func(cfg *testNodeConfig) {
		cfg.reuseKeys = reuseKeys
	})
	chainParameters := newSimplexChainParams(testNodes)
	configs := make([]*Config, 0, numNodes)

	for _, node := range testNodes {
		ctrl := gomock.NewController(t)
		sender := sendermock.NewExternalSender(ctrl)
		mc, err := message.NewCreator(
			prometheus.NewRegistry(),
			constants.DefaultNetworkCompressionType,
			10*time.Second,
		)
		require.NoError(t, err)
		config := &Config{
			Ctx: SimplexChainContext{
				NodeID:    node.NodeID,
				ChainID:   chainID,
				NetworkID: constants.UnitTestID,
			},
			Log:                logging.NoLog{},
			Sender:             sender,
			OutboundMsgBuilder: mc,
			VM:                 newTestVM(),
			DB:                 memdb.New(),
			WAL:                wal.NewMemWAL(t),
			SignBLS:            node.signFunc,
			Params:             chainParameters,
		}
		configs = append(configs, config)
	}

	return configs
}

// newSimplexChainParams creates simplex chain parameters with the given nodes as initial validators.
func newSimplexChainParams(nodes []*testNode) *simplexparams.Parameters {
	params := &simplexparams.Parameters{
		MaxNetworkDelay:    1 * time.Second,
		MaxRebroadcastWait: 1 * time.Second,
	}
	params.InitialValidators = make([]simplexparams.ValidatorInfo, len(nodes))
	for i, node := range nodes {
		params.InitialValidators[i] = node.ValidatorInfo
	}
	return params
}

func generateTestNodes(t testing.TB, num uint64, opts ...testNodeConfigOption) []*testNode {
	var cfg testNodeConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	nodes := make([]*testNode, num)
	for i := uint64(0); i < num; i++ {
		var ls *localsigner.LocalSigner
		var err error
		var pk []byte

		if cfg.reuseKeys {
			ls = cachedBLSKey
			require.NotNil(t, ls, "cached BLS key is not available")
			pk = cachedCompressedPK
		} else {
			ls, err = localsigner.New()
			require.NoError(t, err)
			pk = ls.PublicKey().Compress()
		}

		nodeID := ids.GenerateTestNodeID()
		nodes[i] = &testNode{
			ValidatorInfo: simplexparams.ValidatorInfo{
				NodeID:    nodeID,
				PublicKey: pk,
			},
			signFunc: ls.Sign,
		}
	}
	return nodes
}

// newTestFinalization creates a new finalization over the BlockHeader, by collecting a
// quorum of signatures from the provided configs.
func newTestFinalization(t *testing.T, configs []*Config, bh simplex.BlockHeader) simplex.Finalization {
	quorum := simplex.Quorum(len(configs))
	finalizedVotes := make([]*simplex.FinalizeVote, 0, quorum)

	for _, config := range configs[:quorum] {
		vote := simplex.ToBeSignedFinalization{
			BlockHeader: bh,
		}
		signer, _, err := NewBLSAuth(config)
		require.NoError(t, err)
		sig, err := vote.Sign(&signer)
		require.NoError(t, err)
		finalizedVotes = append(finalizedVotes, &simplex.FinalizeVote{
			Finalization: vote,
			Signature: simplex.Signature{
				Signer: config.Ctx.NodeID[:],
				Value:  sig,
			},
		})
	}

	_, verifier, err := NewBLSAuth(configs[0])
	require.NoError(t, err)
	sigAgg := &SignatureAggregator{verifier: &verifier}

	finalization, err := simplex.NewFinalization(configs[0].Log, sigAgg, finalizedVotes)
	require.NoError(t, err)
	return finalization
}

func newTestVM() *wrappedVM {
	return &wrappedVM{
		VM: &blocktest.VM{},
		blocks: map[ids.ID]*snowmantest.Block{
			snowmantest.Genesis.ID(): snowmantest.Genesis,
		},
	}
}

// wrappedBlock wraps a test block in a VM so that on Accept, it is stored in the VM's block store.
type wrappedBlock struct {
	*snowmantest.Block
	vm *wrappedVM
}

type wrappedVM struct {
	*blocktest.VM
	blocks map[ids.ID]*snowmantest.Block
}

func (wb *wrappedBlock) Accept(ctx context.Context) error {
	if err := wb.Block.Accept(ctx); err != nil {
		return err
	}

	wb.vm.blocks[wb.ID()] = wb.Block
	return nil
}

func (v *wrappedVM) GetBlockIDAtHeight(_ context.Context, height uint64) (ids.ID, error) {
	for _, block := range v.blocks {
		if block.Height() == height {
			return block.ID(), nil
		}
	}
	return ids.Empty, database.ErrNotFound
}

func (v *wrappedVM) GetBlock(_ context.Context, id ids.ID) (snowman.Block, error) {
	block, exists := v.blocks[id]
	if !exists {
		return nil, database.ErrNotFound
	}
	return block, nil
}

func (v *wrappedVM) LastAccepted(_ context.Context) (ids.ID, error) {
	// find the block with the highest height
	if len(v.blocks) == 0 {
		return ids.Empty, database.ErrNotFound
	}

	lastAccepted := snowmantest.Genesis
	for _, block := range v.blocks {
		if block.Height() > lastAccepted.Height() {
			lastAccepted = block
		}
	}

	return lastAccepted.ID(), nil
}
