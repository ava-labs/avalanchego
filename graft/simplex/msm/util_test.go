// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metadata

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"errors"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/graft/simplex/common"
	"github.com/ava-labs/avalanchego/graft/simplex/testutil"
	"github.com/stretchr/testify/require"
)

// Test helpers

var (
	testSK *ecdsa.PrivateKey
	testPK *ecdsa.PublicKey
)

func init() {
	// We generate this key-pair to test that auxiliary info signs the right.
	sk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(fmt.Sprintf("failed to generate test key: %v", err))
	}
	testSK = sk
	testPK = &sk.PublicKey
}

type InnerBlock struct {
	TS          time.Time
	BlockHeight uint64
	bytes       []byte
}

func (i *InnerBlock) Bytes() []byte {
	return i.bytes
}

func (i *InnerBlock) Digest() [32]byte {
	return sha256.Sum256(i.bytes)
}

func (i *InnerBlock) Height() uint64 {
	return i.BlockHeight
}

func (i *InnerBlock) Timestamp() time.Time {
	return i.TS
}

func (i *InnerBlock) Verify(_ context.Context, _ uint64) error {
	return nil
}

// fakeVMBlock is a minimal common.VMBlock implementation for tests.
type fakeVMBlock struct {
	height uint64
}

func (f *fakeVMBlock) Bytes() []byte {
	panic("implement me")
}

func (f *fakeVMBlock) Digest() [32]byte                         { return [32]byte{} }
func (f *fakeVMBlock) Height() uint64                           { return f.height }
func (f *fakeVMBlock) Timestamp() time.Time                     { return time.Time{} }
func (f *fakeVMBlock) Verify(_ context.Context, _ uint64) error { return nil }

type outerBlock struct {
	finalization *common.Finalization
	block        StateMachineBlock
}

type blockStore map[uint64]*outerBlock

func (bs blockStore) clone() blockStore {
	newStore := make(blockStore)
	maps.Copy(newStore, bs)
	return newStore
}

func (bs blockStore) getBlock(seq uint64, _ common.Digest) (StateMachineBlock, *common.Finalization, error) {
	blk, exits := bs[seq]
	if !exits {
		return StateMachineBlock{}, nil, fmt.Errorf("%w: block %d not found", common.ErrBlockNotFound, seq)
	}
	return blk.block, blk.finalization, nil
}

type signer struct {
}

func (s *signer) Sign(digest []byte) (common.SignatureBytes, error) {
	return testSK.Sign(rand.Reader, digest, nil)
}

// signApproval produces a real ECDSA signature over the exact payload the production code signs
// for an epoch-transition approval (assembleApprovalToBeSigned), using the shared test key. The
// signatureVerifier above accepts it. Use this instead of placeholder signature bytes for any
// approval fixture that is expected to pass signature verification.
func signApproval(pChainHeight uint64, auxInfoDigest common.Digest) []byte {
	toBeSigned, err := assembleApprovalToBeSigned(pChainHeight, auxInfoDigest)
	if err != nil {
		panic(fmt.Sprintf("failed to assemble approval payload: %v", err))
	}
	sig, err := testSK.Sign(rand.Reader, toBeSigned, nil)
	if err != nil {
		panic(fmt.Sprintf("failed to sign approval: %v", err))
	}
	return sig
}

type signatureVerifier struct {
	err error
}

func (sv *signatureVerifier) VerifySignature(message []byte, signature common.SignatureBytes, _ common.PublicKeyBytes) error {
	if sv.err != nil {
		return sv.err
	}
	if ecdsa.VerifyASN1(testPK, message, signature) {
		return nil
	}

	// Maybe it's an aggregated signature?
	var aggSig aggregatedSignature
	_, err := asn1.Unmarshal(signature, &aggSig)
	if err != nil {
		return fmt.Errorf("invalid signature format: %w", err)
	}

	for _, sig := range aggSig.Signatures {
		if !ecdsa.VerifyASN1(testPK, message, sig) {
			return fmt.Errorf("invalid signature in aggregate")
		}
	}

	return nil
}

type signatureAggregator struct {
	weightByNodeID map[common.NodeID]uint64
	totalWeight    uint64
}

type aggregatedSignature struct {
	Signatures [][]byte
}

func (sv *signatureAggregator) Aggregate([]common.Signature) (common.QuorumCertificate, error) {
	panic("unused in tests")
}

func (sv *signatureAggregator) AppendSignatures(existing common.SignatureBytes, sigs ...common.SignatureBytes) (common.SignatureBytes, error) {
	all := make([][]byte, 0, len(sigs)+1)
	for _, sig := range sigs {
		all = append(all, sig)
	}
	if len(existing) > 0 {
		// existing is itself a marshaled aggregate from a previous round. Flatten it into the
		// component signatures instead of nesting the blob, so the aggregate stays a single level
		// of individual signatures that signatureVerifier can validate one by one.
		var prev aggregatedSignature
		if _, err := asn1.Unmarshal(existing, &prev); err != nil {
			return nil, err
		}
		all = append(all, prev.Signatures...)
	}
	return asn1.Marshal(aggregatedSignature{Signatures: all})
}

func (sv *signatureAggregator) IsQuorum(signers []common.NodeID) bool {
	var sum uint64
	for _, signer := range signers {
		sum += sv.weightByNodeID[signer]
	}
	return sum*3 > sv.totalWeight*2
}

func (sv *signatureAggregator) AboveByzantineThreshold(signers []common.NodeID) bool {
	var sum uint64
	for _, signer := range signers {
		sum += sv.weightByNodeID[signer]
	}
	return sum >= (sv.totalWeight/3)+1
}

func newSignatureAggregatorCreator() common.SignatureAggregatorCreator {
	return func(weights []common.Node) common.SignatureAggregator {
		s := &signatureAggregator{weightByNodeID: make(map[common.NodeID]uint64, len(weights))}
		for _, nw := range weights {
			s.weightByNodeID[nw.Id] = nw.Weight
			s.totalWeight += nw.Weight
		}
		return s
	}
}

type noOpPChainListener struct{}

func (n *noOpPChainListener) WaitForProgress(ctx context.Context, _ uint64) error {
	<-ctx.Done()
	return ctx.Err()
}

type validatorSetRetriever struct {
	result    NodeBLSMappings
	resultMap map[uint64]NodeBLSMappings
	err       error
}

func (vsr *validatorSetRetriever) getValidatorSet(height uint64) (NodeBLSMappings, error) {
	if vsr.resultMap != nil {
		if result, ok := vsr.resultMap[height]; ok {
			return result, vsr.err
		}
	}
	return vsr.result, vsr.err
}

type keyAggregator struct{}

func (ka *keyAggregator) AggregateKeys(keys ...common.PublicKeyBytes) (common.PublicKeyBytes, error) {
	aggregated := make(common.PublicKeyBytes, 0)
	for _, key := range keys {
		aggregated = append(aggregated, key...)
	}
	return aggregated, nil
}

var (
	genesisBlock = StateMachineBlock{
		// Genesis block metadata has all zero values
		InnerBlock: &testutil.InnerBlock{
			TS:      time.Now(),
			Content: []byte{1, 2, 3},
		},
	}
)

// makeChain builds a chain of endHeight+1 blocks whose heights double as sequence numbers: genesis at
// height 0, non-Simplex blocks below simplexStartHeight, the zero block at simplexStartHeight that opens
// the first epoch with the given validator set, and normal Simplex blocks of that epoch above it.
func makeChain(t *testing.T, simplexStartHeight uint64, endHeight uint64, validatorSet NodeBLSMappings) []StateMachineBlock {
	startTime := time.Now().Add(-time.Duration(endHeight+2) * time.Second)
	blocks := make([]StateMachineBlock, 0, endHeight+1)
	var round, seq uint64
	for h := uint64(0); h <= endHeight; h++ {
		index := len(blocks)

		if h == 0 {
			blocks = append(blocks, genesisBlock)
			continue
		}

		if h < simplexStartHeight {
			blocks = append(blocks, makeNonSimplexBlock(t, simplexStartHeight, startTime, h))
			continue
		}

		seq = uint64(index)

		if h == simplexStartHeight {
			blocks = append(blocks, makeZeroBlock(blocks, validatorSet, round, seq))
		} else {
			blocks = append(blocks, makeNormalSimplexBlock(t, index, blocks, startTime, h, round, seq))
		}
		round++
	}
	return blocks
}

// makeZeroBlock builds the first Simplex block on top of the last block in blocks the way buildBlockZero
// does: it has no inner block, carries its parent's timestamp, and opens the first epoch, numbered by
// its own sequence, with the given validator set as its block validation descriptor.
func makeZeroBlock(blocks []StateMachineBlock, validatorSet NodeBLSMappings, round uint64, seq uint64) StateMachineBlock {
	parent := blocks[len(blocks)-1]
	epochInfo := constructSimplexZeroBlockSimplexEpochInfo(100, validatorSet, parent.InnerBlock.Height())

	return StateMachineBlock{
		Metadata: StateMachineMetadata{
			Timestamp:    uint64(parent.InnerBlock.Timestamp().UnixMilli()),
			PChainHeight: 100,
			SimplexProtocolMetadata: common.ProtocolMetadata{
				Round: round,
				Seq:   seq,
				Epoch: seq,
				Prev:  parent.Digest(),
			},
			SimplexEpochInfo: epochInfo,
		},
	}
}

// makeNormalSimplexBlock builds a normal block of the epoch its parent, the last block in blocks, belongs to.
func makeNormalSimplexBlock(t *testing.T, index int, blocks []StateMachineBlock, start time.Time, h uint64, round uint64, seq uint64) StateMachineBlock {
	content := make([]byte, 10)
	_, err := rand.Read(content)
	require.NoError(t, err)

	parent := blocks[index-1]
	epoch := parent.Metadata.SimplexProtocolMetadata.Epoch

	return StateMachineBlock{
		InnerBlock: &InnerBlock{
			TS:          start.Add(time.Duration(h) * time.Second),
			BlockHeight: h,
			bytes:       []byte{1, 2, 3},
		},
		Metadata: StateMachineMetadata{
			PChainHeight: 100,
			SimplexProtocolMetadata: common.ProtocolMetadata{
				Round: round,
				Seq:   seq,
				Epoch: epoch,
				Prev:  parent.Digest(),
			},
			SimplexEpochInfo: SimplexEpochInfo{
				PrevSealingBlockHash:  common.Digest{},
				PChainReferenceHeight: 100,
				PrevVMBlockSeq:        computePrevVMBlockSeq(&parent, uint64(index-1)),
			},
		},
	}
}

func makeNonSimplexBlock(t *testing.T, startHeight uint64, start time.Time, h uint64) StateMachineBlock {
	content := make([]byte, 10)
	_, err := rand.Read(content)
	require.NoError(t, err)

	return StateMachineBlock{
		InnerBlock: &InnerBlock{
			TS:          start.Add(time.Duration(h-startHeight) * time.Second),
			BlockHeight: h,
			bytes:       []byte{1, 2, 3},
		},
	}
}

type testConfig struct {
	blockStore            blockStore
	signatureVerifier     signatureVerifier
	blockBuilder          bb
	keyAggregator         keyAggregator
	validatorSetRetriever validatorSetRetriever
}

// bb is a test BlockBuilder whose BuildBlock returns a preconfigured block/error.
type bb struct {
	Block common.VMBlock
	Err   error
}

func (bb *bb) BuildBlock(context.Context, uint64) (common.VMBlock, error) {
	return bb.Block, bb.Err
}

func (bb *bb) WaitForPendingBlock(context.Context) {}

func newStateMachine(t *testing.T) (*StateMachine, *testConfig) {
	return newStateMachineWithLogger(t, testutil.MakeLogger(t))
}

func newStateMachineWithLogger(tb testing.TB, logger common.Logger) (*StateMachine, *testConfig) {
	bs := make(blockStore)
	bs[0] = &outerBlock{block: genesisBlock}

	var myNodeID common.NodeID

	var testConfig testConfig
	testConfig.blockStore = bs
	testConfig.validatorSetRetriever.result = NodeBLSMappings{
		{BLSKey: []byte{1}, Weight: 1}, {BLSKey: []byte{2}, Weight: 1},
	}

	smConfig := Config{
		GenesisValidatorSet: NodeBLSMappings{
			{BLSKey: []byte{1}, Weight: 1, NodeID: NodeID{ShortID: [20]byte{1}}},
			{BLSKey: []byte{2}, Weight: 1, NodeID: NodeID{ShortID: [20]byte{2}}},
		},
		LastNonSimplexBlockPChainHeight: 100,
		GetTime:                         time.Now,
		TimeSkewLimit:                   time.Second * 5,
		Logger:                          logger,
		GetBlock:                        testConfig.blockStore.getBlock,
		MaxBlockBuildingWaitTime:        time.Second,
		SignatureVerifier:               &testConfig.signatureVerifier,
		SignatureAggregatorCreator:      newSignatureAggregatorCreator(),
		BlockBuilder:                    &testConfig.blockBuilder,
		KeyAggregator:                   &testConfig.keyAggregator,
		GetPChainHeightForProposing: func() uint64 {
			return 100
		},
		GetPChainHeightForVerifying: func() uint64 {
			return 100
		},
		GetValidatorSet:          testConfig.validatorSetRetriever.getValidatorSet,
		PChainProgressListener:   &noOpPChainListener{},
		LastNonSimplexInnerBlock: genesisBlock.InnerBlock,
		MyNodeID:                 myNodeID,
		AuxiliaryInfoApp: &voteCountingAuxInfoApp{
			threshold: 2,
		},
		Signer: &signer{},
		ComputeICMEpoch: func(input ICMEpochInput) ICMEpochInfo {
			// This is just the ACP-181 implementation from avalanchego
			var zeroEpoch ICMEpochInfo
			if input.ParentEpoch == zeroEpoch {
				return ICMEpochInfo{
					PChainEpochHeight: input.ParentPChainHeight,
					EpochNumber:       1,
					EpochStartTime:    uint64(input.ParentTimestamp.Unix()),
				}
			}
			endTime := time.Unix(int64(input.ParentEpoch.EpochStartTime), 0).Add(time.Second)
			if input.ParentTimestamp.Before(endTime) {
				return input.ParentEpoch
			}
			return ICMEpochInfo{
				PChainEpochHeight: input.ParentPChainHeight,
				EpochNumber:       input.ParentEpoch.EpochNumber + 1,
				EpochStartTime:    uint64(input.ParentTimestamp.Unix()),
			}
		},
	}

	sm, err := NewStateMachine(&smConfig)
	require.NoError(tb, err)

	return sm, &testConfig
}

// concatAggregator concatenates signatures for easy verification in tests.
type concatAggregator struct{}

func (concatAggregator) Aggregate([]common.Signature) (common.QuorumCertificate, error) {
	panic("unused in tests")
}

func (concatAggregator) AppendSignatures(existing common.SignatureBytes, sigs ...common.SignatureBytes) (common.SignatureBytes, error) {
	var result common.SignatureBytes
	for _, sig := range sigs {
		result = append(result, sig...)
	}
	return append(result, existing...), nil
}

func (concatAggregator) IsQuorum([]common.NodeID) bool {
	return false
}

func (concatAggregator) AboveByzantineThreshold([]common.NodeID) bool {
	return false
}

type failingAggregator struct{}

func (failingAggregator) Aggregate([]common.Signature) (common.QuorumCertificate, error) {
	panic("unused in tests")
}

var errTestAggregationFailed = errors.New("aggregation failed")

func (failingAggregator) AppendSignatures(common.SignatureBytes, ...common.SignatureBytes) (common.SignatureBytes, error) {
	return nil, errTestAggregationFailed
}

func (failingAggregator) IsQuorum([]common.NodeID) bool {
	return false
}

func (failingAggregator) AboveByzantineThreshold([]common.NodeID) bool {
	return false
}

type noopTestAuxInfoApp struct {
}

func (t *noopTestAuxInfoApp) IsLegalAppend(common.VersionID, NodeBLSMappings, [][]byte, []byte) error {
	return nil
}

func (t *noopTestAuxInfoApp) IsSufficient(common.VersionID, NodeBLSMappings, [][]byte) (bool, error) {
	return true, nil
}

func (t *noopTestAuxInfoApp) Generate(common.VersionID, NodeBLSMappings, [][]byte) ([]byte, error) {
	return nil, nil
}

func (t *noopTestAuxInfoApp) DefaultVersionID() common.VersionID {
	return 1
}

type voteCountingAuxInfoApp struct {
	threshold  int
	randomTape func() []byte
}

func (t *voteCountingAuxInfoApp) IsLegalAppend(_ common.VersionID, _ NodeBLSMappings, history [][]byte, addition []byte) error {
	set := make(map[string]struct{})
	for _, item := range history {
		set[string(item)] = struct{}{}
	}
	if _, exists := set[string(addition)]; exists {
		return fmt.Errorf("duplicate addition: %s", string(addition))
	}
	return nil
}

func (t *voteCountingAuxInfoApp) IsSufficient(appID common.VersionID, nodes NodeBLSMappings, history [][]byte) (bool, error) {
	if len(history) == 0 {
		return t.threshold == 0, nil
	}
	addition := history[len(history)-1]
	history = history[:len(history)-1]
	if err := t.IsLegalAppend(appID, nodes, history, addition); err != nil {
		return false, err
	}

	history = append(history, addition)
	set := make(map[string]struct{})
	for _, item := range history {
		set[string(item)] = struct{}{}
	}
	final := len(set) >= t.threshold
	return final, nil
}

func (t *voteCountingAuxInfoApp) Generate(common.VersionID, NodeBLSMappings, [][]byte) ([]byte, error) {
	// Simulate a random node voting
	if t.randomTape != nil {
		return t.randomTape(), nil
	}
	var id [20]byte
	rand.Read(id[:])
	return id[:], nil
}

func (t *voteCountingAuxInfoApp) DefaultVersionID() common.VersionID {
	return 1
}

// versionRecordingAuxInfoApp behaves like voteCountingAuxInfoApp (a vote is appended to the
// history on every Generate, and the history becomes sufficient once it holds `threshold`
// distinct votes), but it asserts that every method is invoked with expectedVersionID, and its
// DefaultVersionID() returns whatever defaultVersionID is currently set to. A test drives both
// fields directly to prove backward compatibility: once an epoch's auxiliary info carries a
// VersionID, every invocation keeps using that VersionID even after the default changes.
type versionRecordingAuxInfoApp struct {
	t                 *testing.T
	threshold         int
	votes             [][]byte
	defaultVersionID  common.VersionID
	expectedVersionID common.VersionID
}

func (a *versionRecordingAuxInfoApp) DefaultVersionID() common.VersionID {
	return a.defaultVersionID
}

func (a *versionRecordingAuxInfoApp) IsLegalAppend(versionID common.VersionID, _ NodeBLSMappings, history [][]byte, addition []byte) error {
	require.Equal(a.t, a.expectedVersionID, versionID)
	set := make(map[string]struct{})
	for _, item := range history {
		set[string(item)] = struct{}{}
	}
	if _, exists := set[string(addition)]; exists {
		return fmt.Errorf("duplicate addition: %s", string(addition))
	}
	return nil
}

func (a *versionRecordingAuxInfoApp) IsSufficient(versionID common.VersionID, _ NodeBLSMappings, history [][]byte) (bool, error) {
	require.Equal(a.t, a.expectedVersionID, versionID)
	set := make(map[string]struct{})
	for _, item := range history {
		set[string(item)] = struct{}{}
	}
	return len(set) >= a.threshold, nil
}

func (a *versionRecordingAuxInfoApp) Generate(versionID common.VersionID, _ NodeBLSMappings, _ [][]byte) ([]byte, error) {
	require.Equal(a.t, a.expectedVersionID, versionID)
	next := a.votes[0]
	a.votes = a.votes[1:]
	return next, nil
}
