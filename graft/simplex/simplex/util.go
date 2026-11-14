// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	cryptoRand "crypto/rand"
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"

	"github.com/ava-labs/simplex/common"
	"go.uber.org/zap"
)

type verifiableMessage interface {
	Verify(nodes common.Nodes) error
}

// RetrieveLastIndexFromStorage retrieves the latest block and finalization from storage.
// Returns an error if it cannot be retrieved but the storage has some block.
// Returns (nil, nil) if the storage is empty.
func RetrieveLastIndexFromStorage(s common.Storage) (*common.VerifiedFinalizedBlock, error) {
	numBlocks := s.NumBlocks()
	if numBlocks == 0 {
		return nil, nil
	}
	lastBlock, finalization, err := s.Retrieve(numBlocks - 1)
	if err != nil {
		return nil, fmt.Errorf("failed retrieving last block from storage with seq %d: %w", numBlocks-1, err)
	}
	return &common.VerifiedFinalizedBlock{
		VerifiedBlock: lastBlock,
		Finalization:  finalization,
	}, nil
}

func hasSomeNodeSignedTwice(nodeIDs []common.NodeID) (common.NodeID, bool) {
	seen := make(map[string]struct{}, len(nodeIDs))

	for _, nodeID := range nodeIDs {
		if _, alreadySeen := seen[string(nodeID)]; alreadySeen {
			return nodeID, true
		}
		seen[string(nodeID)] = struct{}{}
	}

	return common.NodeID{}, false
}

func VerifyQC(qc common.QuorumCertificate, isQuorum func(signers []common.NodeID) bool, eligibleSigners map[string][]byte, messageToVerify verifiableMessage, nodes common.Nodes) error {
	if qc == nil {
		return fmt.Errorf("nil QuorumCertificate")
	}
	// Ensure no node signed the QuorumCertificate twice
	doubleSigner, signedTwice := hasSomeNodeSignedTwice(qc.Signers())
	if signedTwice {
		return fmt.Errorf("quorum certificate is signed by the same node (%s) more than once", doubleSigner)
	}

	// Check enough signers signed the QuorumCertificate
	if !isQuorum(qc.Signers()) {
		return fmt.Errorf("quorum certificate signed by insufficient (%d) nodes", len(qc.Signers()))
	}

	// Check QuorumCertificate was signed by only eligible nodes
	for _, signer := range qc.Signers() {
		if _, exists := eligibleSigners[string(signer)]; !exists {
			return fmt.Errorf("quorum certificate contains an unknown signer (%s)", signer)
		}
	}

	return messageToVerify.Verify(nodes)
}

// GetLatestVerifiedQuorumRound returns the latest verified quorum round given
// a round and empty notarization. If both are nil, it returns nil.
func GetLatestVerifiedQuorumRound(round *Round, emptyNotarization *common.EmptyNotarization) *common.VerifiedQuorumRound {
	var verifiedQuorumRound *common.VerifiedQuorumRound
	var highestRound uint64
	var exists bool

	if round != nil && (round.finalization != nil || round.notarization != nil) {
		highestRound = round.num
		verifiedQuorumRound = &common.VerifiedQuorumRound{
			VerifiedBlock: round.block,
			Notarization:  round.notarization,
			Finalization:  round.finalization,
		}
		exists = true
	}

	if emptyNotarization != nil {
		emptyNoteRound := emptyNotarization.Vote.Round
		if emptyNoteRound > highestRound || !exists {
			verifiedQuorumRound = &common.VerifiedQuorumRound{
				EmptyNotarization: emptyNotarization,
			}
		}
	}

	return verifiedQuorumRound
}

// SetRound is a helper function that is used for tests to create a round.
func SetRound(block common.VerifiedBlock, notarization *common.Notarization, finalization *common.Finalization) *Round {
	round := &Round{
		block:        block,
		notarization: notarization,
		finalization: finalization,
		num:          block.BlockHeader().Round,
	}

	return round
}

type OneTimeVerifier struct {
	lock    sync.Mutex
	digests map[common.Digest]verifiedResult
	logger  common.Logger
}

func NewOneTimeVerifier(logger common.Logger) *OneTimeVerifier {
	return &OneTimeVerifier{
		digests: make(map[common.Digest]verifiedResult),
		logger:  logger,
	}
}

func (otv *OneTimeVerifier) Wrap(block common.Block) common.Block {
	return &oneTimeVerifiedBlock{
		otv:   otv,
		Block: block,
	}
}

type verifiedResult struct {
	seq uint64
	vb  common.VerifiedBlock
	err error
}

type oneTimeVerifiedBlock struct {
	otv *OneTimeVerifier
	common.Block
}

func (block *oneTimeVerifiedBlock) Verify(ctx context.Context, verifyOpts ...common.VerifyOptions) (common.VerifiedBlock, error) {
	block.otv.lock.Lock()
	defer block.otv.lock.Unlock()

	header := block.BlockHeader()
	digest := header.Digest
	seq := header.Seq

	if result, exists := block.otv.digests[digest]; exists {
		block.otv.logger.Debug("Attempted to verify an already verified block", zap.Uint64("round", header.Round), zap.Error(result.err))
		return result.vb, result.err
	}

	vb, err := block.Block.Verify(ctx, verifyOpts...)
	if err != nil {
		block.otv.logger.Debug("Block verification failed", zap.Uint64("round", header.Round), zap.Error(err))
		return nil, err
	}

	// cleanup
	defer func() {
		for digest, vr := range block.otv.digests {
			if vr.seq < seq {
				delete(block.otv.digests, digest)
			}
		}
	}()

	block.otv.digests[digest] = verifiedResult{
		seq: seq,
		vb:  vb,
		err: err,
	}

	return vb, err
}

// BatchSequences distributes [seqs] as evenly as possible among [numNodes]
// nodes, returning one batch per node share. Every batch contains at most
// [maxSize] sequences. a share exceeding [maxSize] is emitted as multiple
// batches. [seqs] does not need to be sorted or contiguous, it is sorted in
// place, and the returned batches are sub-slices of it, ordered from lowest
// to highest sequence.
func BatchSequences(seqs []uint64, numNodes uint64, maxSize uint64) [][]uint64 {
	if len(seqs) == 0 || numNodes == 0 || maxSize == 0 {
		return nil
	}
	slices.Sort(seqs)

	numSeqs := uint64(len(seqs))

	seqsPerNode := (numSeqs + uint64(numNodes) - 1) / uint64(numNodes)

	batchSize := min(seqsPerNode, maxSize)
	return slices.Collect(slices.Chunk(seqs, int(batchSize)))
}

type voteSigner interface {
	Signer() common.NodeID
}

func NodeIDsFromVotes[VS voteSigner](votes []VS) []common.NodeID {
	nodeIDs := make([]common.NodeID, 0, len(votes))
	for _, vote := range votes {
		nodeIDs = append(nodeIDs, vote.Signer())
	}
	return nodeIDs
}

func emptyVotesToSignatures(votes []*common.EmptyVote) []common.Signature {
	sigs := make([]common.Signature, 0, len(votes))
	for _, vote := range votes {
		sigs = append(sigs, vote.Signature)
	}
	return sigs
}

type walRound struct {
	round             uint64
	emptyNotarization *common.EmptyNotarization
	emptyVote         *common.ToBeSignedEmptyVote
	notarization      *common.Notarization
	finalization      *common.Finalization
	block             common.Block
}

func (t *walRound) String() string {
	hasEmptyNotarization := t.emptyNotarization != nil
	hasEmptyVote := t.emptyVote != nil
	hasNotarization := t.notarization != nil
	hasFinalization := t.finalization != nil
	hasBlock := t.block != nil
	return fmt.Sprintf("walRound{round: %d, hasEmptyNotarization: %t, hasEmptyVote: %t, hasNotarization: %t, hasFinalization: %t, hasBlock: %t}", t.round, hasEmptyNotarization, hasEmptyVote, hasNotarization, hasFinalization, hasBlock)
}

func NewRandomSource() (*rand.Rand, error) {
	var seedBytes [32]byte

	if _, err := cryptoRand.Read(seedBytes[:]); err != nil {
		return nil, fmt.Errorf("failed to read random bytes: %w", err)
	}

	chacha := rand.NewChaCha8(seedBytes)
	return rand.New(chacha), nil
}
