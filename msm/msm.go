// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metadata

import (
	"context"
	"encoding/asn1"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/ava-labs/simplex/avalanchego"
	"github.com/ava-labs/simplex/common"
	"go.uber.org/zap"
)

const (
	// Max allowed time difference between a block's timestamp and the current time.
	// A block cannot be more than a certain time in the future compared to the current time.
	maxSkew = 10 * time.Second
)

// state encodes the different stages of the epoch transition process, which determines how we build and verify blocks.
//
// StateMachineMetadata.NextState() inspects the parent block's metadata to perform the following state transitions:
//
//	 (initial state: No Simplex blocks yet)
//	                  │
//	                  ▼
//	┌───────────────────────────────────┐
//	│       stateFirstSimplexBlock      │  builds the zero block (no inner block);
//	│                                   │  creates epoch 1 with the initial validator set
//	└─────────────────┬─────────────────┘
//	                  │
//	                  ▼
//	┌───────────────────────────────────┐ ◀── validator set unchanged ──┐
//	│      stateBuildBlockNormalOp      │                               │
//	│  builds inner blocks within the   │ ──────────────────────────────┘
//	│  current epoch                    │ ◀────────────────────────────────────────────┐
//	└─────────────────┬─────────────────┘                                               │
//	                  │ validator set changed                                           │
//	                  │ (sets NextPChainReferenceHeight > 0)                            │
//	                  ▼                                                                 │
//	┌───────────────────────────────────┐ ◀── not enough approvals ─────┐               │
//	│   stateBuildCollectingApprovals   │                               │               │
//	│  aggregates approvals from        │ ──────────────────────────────┘               │
//	│  the next epoch's validator set   │                                               │
//	└─────────────────┬─────────────────┘                                               │
//	                  │ quorum reached: emit sealing block                              │
//	                  │ (BlockValidationDescriptor set)                                 │
//	                  ▼                                                                 │
//	┌───────────────────────────────────┐ ◀── sealing block ────────────┐               │
//	│    stateBuildBlockEpochSealed     │     not finalized yet         │               │
//	│  emits Telock (no inner block)    │ ──────────────────────────────┘               │
//	│  until the sealing block is       │                                               │
//	│  finalized; then opens the new    │ ─── new epoch ────────────────────────────────┘
//	│  epoch                            │
//	└───────────────────────────────────┘

var (
	errLastNonSimplexInnerBlockNil       = errors.New("failed constructing zero block: last non-Simplex inner block is nil")
	errInvalidProtocolMetadataSeq        = errors.New("invalid ProtocolMetadata sequence number: should be > 0")
	errInvalidProtocolMetadataEpoch      = errors.New("invalid ProtocolMetadata epoch number")
	errUnknownState                      = errors.New("unknown state")
	errBuiltGenesisInnerBlock            = errors.New("received a genesis block")
	errZeroBlockParentNoInnerBlock       = errors.New("zero block's parent has no inner block")
	errNilBlock                          = errors.New("block is nil")
	errInvalidPChainHeight               = errors.New("invalid P-chain height")
	errZeroBlockHasInnerBlock            = errors.New("zero block must not have an inner block")
	errZeroBlockPrevDigestMismatch       = errors.New("zero block previous digest does not match last non-Simplex block digest")
	errZeroBlockSeqMismatch              = errors.New("zero block sequence does not succeed the last non-Simplex block sequence")
	errZeroBlockTimestampMismatch        = errors.New("zero block timestamp does not match last non-Simplex inner block timestamp")
	errPrevSealingBlockNotFinalized      = errors.New("previous sealing block is not finalized")
	errBlockDigestMismatch               = errors.New("does not match proposed block digest")
	errSealingBlockSeqUnset              = errors.New("cannot build epoch sealed block: sealing block sequence is 0 or undefined")
	errEmptyNextEpochApprovals           = errors.New("next epoch approvals are empty")
	errApprovalsBitmaskTooWide           = errors.New("approvals bitmask is wider than the validator set")
	errPChainReferenceHeightMismatch     = errors.New("unexpected P-chain reference height")
	errPChainReferenceHeightDecreased    = errors.New("p-chain reference height is decreasing")
	errValidatorSetUnchanged             = errors.New("validator set unchanged; next P-chain reference height should not have advanced")
	errApprovalStoreValidatorSetMismatch = errors.New("approval store already initialized for a different validator set")
	errPChainHeightNotReached            = errors.New("haven't reached referenced P-chain height yet")
	errPChainHeightTooBig                = errors.New("invalid P-chain height: greater than current")
	errPChainHeightSmallerThanParent     = errors.New("invalid P-chain height: smaller than parent block's")
	errSignerSetShrunk                   = errors.New("some signers from parent block are missing from next epoch approvals of proposed block")
	errNextEpochApprovalsShrunk          = errors.New("previous block has next epoch approvals but proposed block doesn't have next epoch approvals")
	errTimestampTooBig                   = errors.New("invalid timestamp: exceeds maximum int64 value")
	errTimestampDecreasing               = errors.New("invalid timestamp: proposed timestamp is before parent block's timestamp")
	errTimestampTooFarInFuture           = errors.New("invalid timestamp: proposed timestamp is too far in the future compared to current time")
	errAuxInfoBlockRetrieval             = errors.New("failed to retrieve block while collecting auxiliary info")
	errAuxInfoIllegalAppend              = errors.New("proposed auxiliary info is not a legal append to the history")

	signatureContext = "MSM approval"
)

// ICMEpochInput defines the input for computing the ICM Epoch information for the next block.
type ICMEpochInput struct {
	// ParentPChainHeight is the P-chain height recorded in the parent block.
	ParentPChainHeight uint64
	// ParentTimestamp is the timestamp of the parent block.
	ParentTimestamp time.Time
	// ChildTimestamp is the timestamp of the block being built.
	ChildTimestamp time.Time
	// ParentEpoch is the ICM epoch information from the parent block.
	ParentEpoch ICMEpochInfo
}

// ICMEpochTransition computes the next ICM epoch given the current upgrade configuration and epoch input.
type ICMEpochTransition func(ICMEpochInput) ICMEpochInfo

// KeyAggregator combines multiple public keys into a single aggregated public key.
type KeyAggregator interface {
	AggregateKeys(keys ...common.PublicKeyBytes) (common.PublicKeyBytes, error)
}

// ValidatorSetRetriever retrieves the validator set at a given P-chain height.
type ValidatorSetRetriever func(pChainHeight uint64) (NodeBLSMappings, error)

// BlockRetriever retrieves a block and its finalization status given the block's sequence number and expected digest.
// If the block cannot be found it returns ErrBlockNotFound.
// If an error occurs during retrieval, it returns a non-nil error.
type BlockRetriever func(seq uint64, digest common.Digest) (StateMachineBlock, *common.Finalization, error)

// BlockBuilder builds a new VM block with the given observed P-chain height.
type BlockBuilder interface {
	BuildBlock(ctx context.Context, pChainHeight uint64) (avalanchego.VMBlock, error)

	// WaitForPendingBlock returns when either the given context is cancelled,
	// or when the VM signals that a block should be built.
	WaitForPendingBlock(ctx context.Context)
}

// AuxiliaryInfoGenVerifier abstracts the application-specific logic for generating and verifying auxiliary information
// that is piggybacked on epoch transitions.
type AuxiliaryInfoGenVerifier interface {
	// IsLegalAppend checks whether the given auxiliary information byte slice [x]
	// can be appended to the history of auxiliary information for the given versionID, according to the app's rules.
	// Returns nil if the append is legal, or an error if the append is not legal or if any error occurs during the check.
	IsLegalAppend(versionID common.VersionID, nodes NodeBLSMappings, history [][]byte, x []byte) error

	// IsSufficient checks whether the given history of auxiliary information for the given versionID is sufficient
	// to start the epoch transition process.
	IsSufficient(versionID common.VersionID, nodes NodeBLSMappings, history [][]byte) (bool, error)

	// Generate generates an auxiliary information encoded as a byte slice based on the history of auxiliary information
	// for the given versionID in the current epoch so far.
	// If this is the first invocation in the epoch, DefaultVersionID() should be passed as the VersionID.
	// Otherwise, the versionID from previous blocks in the epoch should be used.
	// If the application deems the given history to be sufficient for the epoch change, it can return a nil byte slice,
	// in which case it will not be appended to the history.
	Generate(versionID common.VersionID, nodes NodeBLSMappings, history [][]byte) ([]byte, error)

	// DefaultVersionID returns the default VersionID that should be used for epochs that don't have any any auxiliary information yet.
	DefaultVersionID() common.VersionID
}

// StateMachine manages block building and verification across epoch transitions.
type StateMachine struct {
	*Config
	lock          sync.RWMutex
	approvalStore *ApprovalStore

	auxInfoStore *auxInfoStore
}

// Config contains the dependencies and configuration parameters needed to initialize the StateMachine.
type Config struct {
	// LatestPersistedHeight is the height of the most recently persisted block.
	LatestPersistedHeight uint64
	// MaxBlockBuildingWaitTime is the maximum duration to wait for the VM to build a block
	// before producing a block without an inner block.
	MaxBlockBuildingWaitTime time.Duration
	// TimeSkewLimit is the maximum allowed time difference between a block's timestamp and the current time.
	TimeSkewLimit time.Duration
	// GetTime returns the current time.
	GetTime func() time.Time
	// GetPChainHeightForProposing returns the latest known P-chain height to be used when building a block.
	GetPChainHeightForProposing func() uint64
	// GetPChainHeightForVerifying returns the latest known P-chain height to be used when verifying a block.
	GetPChainHeightForVerifying func() uint64
	// BlockBuilder builds new VM blocks.
	BlockBuilder BlockBuilder
	// Logger is used for logging state machine operations.
	Logger common.Logger
	// GetValidatorSet retrieves the validator set at a given P-chain height.
	GetValidatorSet ValidatorSetRetriever
	// GetBlock retrieves a previously built or finalized block.
	GetBlock BlockRetriever
	// SignatureAggregatorCreator creates a new SignatureAggregator for aggregating validator signatures for epoch transitions.
	SignatureAggregatorCreator common.SignatureAggregatorCreator
	// KeyAggregator aggregates public keys from validators.
	KeyAggregator KeyAggregator
	// SignatureVerifier verifies signatures from validators, such as their approvals for epoch transitions.
	SignatureVerifier common.SignatureVerifier
	// PChainProgressListener listens for changes in the P-chain height to trigger block building or epoch transitions.
	PChainProgressListener PChainProgressListener
	// LastNonSimplexBlockPChainHeight is the P-chain height of the last block built by a non-Simplex proposer.
	// It is used to determine the validator set of the first ever Simplex epoch.
	LastNonSimplexBlockPChainHeight uint64
	// LastNonSimplexInnerBlock is the inner block of the last block built by a non-Simplex proposer.
	LastNonSimplexInnerBlock avalanchego.VMBlock
	// GenesisValidatorSet is the validator set used for the genesis block.
	GenesisValidatorSet NodeBLSMappings
	// MyNodeID
	MyNodeID common.NodeID
	// Signer
	Signer common.Signer
	// ComputeICMEpoch computes the ICM epoch information in order to know which P-chain height to encode.
	ComputeICMEpoch ICMEpochTransition
	// AuxiliaryInfoApp abstracts an application that piggybacks on epoch changes.
	AuxiliaryInfoApp AuxiliaryInfoGenVerifier
}

type state uint8

const (
	stateFirstSimplexBlock state = iota + 1
	stateBuildBlockNormalOp
	stateBuildCollectingApprovals
	stateBuildBlockEpochSealed
)

func NewStateMachine(config *Config) (*StateMachine, error) {
	if config.LastNonSimplexInnerBlock == nil {
		config.Logger.Error("Last non-Simplex inner block is nil, cannot build zero block with correct metadata")
		return nil, errLastNonSimplexInnerBlockNil
	}
	if config.TimeSkewLimit == 0 {
		config.TimeSkewLimit = maxSkew
	}

	sm := StateMachine{Config: config, auxInfoStore: newAuxInfoStore(config.AuxiliaryInfoApp, config.Logger)}

	// Get the last block
	lastBlock, _, err := config.GetBlock(config.LatestPersistedHeight, common.Digest{})
	if err != nil {
		return nil, err
	}

	return &sm, sm.maybeInitApprovalStore(lastBlock)
}

// HandleAuxiliaryMessage processes
func (sm *StateMachine) HandleAuxiliaryInfo(info common.AuxiliaryInfo, from avalanchego.NodeID) {
	sm.auxInfoStore.HandleAuxiliaryMessage(info, from)
}

// HandleApproval processes a validator set approval from a node.
func (sm *StateMachine) HandleApproval(approval *common.ValidatorSetApproval) {
	sm.lock.Lock()
	approvalStore := sm.approvalStore
	sm.lock.Unlock()

	if approvalStore == nil {
		sm.Logger.Debug("Approval store is not initialized, ignoring approval",
			zap.String("nodeID", fmt.Sprintf("%x", approval.NodeID)),
			zap.Uint64("pChainHeight", approval.PChainHeight))
		return
	}

	approvalStore.HandleApproval(approval)
}

// OnBlockIndex is called when `block` is indexed. If the block is a transition block, we check to
// see if we need to initialize the approval store.
func (sm *StateMachine) OnBlockIndex(block StateMachineBlock) error {
	return sm.maybeInitApprovalStore(block)
}

func (sm *StateMachine) maybeInitApprovalStore(block StateMachineBlock) error {
	if block.Type() != BlockTypeTransitioning {
		return nil
	}

	validators, err := sm.GetValidatorSet(block.Metadata.SimplexEpochInfo.NextPChainReferenceHeight)
	if err != nil {
		return err
	}

	sm.lock.Lock()
	defer sm.lock.Unlock()

	if sm.approvalStore == nil {
		sm.approvalStore = NewApprovalStore(sm.SignatureVerifier, validators, sm.Logger)
	} else if !validators.Equal(sm.approvalStore.validators) {
		return errApprovalStoreValidatorSetMismatch
	}
	return nil
}

// WaitForPendingBlock waits for either the VM to signal that a block is ready to be built,
// or for the state machine to determine that a block should be built immediately due to an epoch transition.
// In the latter case, we only wait up to MaxBlockBuildingWaitTime before returning.
func (sm *StateMachine) WaitForPendingBlock(ctx context.Context, currentRoundMetadata common.ProtocolMetadata) {
	if currentRoundMetadata.Seq == 0 {
		// This shouldn't happen because we never build the genesis block.
		// Just call the VM's WaitForPendingBlock to avoid blocking the state machine.
		sm.Logger.Debug("WaitForPendingBlock called with seq 0, which is invalid; forwarding to VM to avoid blocking")
		sm.BlockBuilder.WaitForPendingBlock(ctx)
		return
	}

	// In order to know whether we're transitioning to a new epoch, or should transition to one,
	// we need to look at the previous block's metadata.

	lastBlockSeq := currentRoundMetadata.Seq - 1

	lastBlock, finalization, err := sm.GetBlock(lastBlockSeq, currentRoundMetadata.Prev)
	if err != nil {
		sm.Logger.Debug(
			"WaitForPendingBlock failed to get block",
			zap.Uint64("Current Seq", currentRoundMetadata.Seq),
			zap.Uint64("Current Round", currentRoundMetadata.Round),
			zap.Uint64("seq", lastBlockSeq),
			zap.Error(err),
		)
		sm.BlockBuilder.WaitForPendingBlock(ctx)
		return
	}

	// In case the last block is a Telock, we want to look at the sealing block's metadata instead.
	if lastBlock.Type() == BlockTypeTelock {
		sealingBlockSeq := lastBlock.Metadata.SimplexEpochInfo.SealingBlockSeq
		var sealingBlock StateMachineBlock
		sealingBlock, finalization, err = sm.GetBlock(sealingBlockSeq, common.Digest{})
		if err != nil {
			sm.Logger.Debug("Failed retrieving sealing block for previous epoch", zap.Uint64("seq", sealingBlockSeq), zap.Error(err))
			sm.BlockBuilder.WaitForPendingBlock(ctx)
			return
		}

		// If the sealing block isn't finalized, we need to build a Telock immediately to extend the epoch.
		if finalization == nil {
			return
		}
		// Else, err is nil and the sealing block is finalized,
		// so we can use the sealing block's metadata to determine whether we should build a block immediately or not.
		lastBlock = sealingBlock
		lastBlockSeq = sealingBlockSeq
	}

	currentState := lastBlock.Metadata.NextState()

	// We first check if we have obvious signs that we need to build a block immediately:
	var shouldObviouslyBuildBlockImmediately bool

	switch currentState {
	case stateFirstSimplexBlock, stateBuildCollectingApprovals:
		// In case of the first simplex block we need to persist the validator set,
		// and if we're collecting approvals then we're in the process of an epoch change already.
		shouldObviouslyBuildBlockImmediately = true
	case stateBuildBlockEpochSealed:
		// The parent is a sealing block, whose finalization we already fetched above.
		// If it isn't finalized we must build a Telock immediately to extend the epoch;
		// if it is finalized, we fall through to the normal epoch-transition check below.
		if finalization == nil {
			shouldObviouslyBuildBlockImmediately = true
		}
	default: // Handles stateBuildBlockNormalOp or any unknown state, don't do anything and just exit the switch.
	}

	// If it's obvious that we should build a block immediately,
	// we wait up to MaxBlockBuildingWaitTime and then return immediately.
	if shouldObviouslyBuildBlockImmediately {
		ctx, cancel := context.WithTimeout(ctx, sm.MaxBlockBuildingWaitTime)
		defer cancel()
		sm.BlockBuilder.WaitForPendingBlock(ctx)
		return
	}

	// Otherwise, we might need to build a block if we detect that we should transition to a new epoch,
	// so we initialize a blockBuildingDecider and listen while waiting for the VM.
	// We return when either the VM signals that a block is ready to be built,
	// or that the blockBuildingDecider detects that we should transition to a new epoch.
	var currentValidatorSet NodeBLSMappings
	if lastBlock.Type() == BlockTypeSealing {
		// We've moved to a new epoch, so the current validator set is the one the sealing block
		// itself carries; getCurrentValidatorSet would instead look up the sealed (previous) epoch.
		bvd := lastBlock.Metadata.SimplexEpochInfo.BlockValidationDescriptor
		if bvd == nil || len(bvd.AggregatedMembership.Members) == 0 {
			sm.Logger.Debug("Sealing block has no validator set, cannot determine current validator set for next epoch", zap.Uint64("seq", lastBlockSeq))
			sm.BlockBuilder.WaitForPendingBlock(ctx)
			return
		}
		currentValidatorSet = bvd.AggregatedMembership.Members
	} else {
		// If the last block is not a sealing block, we need to look up the current validator set encoded in the sealing block of the previous epoch.
		currentValidatorSet, err = getCurrentValidatorSet(&lastBlock, sm.GetBlock)
		if err != nil {
			sm.Logger.Debug("WaitForPendingBlock failed to get current validator set", zap.Uint64("seq", lastBlockSeq), zap.Error(err))
			sm.BlockBuilder.WaitForPendingBlock(ctx)
			return
		}
	}

	blockBuildingDecider := sm.createBlockBuildingDecider(currentValidatorSet)
	_, err = blockBuildingDecider.shouldBuildBlock(ctx)
	if err != nil {
		sm.Logger.Debug(
			"Error while deciding whether to build a block",
			zap.Uint64("Current Round", currentRoundMetadata.Round),
			zap.Uint64("Current Seq", currentRoundMetadata.Seq),
			zap.Stringer("Prev Digest", currentRoundMetadata.Prev),
			zap.Error(err),
		)
		return
	}
}

// BuildBlock constructs the next block on top of the given parent block, and passes in the provided simplex metadata and blacklist.
func (sm *StateMachine) BuildBlock(ctx context.Context, metadata common.ProtocolMetadata, blacklist common.Blacklist) (*StateMachineBlock, error) {
	// The zero sequence number is reserved for the genesis block, which should never be built.
	if metadata.Seq == 0 {
		return nil, fmt.Errorf("%w: got %d", errInvalidProtocolMetadataSeq, metadata.Seq)
	}

	prevBlockSeq := metadata.Seq - 1

	parentBlock, _, err := sm.GetBlock(prevBlockSeq, metadata.Prev)
	if err != nil {
		return nil, fmt.Errorf("failed retrieving parent block at height %d with digest %s: %w", prevBlockSeq, metadata.Prev.String(), err)
	}

	start := time.Now()

	sm.Logger.Debug("Building block",
		zap.Uint64("seq", metadata.Seq),
		zap.Uint64("round", metadata.Round),
		zap.Uint64("epoch", metadata.Epoch),
		zap.Stringer("prevHash", metadata.Prev))

	defer func() {
		elapsed := time.Since(start)
		sm.Logger.Debug("Built block",
			zap.Uint64("seq", metadata.Seq),
			zap.Uint64("round", metadata.Round),
			zap.Uint64("epoch", metadata.Epoch),
			zap.Stringer("prevHash", metadata.Prev),
			zap.Duration("elapsed", elapsed),
		)
	}()

	// In order to know where in the epoch change process we are,
	// we identify the current state by looking at the parent block's epoch info.
	currentState := parentBlock.Metadata.NextState()

	switch currentState {
	case stateFirstSimplexBlock:
		return sm.buildBlockZero(parentBlock, metadata, blacklist)
	case stateBuildBlockNormalOp:
		return sm.buildBlockNormalOp(ctx, &parentBlock, metadata, blacklist, prevBlockSeq)
	case stateBuildCollectingApprovals:
		return sm.buildBlockCollectingApprovals(ctx, &parentBlock, metadata, blacklist, prevBlockSeq)
	case stateBuildBlockEpochSealed:
		return sm.buildBlockEpochSealed(ctx, &parentBlock, metadata, blacklist, prevBlockSeq)
	default:
		return nil, fmt.Errorf("%w: %d", errUnknownState, currentState)
	}
}

// VerifyBlock validates a proposed block by checking its metadata, epoch info,
// and inner block against the previous block and the current state.
func (sm *StateMachine) VerifyBlock(ctx context.Context, block *StateMachineBlock) error {
	if block == nil {
		return errNilBlock
	}

	pmd := block.Metadata.SimplexProtocolMetadata
	seq := pmd.Seq

	if seq == 0 {
		// This shouldn't happen, but in case we're asked to verify a block with a sequence of 0,
		// we should reject it, because the zero sequence number is reserved for the genesis block, which should never be proposed.
		return errBuiltGenesisInnerBlock
	}

	prevBlock, _, err := sm.GetBlock(seq-1, pmd.Prev)
	if err != nil {
		return fmt.Errorf("failed to retrieve previous (%d) inner block: %w", seq-1, err)
	}

	prevMD := prevBlock.Metadata
	currentState := prevMD.NextState()

	switch currentState {
	case stateFirstSimplexBlock:
		err = sm.verifyBlockZero(block, prevBlock)
	default:
		err = sm.verifyNonZeroBlock(ctx, block, &prevBlock, seq-1)
	}
	return err
}

func (sm *StateMachine) verifyNonZeroBlock(ctx context.Context, block, prevBlock *StateMachineBlock, prevSeq uint64) error {
	prevBlockMD := prevBlock.Metadata
	currentState := prevBlockMD.NextState()

	if err := verifyTimestamp(block, prevBlock, sm.GetTime(), sm.TimeSkewLimit); err != nil {
		return fmt.Errorf("failed to verify timestamp: %w", err)
	}

	currentPChainHeight := sm.GetPChainHeightForVerifying()
	prevPChainHeight := prevBlockMD.PChainHeight
	proposedPChainHeight := block.Metadata.PChainHeight

	if err := verifyPChainHeight(proposedPChainHeight, currentPChainHeight, prevPChainHeight); err != nil {
		return fmt.Errorf("failed to verify P-chain height: %w", err)
	}

	switch currentState {
	case stateBuildBlockNormalOp:
		return sm.verifyNormalBlock(ctx, prevBlock, block, prevSeq)
	case stateBuildCollectingApprovals:
		return sm.verifyCollectingApprovalsBlock(ctx, prevBlock, block, prevSeq)
	case stateBuildBlockEpochSealed:
		return sm.verifyBlockEpochSealed(ctx, prevBlock, block, prevSeq)
	default:
		return fmt.Errorf("%w: %d", errUnknownState, currentState)
	}
}

func verifyTimestamp(block *StateMachineBlock, prevBlock *StateMachineBlock, now time.Time, timeSkewLimit time.Duration) error {
	if block.Metadata.Timestamp > math.MaxInt64 {
		return fmt.Errorf("%w: timestamp %d exceeds maximum int64 value", errTimestampTooBig, block.Metadata.Timestamp)
	}

	if block.Metadata.Timestamp < prevBlock.Metadata.Timestamp {
		return fmt.Errorf("%w: proposed %d < parent %d", errTimestampDecreasing, block.Metadata.Timestamp, prevBlock.Metadata.Timestamp)
	}

	proposedTime := time.UnixMilli(int64(block.Metadata.Timestamp))

	if now.Add(timeSkewLimit).Before(proposedTime) {
		return fmt.Errorf("%w: proposed timestamp %v, max skew: %v", errTimestampTooFarInFuture, proposedTime, maxSkew)
	}
	return nil
}

// buildBlockNormalOp builds a block while potentially also transitioning to a new epoch, depending on the P-chain.
//
// Relevant fields (PCH = PChainReferenceHeight, NPCH = NextPChainReferenceHeight,
// EN = SimplexProtocolMetadata.Epoch):
//
//	parent (NormalOp)            validator set unchanged    validator set changed at p'
//	┌─────────────────┐          ┌─────────────────┐        ┌─────────────────┐
//	│ PCH  = p        │   ───►   │ PCH  = p (copy) │        │ PCH  = p (copy) │
//	│ EN   = e        │    OR    │ EN   = e (copy) │   OR   │ EN   = e (copy) │
//	│ NPCH = 0        │          │ NPCH = 0        │        │ NPCH = p' (> 0) │
//	└─────────────────┘          └─────────────────┘        └─────────────────┘
//	                             → stays NormalOp           → CollectingApprovals
func (sm *StateMachine) buildBlockNormalOp(ctx context.Context, parentBlock *StateMachineBlock, simplexMetadata common.ProtocolMetadata, simplexBlacklist common.Blacklist, prevBlockSeq uint64) (*StateMachineBlock, error) {
	// Since in the previous block, we were not transitioning to a new epoch,
	// the P-chain reference height and epoch of the new block should remain the same.
	newSimplexEpochInfo := SimplexEpochInfo{
		PChainReferenceHeight: parentBlock.Metadata.SimplexEpochInfo.PChainReferenceHeight,
		PrevVMBlockSeq:        computePrevVMBlockSeq(parentBlock, prevBlockSeq),
	}

	return sm.buildBlockOrTransitionEpoch(ctx, parentBlock, simplexMetadata, simplexBlacklist, newSimplexEpochInfo)
}

// buildBlockOrTransitionEpoch builds a block and decides whether to transition to a new epoch based on the P-chain height and validator set changes.
func (sm *StateMachine) buildBlockOrTransitionEpoch(ctx context.Context, parentBlock *StateMachineBlock, simplexMetadata common.ProtocolMetadata, simplexBlacklist common.Blacklist, newSimplexEpochInfo SimplexEpochInfo) (*StateMachineBlock, error) {
	var currentValidatorSet NodeBLSMappings
	var err error

	if parentBlock.Type() != BlockTypeSealing {
		currentValidatorSet, err = getCurrentValidatorSet(parentBlock, sm.GetBlock)
		if err != nil {
			return nil, fmt.Errorf("failed to get current validator set: %w", err)
		}
	} else {
		bvd := parentBlock.Metadata.SimplexEpochInfo.BlockValidationDescriptor
		if bvd == nil || len(bvd.AggregatedMembership.Members) == 0 {
			return nil, fmt.Errorf("sealing block has no validator set, cannot determine current validator set for next epoch")
		}
		currentValidatorSet = bvd.AggregatedMembership.Members
	}

	blockBuildingDecider := sm.createBlockBuildingDecider(currentValidatorSet)
	decisionToBuildBlock, err := blockBuildingDecider.shouldBuildBlock(ctx)
	if err != nil {
		if errors.Is(context.Cause(ctx), common.ErrShouldBuildEmptyBlock) {
			now := sm.GetTime()
			icmEpochInfo := computeICMEpochInfo(parentBlock, sm.ComputeICMEpoch, now)
			pChainHeight := sm.GetPChainHeightForProposing()
			return wrapBlock(nil, newSimplexEpochInfo, pChainHeight, simplexMetadata, simplexBlacklist, now, parentBlock.Metadata.Timestamp, icmEpochInfo, nil), nil
		}
		return nil, err
	}

	sm.Logger.Debug("Block building decision",
		zap.Bool("build inner block", decisionToBuildBlock.buildInnerBlock),
		zap.Bool("transition epoch", decisionToBuildBlock.transitionEpoch),
		zap.Uint64("P-chain height", decisionToBuildBlock.pChainHeight))

	if decisionToBuildBlock.transitionEpoch {
		sealingBlockSeq := parentBlock.Metadata.SimplexProtocolMetadata.Epoch
		if parentBlock.Type() == BlockTypeSealing {
			sealingBlockSeq = parentBlock.Metadata.SimplexProtocolMetadata.Seq
		}
		isSealingBlockFinalized, err := sm.isFinalized(sealingBlockSeq)
		if err != nil {
			return nil, err
		}

		if isSealingBlockFinalized {
			sm.Logger.Debug("Transitioning epoch after building block", zap.Uint64("newPChainRefHeight", decisionToBuildBlock.pChainHeight))
			newSimplexEpochInfo.NextPChainReferenceHeight = decisionToBuildBlock.pChainHeight
		}
	}

	now := sm.GetTime()
	icmEpochInfo := computeICMEpochInfo(parentBlock, sm.ComputeICMEpoch, now)

	var innerBlock avalanchego.VMBlock

	if decisionToBuildBlock.buildInnerBlock {
		innerBlock, err = sm.BlockBuilder.BuildBlock(ctx, icmEpochInfo.PChainEpochHeight)
		if err != nil {
			if errors.Is(context.Cause(ctx), common.ErrShouldBuildEmptyBlock) {
				return wrapBlock(nil, newSimplexEpochInfo, decisionToBuildBlock.pChainHeight, simplexMetadata, simplexBlacklist, now, parentBlock.Metadata.Timestamp, icmEpochInfo, nil), nil
			}
			return nil, err
		}
	}

	return wrapBlock(innerBlock, newSimplexEpochInfo, decisionToBuildBlock.pChainHeight, simplexMetadata, simplexBlacklist, now, parentBlock.Metadata.Timestamp, icmEpochInfo, nil), nil
}

func (sm *StateMachine) isFinalized(blockSeq uint64) (bool, error) {
	_, finalization, err := sm.GetBlock(blockSeq, common.Digest{})
	if err != nil {
		return false, fmt.Errorf("failed to retrieve block (%d): %w", blockSeq, err)
	}
	return finalization != nil, nil
}

func computeICMEpochInfo(parentBlock *StateMachineBlock, computeICMEpoch ICMEpochTransition, childTimestamp time.Time) ICMEpochInfo {
	parentTimestamp := time.UnixMilli(int64(parentBlock.Metadata.Timestamp))

	if childTimestamp.Before(parentTimestamp) {
		childTimestamp = parentTimestamp
	}

	icmEpochInfo := computeICMEpoch(ICMEpochInput{
		ParentPChainHeight: parentBlock.Metadata.PChainHeight,
		ParentEpoch: ICMEpochInfo{
			PChainEpochHeight: parentBlock.Metadata.ICMEpochInfo.PChainEpochHeight,
			EpochNumber:       parentBlock.Metadata.ICMEpochInfo.EpochNumber,
			EpochStartTime:    parentBlock.Metadata.ICMEpochInfo.EpochStartTime,
		},
		ChildTimestamp:  childTimestamp,
		ParentTimestamp: parentTimestamp,
	})
	return icmEpochInfo
}

func verifyAgainstExpected(
	ctx context.Context,
	parentBlock *StateMachineBlock,
	innerBlock avalanchego.VMBlock,
	expectedSimplexEpochInfo SimplexEpochInfo,
	expectedEpoch uint64,
	expectedPChainHeight uint64,
	nextBlock *StateMachineBlock,
	timestamp time.Time,
	expectedIcmEpochInfo ICMEpochInfo,
	auxInfo *AuxiliaryInfoBatch,
) error {
	if epoch := nextBlock.Metadata.SimplexProtocolMetadata.Epoch; epoch != expectedEpoch {
		return fmt.Errorf("%w: got %d, expected %d", errInvalidProtocolMetadataEpoch, epoch, expectedEpoch)
	}

	// Verify the metadata matches the expected values, only afterwards verify the inner block, if any.
	expectedBlock := wrapBlock(
		innerBlock, expectedSimplexEpochInfo, expectedPChainHeight,
		nextBlock.Metadata.SimplexProtocolMetadata, nextBlock.Metadata.SimplexBlacklist, timestamp, parentBlock.Metadata.Timestamp, expectedIcmEpochInfo, auxInfo)
	if expectedBlock.Digest() != nextBlock.Digest() {
		return fmt.Errorf("expected block digest %s does not match proposed block digest %s: %w",
			expectedBlock.Digest(),
			nextBlock.Digest(),
			errBlockDigestMismatch)
	}

	if innerBlock != nil {
		if err := innerBlock.Verify(ctx, expectedIcmEpochInfo.PChainEpochHeight); err != nil {
			return err
		}
	}
	return nil
}

func (sm *StateMachine) verifyNormalBlock(ctx context.Context, parentBlock *StateMachineBlock, nextBlock *StateMachineBlock, prevBlockSeq uint64) error {
	newSimplexEpochInfo := SimplexEpochInfo{
		PChainReferenceHeight: parentBlock.Metadata.SimplexEpochInfo.PChainReferenceHeight,
		PrevVMBlockSeq:        computePrevVMBlockSeq(parentBlock, prevBlockSeq),
	}

	proposedPChainHeight := nextBlock.Metadata.PChainHeight

	timestamp := time.UnixMilli(int64(nextBlock.Metadata.Timestamp))

	icmEpochInfo := computeICMEpochInfo(parentBlock, sm.ComputeICMEpoch, timestamp)

	if err := sm.verifyNextPChainRefHeightNormal(parentBlock, nextBlock.Metadata.SimplexEpochInfo); err != nil {
		return fmt.Errorf("failed to verify next P-chain reference height for normal block: %w", err)
	}
	newSimplexEpochInfo.NextPChainReferenceHeight = nextBlock.Metadata.SimplexEpochInfo.NextPChainReferenceHeight

	return verifyAgainstExpected(ctx, parentBlock, nextBlock.InnerBlock, newSimplexEpochInfo, parentBlock.Metadata.SimplexProtocolMetadata.Epoch, proposedPChainHeight, nextBlock, timestamp, icmEpochInfo, nil)
}

func verifyPChainHeight(proposedPChainHeight uint64, currentPChainHeight uint64, prevPChainHeight uint64) error {
	if proposedPChainHeight > currentPChainHeight {
		return fmt.Errorf("%w: proposed %d, current %d",
			errPChainHeightTooBig, proposedPChainHeight, currentPChainHeight)
	}

	if prevPChainHeight > proposedPChainHeight {
		return fmt.Errorf("%w: proposed %d, parent %d",
			errPChainHeightSmallerThanParent, proposedPChainHeight, prevPChainHeight)
	}
	return nil
}

func (sm *StateMachine) verifyNextPChainRefHeightNormal(parentBlock *StateMachineBlock, next SimplexEpochInfo) error {
	prevMD := parentBlock.Metadata
	prev := prevMD.SimplexEpochInfo
	// Next P-chain height can only increase, not decrease.
	if next.NextPChainReferenceHeight > 0 && prev.PChainReferenceHeight > next.NextPChainReferenceHeight {
		return fmt.Errorf("%w: previous P-chain reference height is %d and the proposed P-chain reference height is %d", errPChainReferenceHeightDecreased, prev.PChainReferenceHeight, next.NextPChainReferenceHeight)
	}

	// If the previous block already has a next P-chain reference height,
	// we should keep the same next P-chain reference height until we reach it.
	if prev.NextPChainReferenceHeight > 0 {
		if next.NextPChainReferenceHeight != prev.NextPChainReferenceHeight {
			return fmt.Errorf("%w: expected %d but got %d", errPChainReferenceHeightMismatch, prev.NextPChainReferenceHeight, next.NextPChainReferenceHeight)
		}
		return nil
	}

	// If we reached here, then prev.NextPChainReferenceHeight == 0.
	// If the previous block's next P-chain reference height is 0, and the new block's next P-chain reference height is > 0,
	// we need to ensure that we have finalized the sealing block of the previous epoch.
	if next.NextPChainReferenceHeight > 0 {
		sealingBlockSeq := prevMD.SimplexProtocolMetadata.Epoch
		_, finalization, err := sm.GetBlock(sealingBlockSeq, common.Digest{})
		if err != nil {
			return fmt.Errorf("failed to retrieve sealing block for previous epoch (%d): %w", sealingBlockSeq, err)
		}
		if finalization == nil {
			return fmt.Errorf("%w: sealing block sequence %d", errPrevSealingBlockNotFinalized, sealingBlockSeq)
		}
	}

	// Make sure we have reached the next P-chain reference height, otherwise we won't be able to validate it.
	pChainHeight := sm.GetPChainHeightForVerifying()

	if pChainHeight < next.NextPChainReferenceHeight {
		return fmt.Errorf("%w: target %d, current %d", errPChainHeightNotReached, next.NextPChainReferenceHeight, pChainHeight)
	}

	// It might be that this block is the first block that has set the next P-chain reference height for the epoch,
	// so check if it has done so correctly by observing whether the validator set has indeed changed.

	// Else, !currentValidatorSet.Equal(newValidatorSet) || next.NextPChainReferenceHeight == 0
	// We first check the case for next.NextPChainReferenceHeight == 0,
	// because if it is, then we don't need to check the validator set at all and we return early.
	if next.NextPChainReferenceHeight == 0 {
		return nil
	}

	// parentBlock cannot be a sealing block because if it were, then prev.NextPChainReferenceHeight would be > 0, and we would have returned above.
	currentValidatorSet, err := getCurrentValidatorSet(parentBlock, sm.GetBlock)
	if err != nil {
		return fmt.Errorf("failed to get current validator set: %w", err)
	}

	newValidatorSet, err := sm.GetValidatorSet(next.NextPChainReferenceHeight)
	if err != nil {
		return err
	}

	// If the validator set doesn't change, we shouldn't have increased the next P-chain reference height.
	if currentValidatorSet.Equal(newValidatorSet) {
		return fmt.Errorf("%w: validator set at proposed next P-chain reference height %d matches previous block's P-chain reference height %d",
			errValidatorSetUnchanged, next.NextPChainReferenceHeight, prev.PChainReferenceHeight)
	}

	return nil
}

// verifyNextPChainRefHeightForNewEpoch validates the proposed NextPChainReferenceHeight on the
// first block of a new epoch.
// This handles a corner case where the first block of an epoch initiates an epoch transition.
// We cannot reuse verifyNextPChainRefHeightNormal here — the baseline
// for the validator-set change check is the new epoch's PChainReferenceHeight, not the parent's,
// as in verifyNextPChainRefHeightNormal.
func (sm *StateMachine) verifyNextPChainRefHeightForNewEpoch(expectedEpochInfo SimplexEpochInfo, next SimplexEpochInfo, currentValidatorSet NodeBLSMappings) error {
	// The first block of the epoch doesn't trigger an epoch change, we're all set.
	if next.NextPChainReferenceHeight == 0 {
		return nil
	}

	// Next P-chain reference height cannot be smaller than the P-chain reference height,
	// as the P-chain reference height itself cannot decrease, and the next P-chain reference height
	// becomes the P-chain reference height when we change epochs.
	if next.NextPChainReferenceHeight < expectedEpochInfo.PChainReferenceHeight {
		return fmt.Errorf("%w: new epoch P-chain reference height is %d and the proposed next P-chain reference height is %d",
			errPChainReferenceHeightDecreased, expectedEpochInfo.PChainReferenceHeight, next.NextPChainReferenceHeight)
	}

	// If we haven't reached this P-chain height yet, we cannot accept the next P-chain reference height,
	// because there is no way of querying the validator set for the next P-chain reference height.
	pChainHeight := sm.GetPChainHeightForVerifying()
	if pChainHeight < next.NextPChainReferenceHeight {
		return fmt.Errorf("%w: target %d, current %d", errPChainHeightNotReached, next.NextPChainReferenceHeight, pChainHeight)
	}

	newValidatorSet, err := sm.GetValidatorSet(next.NextPChainReferenceHeight)
	if err != nil {
		return err
	}

	if currentValidatorSet.Equal(newValidatorSet) {
		return fmt.Errorf("%w: validator set at proposed next P-chain reference height %d matches new epoch's P-chain reference height %d",
			errValidatorSetUnchanged, next.NextPChainReferenceHeight, expectedEpochInfo.PChainReferenceHeight)
	}

	return nil
}

func (sm *StateMachine) createBlockBuildingDecider(currentValidatorSet NodeBLSMappings) blockBuildingDecider {
	blockBuildingDecider := blockBuildingDecider{
		logger:                   sm.Logger,
		maxBlockBuildingWaitTime: sm.MaxBlockBuildingWaitTime,
		pChainListener:           sm.PChainProgressListener,
		getPChainHeight:          sm.GetPChainHeightForProposing,
		waitForPendingBlock:      sm.BlockBuilder.WaitForPendingBlock,
		hasValidatorSetChanged: func(pChainHeight uint64) (bool, error) {
			// The given pChainHeight was sampled by the caller of shouldTransitionEpoch().
			// We compare between the current validator set, defined by the P-chain reference height in the parent block,
			// and the new validator set defined by the given pChainHeight.
			// If they are different, then we should transition to a new epoch.

			newValidatorSet, err := sm.GetValidatorSet(pChainHeight)
			if err != nil {
				return false, err
			}

			if !currentValidatorSet.Equal(newValidatorSet) {
				sm.Logger.Debug("Validator set has changed, should transition epoch",
					zap.String("currentValidatorSet", fmt.Sprintf("%v", currentValidatorSet.Nodes())),
					zap.String("newValidatorSet", fmt.Sprintf("%v", newValidatorSet.Nodes())),
					zap.Uint64("newPChainHeight", pChainHeight))
				return true, nil
			}
			return false, nil
		},
	}
	return blockBuildingDecider
}

// buildBlockZero builds the first ever block for Simplex,
// which is a special block that introduces the first validator set and starts the first epoch.
//
// How the epoch (EN, carried in SimplexProtocolMetadata), PrevSealingBlockHash (PSH),
// and SealingBlockSeq (SBS) evolve along the block chain (Seq = block sequence number; h(n) = digest of
// the block at sequence n):
//
//		────────────────── Epoch 1 ────────────────────────────────────│─── Epoch s ────
//		                                                               │
//		Seq:     z          ...     s            s+1     ...    s+x    │ s+1  (Telocks get pruned) ...
//		       ┌──────┐            ┌────────┐  ┌──────┐       ┌──────┐ │ ┌────────────┐
//		       │ Zero │    ...     │Sealing │  │Telock│  ...  │Telock│ │ │first block │  ...
//		       │ block│            │ block  │  │      │       │      │ │ │ of epoch s │
//		       └──────┘            └────────┘  └──────┘       └──────┘ │ └────────────┘
//		       EN  = 1             EN  = 1     EN  = 1        EN  = 1  │ EN  = s
//		       SBS = 0             SBS = 0     SBS = s        SBS = s  │ SBS = 0
//		       PSH = 0             PSH = h(z)  PSH = 0        PSH = 0  │ PSH = 0
//
//		- EN  : copied within an epoch; on the first block of a new epoch, EN
//		        equals the sequence number of the previous epoch's sealing block.
//	         The first epoch number is set to the sequence number of that block.
//		- PSH : only set on a sealing block. In the first epoch it points to the zero block;
//		        otherwise it points to the previous epoch's sealing block.
//		- SBS : 0 except on Telocks of a sealed-but-not-yet-finalized epoch, where
//		        it equals the sequence number of that epoch's sealing block.
func (sm *StateMachine) buildBlockZero(parentBlock StateMachineBlock, simplexMetadata common.ProtocolMetadata, simplexBlacklist common.Blacklist) (*StateMachineBlock, error) {
	pChainHeight := sm.LastNonSimplexBlockPChainHeight

	var validatorSet NodeBLSMappings
	if sm.LastNonSimplexInnerBlock.Height() == 0 {
		validatorSet = sm.GenesisValidatorSet
	} else {
		var err error
		validatorSet, err = sm.GetValidatorSet(pChainHeight)
		if err != nil {
			return nil, err
		}
	}

	var prevVMBlockSeq uint64
	if parentBlock.InnerBlock != nil {
		prevVMBlockSeq = parentBlock.InnerBlock.Height()
	} else {
		// We can only have blocks without inner blocks in Simplex blocks, but this is the first Simplex block.
		// Therefore, the parent block must have an inner block.
		sm.Logger.Error("Parent block has no inner block, cannot determine previous VM block sequence for zero block")
		return nil, errZeroBlockParentNoInnerBlock
	}

	// For the zero block, we set the timestamp to be the same as the last non-Simplex inner block's timestamp.
	// We do it because we need to carry over a minimum timestamp from the non-Simplex blocks.
	timestamp := sm.LastNonSimplexInnerBlock.Timestamp().UnixMilli()
	simplexEpochInfo := constructSimplexZeroBlockSimplexEpochInfo(pChainHeight, validatorSet, prevVMBlockSeq)

	// The zero block builds on top of the last non-Simplex block, which is identified by its inner block's
	// digest rather than by a Simplex block digest, and sits right above it in the sequence.
	simplexMetadata.Prev = sm.LastNonSimplexInnerBlock.Digest()
	simplexMetadata.Seq = sm.LastNonSimplexInnerBlock.Height() + 1

	// The zero block carries over the parent's ICM epoch unchanged, just as it carries over the
	// timestamp. If the parent is a genesis block that predates ICM, the carried-over epoch is empty,
	// and the first ICM epoch begins on the block built on top of the zero block.
	parentICMEpochInfo := parentBlock.Metadata.ICMEpochInfo
	icmEpochInfo := ICMEpochInfo{
		PChainEpochHeight: parentICMEpochInfo.PChainEpochHeight,
		EpochNumber:       parentICMEpochInfo.EpochNumber,
		EpochStartTime:    parentICMEpochInfo.EpochStartTime,
	}

	return wrapBlock(nil,
		simplexEpochInfo,
		pChainHeight,
		simplexMetadata,
		simplexBlacklist,
		time.UnixMilli(timestamp),
		parentBlock.Metadata.Timestamp,
		icmEpochInfo,
		nil,
	), nil
}

func (sm *StateMachine) verifyBlockZero(block *StateMachineBlock, prevBlock StateMachineBlock) error {
	if prevBlock.InnerBlock == nil {
		return fmt.Errorf("%w: parent digest %s", errZeroBlockParentNoInnerBlock, prevBlock.Digest())
	}

	pChainHeight := sm.LastNonSimplexBlockPChainHeight
	prevVMBlockSeq := prevBlock.InnerBlock.Height()

	if block.Metadata.PChainHeight != pChainHeight {
		return fmt.Errorf("%w: got %d, expected %d",
			errInvalidPChainHeight, block.Metadata.PChainHeight, pChainHeight)
	}

	// The zero block opens the first epoch, which is numbered after the zero block's own sequence.
	expectedEpoch := prevVMBlockSeq + 1
	if epoch := block.Metadata.SimplexProtocolMetadata.Epoch; epoch != expectedEpoch {
		return fmt.Errorf("%w: got %d, expected %d", errInvalidProtocolMetadataEpoch, epoch, expectedEpoch)
	}

	var expectedValidatorSet NodeBLSMappings
	if prevVMBlockSeq == 0 {
		expectedValidatorSet = sm.GenesisValidatorSet
	} else {
		var err error
		expectedValidatorSet, err = sm.GetValidatorSet(pChainHeight)
		if err != nil {
			return fmt.Errorf("failed to retrieve validator set at height %d: %w", pChainHeight, err)
		}
	}

	now := sm.GetTime()
	if err := verifyTimestamp(block, &prevBlock, now, sm.TimeSkewLimit); err != nil {
		return fmt.Errorf("failed to verify timestamp for zero block: %w", err)
	}

	// The zero block carries over the parent's ICM epoch unchanged (see buildBlockZero).
	expectedICMEpochInfo := prevBlock.Metadata.ICMEpochInfo

	// If we have compared all fields so far, the rest of the fields we compare by constructing an explicit expected SimplexEpochInfo
	expectedSimplexEpochInfo := constructSimplexZeroBlockSimplexEpochInfo(pChainHeight, expectedValidatorSet, prevVMBlockSeq)
	expectedBlock := wrapBlock(nil,
		expectedSimplexEpochInfo,
		pChainHeight,
		block.Metadata.SimplexProtocolMetadata,
		block.Metadata.SimplexBlacklist,
		time.UnixMilli(int64(block.Metadata.Timestamp)),
		prevBlock.Metadata.Timestamp,
		expectedICMEpochInfo,
		nil,
	)

	if expectedBlock.Digest() != block.Digest() {
		return fmt.Errorf("%w: expected %s but got %s", errBlockDigestMismatch, expectedBlock.Digest(), block.Digest())
	}

	// The InnerBlock must match the last non-Simplex inner block.
	if block.InnerBlock != nil {
		return errZeroBlockHasInnerBlock
	}

	// The zero block must build upon the last non-Simplex block
	if block.Metadata.SimplexProtocolMetadata.Prev != sm.LastNonSimplexInnerBlock.Digest() {
		return errZeroBlockPrevDigestMismatch
	}
	if block.Metadata.SimplexProtocolMetadata.Seq != sm.LastNonSimplexInnerBlock.Height()+1 {
		return errZeroBlockSeqMismatch
	}

	// The timestamp must equal the last non-Simplex inner block's timestamp.
	// We do it because we need to carry over a minimum timestamp from the non-Simplex blocks.
	expectedTimestamp := uint64(sm.LastNonSimplexInnerBlock.Timestamp().UnixMilli())
	if block.Metadata.Timestamp != expectedTimestamp {
		return fmt.Errorf("%w: expected %d but got %d", errZeroBlockTimestampMismatch, expectedTimestamp, block.Metadata.Timestamp)
	}

	return nil
}

// buildBlockCollectingApprovals builds either another collecting-approvals block (if not enough approvals yet)
// or a sealing block (if quorum is reached).
//
// Relevant fields (EN = SimplexProtocolMetadata.Epoch, NPCH = NextPChainReferenceHeight,
// NEA = NextEpochApprovals, BVD = BlockValidationDescriptor, PSH = PrevSealingBlockHash):
//
//	parent (Collecting)              not enough approvals yet           quorum of approvals reached: sealing block
//	┌──────────────────┐             ┌────────────────────┐             ┌────────────────────────────┐
//	│ EN   = e         │             │ EN   = e           │             │ EN   = e                   │
//	│ NPCH = p'        │   ────►     │ NPCH = p'          │             │ NPCH = p'                  │
//	│ NEA  = A_old     │             │ NEA  = A_old ∪ new │     OR      │ NEA  = A_old ∪ new         │
//	│ BVD  = nil       │             │ BVD  = nil         │             │ BVD  = validator set at p' │
//	│                  │             │                    │             │ PSH  = h(prev epoch's      │
//	│                  │             │                    │             │        sealing block)      │
//	└──────────────────┘             └────────────────────┘             └────────────────────────────┘
//	                                 → stays Collecting                 → BuildBlockEpochSealed
func (sm *StateMachine) buildBlockCollectingApprovals(ctx context.Context, parentBlock *StateMachineBlock, simplexMetadata common.ProtocolMetadata, simplexBlacklist common.Blacklist, prevBlockSeq uint64) (*StateMachineBlock, error) {
	// We prepare information that is needed to compute the approvals for the new epoch,
	// such as the validator set for the next epoch, and the approvals from peers.
	prevBlockNextPChainReferenceHeight := parentBlock.Metadata.SimplexEpochInfo.NextPChainReferenceHeight
	validators, err := sm.GetValidatorSet(prevBlockNextPChainReferenceHeight)
	if err != nil {
		return nil, err
	}

	auxInfoHistory, err := GetAuxiliaryHistory(parentBlock, prevBlockSeq, sm.GetBlock, sm.AuxiliaryInfoApp.DefaultVersionID())
	if err != nil {
		return nil, err
	}

	isAuxInfoReadyForEpochTransition, err := sm.AuxiliaryInfoApp.IsSufficient(auxInfoHistory.OldestVersionID, validators, auxInfoHistory.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to check if auxiliary info history is final: %w", err)
	}

	var newApprovals *approvals
	if isAuxInfoReadyForEpochTransition {
		newApprovals, err = sm.computeNewApprovals(parentBlock, validators, auxInfoHistory.LastHistoryDigest())
		if err != nil {
			return nil, err
		}
	} else {
		// We're not ready for epoch transition yet, so putting a zero-value approvals here
		// makes us stay in the collecting approvals state without contributing any approvals.
		newApprovals = &approvals{}
	}

	newSimplexEpochInfo := computeSimplexEpochInfoForCollectingApprovalsBlock(parentBlock, prevBlockSeq, newApprovals)

	// The block being built belongs to the epoch of its parent, as the epoch only advances
	// once the sealing block that follows is finalized.
	parentEpoch := parentBlock.Metadata.SimplexProtocolMetadata.Epoch
	pChainHeight := parentBlock.Metadata.PChainHeight

	now := sm.GetTime()
	icmEpochInfo := computeICMEpochInfo(parentBlock, sm.ComputeICMEpoch, now)
	auxInfo, err := sm.buildAuxInfoBatch(auxInfoHistory, parentBlock, validators, !isAuxInfoReadyForEpochTransition)
	if err != nil {
		return nil, fmt.Errorf("failed to build the auxiliary info batch: %w", err)
	}
	// We might not have enough approvals to seal the current epoch,
	// in which case we just carry over the approvals we have so far to the next block,
	// so that eventually we'll have enough approvals to seal the epoch.
	if !newApprovals.canSeal {
		sm.Logger.Debug("Not enough approvals to seal epoch, building block without sealing the epoch")
		return sm.buildBlockImpatiently(ctx, parentBlock, now, simplexMetadata, simplexBlacklist, newSimplexEpochInfo, pChainHeight, icmEpochInfo, auxInfo)
	}

	sm.Logger.Debug("Have enough approvals to seal epoch, building sealing block")

	// Else, we have enough approvals to seal the epoch, so we create the sealing block.
	return sm.createSealingBlock(ctx, parentBlock, now, simplexMetadata, simplexBlacklist, newSimplexEpochInfo, parentEpoch, pChainHeight, icmEpochInfo, auxInfo)
}

func (sm *StateMachine) verifyCollectingApprovalsBlock(ctx context.Context, parentBlock *StateMachineBlock, nextBlock *StateMachineBlock, prevBlockSeq uint64) error {
	prevEpochInfo := parentBlock.Metadata.SimplexEpochInfo
	nextEpochInfo := nextBlock.Metadata.SimplexEpochInfo
	parentEpoch := parentBlock.Metadata.SimplexProtocolMetadata.Epoch

	validators, err := sm.GetValidatorSet(prevEpochInfo.NextPChainReferenceHeight)
	if err != nil {
		return err
	}

	newApprovals := nextBlock.Metadata.SimplexEpochInfo.NextEpochApprovals

	expectedAuxInfo, auxInfoDigest, isAuxInfoReady, err := sm.computeExpectedAuxInfoForApprovalCollection(parentBlock, nextBlock, prevBlockSeq, validators)
	if err != nil {
		return fmt.Errorf("failed to compute expected auxiliary info for approval collection: %w", err)
	}

	// If the Auxiliary info is ready for epoch change, the block builder should at least include its own approval in the block it builds,
	// so we should have some approvals in the proposed block.
	if newApprovals == nil || len(newApprovals.NodeIDs) == 0 || len(newApprovals.Signature) == 0 {
		if isAuxInfoReady {
			return errEmptyNextEpochApprovals
		}
	}

	// If we aren't ready for epoch transition, we cannot collect approvals just yet.
	// So just make an empty NextEpochApprovals.
	if !isAuxInfoReady {
		if newApprovals != nil && (len(newApprovals.NodeIDs) > 0 || len(newApprovals.Signature) > 0) {
			return fmt.Errorf("expected no approvals when auxiliary info is not ready for epoch transition, but got some")
		}
		newSimplexEpochInfo := computeSimplexEpochInfoForCollectingApprovalsBlock(parentBlock, prevBlockSeq, &approvals{})
		timestamp := time.UnixMilli(int64(nextBlock.Metadata.Timestamp))
		icmEpochInfo := computeICMEpochInfo(parentBlock, sm.ComputeICMEpoch, timestamp)

		return verifyAgainstExpected(ctx, parentBlock, nextBlock.InnerBlock, newSimplexEpochInfo, parentEpoch, nextBlock.Metadata.PChainHeight, nextBlock, timestamp, icmEpochInfo, expectedAuxInfo)
	}

	newSimplexEpochInfo := computeSimplexEpochInfoForCollectingApprovalsBlock(parentBlock, prevBlockSeq, &approvals{
		nodeIDs:   newApprovals.NodeIDs,
		signature: newApprovals.Signature,
	})

	err = sm.verifyNextEpochApprovalsSignature(parentBlock.Metadata, nextBlock.Metadata, validators, auxInfoDigest)
	if err != nil {
		return err
	}

	// A node cannot remove other nodes' approvals, only add its own approval if it wasn't included in the previous block.
	// So the set of signers in next.NextEpochApprovals should be a superset of the set of signers in prev.NextEpochApprovals.
	if err := areNextEpochApprovalsSignersSupersetOfApprovalsOfPrevBlock(prevEpochInfo, nextEpochInfo); err != nil {
		return err
	}

	sigAggr := sm.SignatureAggregatorCreator(validators.Nodes())
	approvals := avalanchego.BitmaskFromBytes(newApprovals.NodeIDs)
	canSeal := sigAggr.IsQuorum(validators.SelectSubset(approvals))

	if canSeal {
		newSimplexEpochInfo, err = sm.computeSimplexEpochInfoForSealingBlock(newSimplexEpochInfo, parentEpoch)
		if err != nil {
			return fmt.Errorf("failed to compute simplex epoch info for sealing block: %w", err)
		}
	}

	timestamp := time.UnixMilli(int64(nextBlock.Metadata.Timestamp))
	icmEpochInfo := computeICMEpochInfo(parentBlock, sm.ComputeICMEpoch, timestamp)

	return verifyAgainstExpected(ctx, parentBlock, nextBlock.InnerBlock, newSimplexEpochInfo, parentEpoch, nextBlock.Metadata.PChainHeight, nextBlock, timestamp, icmEpochInfo, expectedAuxInfo)
}

func (sm *StateMachine) verifyNextEpochApprovalsSignature(prevMD StateMachineMetadata, nextMD StateMachineMetadata, validators NodeBLSMappings, auxInfoDigest common.Digest) error {
	prev := prevMD.SimplexEpochInfo
	next := nextMD.SimplexEpochInfo

	// First figure out which validators are approving the next epoch by looking at the bitmask of approving nodes,
	// and then aggregate their public keys together to verify the signature.

	nodeIDsBitmask := next.NextEpochApprovals.NodeIDs
	aggPK, err := sm.aggregatePubKeysForBitmask(nodeIDsBitmask, validators)
	if err != nil {
		return err
	}

	pChainHeight := prev.NextPChainReferenceHeight

	toBeSigned, err := assembleApprovalToBeSigned(pChainHeight, auxInfoDigest)
	if err != nil {
		return err
	}

	if err := sm.SignatureVerifier.VerifySignature(toBeSigned, next.NextEpochApprovals.Signature, aggPK); err != nil {
		return fmt.Errorf("failed to verify signature: %w", err)
	}
	return nil
}

// assembleApprovalToBeSigned assembles the payload that is signed when approving an epoch transition.
// It consists of the P-chain reference height and the aux info digest (zeroed if not applicable).
func assembleApprovalToBeSigned(pChainHeight uint64, auxInfoDigest common.Digest) ([]byte, error) {
	payloadToSignBuff := make([]byte, 8+32) // 8 bytes for the P-chain height and 32 bytes for the aux info digest if applicable.
	binary.BigEndian.PutUint64(payloadToSignBuff[:8], pChainHeight)
	copy(payloadToSignBuff[8:], auxInfoDigest[:])

	signedMsg := common.SignedMessage{Payload: payloadToSignBuff, Context: signatureContext}
	return asn1.Marshal(signedMsg)
}

func SignApproval(signer common.Signer, nextPChainReferenceHeight uint64, auxInfoDigest common.Digest) ([]byte, error) {
	toBeSigned, err := assembleApprovalToBeSigned(nextPChainReferenceHeight, auxInfoDigest)
	if err != nil {
		return nil, err
	}

	sig, err := signer.Sign(toBeSigned)
	if err != nil {
		return nil, fmt.Errorf("failed to sign approval: %w", err)
	}
	return sig, nil
}

func (sm *StateMachine) aggregatePubKeysForBitmask(nodeIDsBitmask []byte, validators NodeBLSMappings) ([]byte, error) {
	approvingNodes := avalanchego.BitmaskFromBytes(nodeIDsBitmask)

	// Check that the bitmask is not wider than the number of validators, otherwise it would imply that some approving nodes are not in the validator set.
	if bitLen := approvingNodes.BitLen(); bitLen > len(validators) {
		return nil, fmt.Errorf("%w: highest set bit implies %d nodes, but there are %d validators",
			errApprovalsBitmaskTooWide, bitLen, len(validators))
	}

	publicKeys := make([]common.PublicKeyBytes, 0, len(validators))
	for i := range validators {
		if !approvingNodes.Contains(i) {
			continue
		}
		publicKeys = append(publicKeys, validators[i].BLSKey)
	}

	aggPK, err := sm.KeyAggregator.AggregateKeys(publicKeys...)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate public keys: %w", err)
	}
	return aggPK, nil
}

func computeSimplexEpochInfoForCollectingApprovalsBlock(parentBlock *StateMachineBlock, prevBlockSeq uint64, newApprovals *approvals) SimplexEpochInfo {
	// The P-chain reference height should remain the same until we transition to the new epoch.
	// The next P-chain reference height should have been set in the previous block,
	// which is the reason why we are collecting approvals in the first place.
	newSimplexEpochInfo := SimplexEpochInfo{
		PChainReferenceHeight:     parentBlock.Metadata.SimplexEpochInfo.PChainReferenceHeight,
		NextPChainReferenceHeight: parentBlock.Metadata.SimplexEpochInfo.NextPChainReferenceHeight,
		PrevVMBlockSeq:            computePrevVMBlockSeq(parentBlock, prevBlockSeq),
	}

	// This might be the first time we created approvals for the next epoch,
	// so we need to initialize the NextEpochApprovals.
	if newSimplexEpochInfo.NextEpochApprovals == nil {
		newSimplexEpochInfo.NextEpochApprovals = &NextEpochApprovals{}
	}
	// The node IDs and signature are aggregated across all past and present approvals.
	newSimplexEpochInfo.NextEpochApprovals.NodeIDs = newApprovals.nodeIDs
	newSimplexEpochInfo.NextEpochApprovals.Signature = newApprovals.signature
	return newSimplexEpochInfo
}

func (sm *StateMachine) computeNewApprovals(parentBlock *StateMachineBlock, validators NodeBLSMappings, auxInfoDigest common.Digest) (*approvals, error) {
	prevBlockNextPChainReferenceHeight := parentBlock.Metadata.SimplexEpochInfo.NextPChainReferenceHeight
	sigAggr := sm.SignatureAggregatorCreator(validators.Nodes())

	// We retrieve approvals that validators have sent us for the next epoch.
	// These approvals are signed by validators of the next epoch.
	// The store is nil until a transitioning block is indexed, which may happen after we build on it.
	sm.lock.RLock()
	approvalStore := sm.approvalStore
	sm.lock.RUnlock()
	var approvalsFromPeers ValidatorSetApprovals
	if approvalStore != nil {
		approvalsFromPeers = approvalStore.Approvals()
	}
	sm.Logger.Debug("Retrieved approvals from peers", zap.Int("numApprovals", len(approvalsFromPeers)))

	// Optimistically sign the epoch transition even if we have already did so in a previous round.
	// We'll just deduplicate this approval later on.
	sig, err := SignApproval(sm.Signer, prevBlockNextPChainReferenceHeight, auxInfoDigest)
	if err != nil {
		return nil, err
	}

	approvalsFromPeers = append(approvalsFromPeers, common.ValidatorSetApproval{
		NodeID:        avalanchego.NodeID(sm.MyNodeID),
		PChainHeight:  prevBlockNextPChainReferenceHeight,
		AuxInfoDigest: auxInfoDigest,
		Signature:     sig,
	})

	sm.Logger.Debug("Retrieved approvals from peers", zap.Int("numApprovals", len(approvalsFromPeers)))

	nextPChainHeight := prevBlockNextPChainReferenceHeight
	prevNextEpochApprovals := parentBlock.Metadata.SimplexEpochInfo.NextEpochApprovals

	newApprovals, err := computeNewApprovals(prevNextEpochApprovals, approvalsFromPeers, nextPChainHeight, auxInfoDigest, sigAggr, validators, sm.Logger)
	if err != nil {
		return nil, err
	}
	return newApprovals, nil
}

// buildBlockImpatiently builds a block by waiting for the VM to build a block until MaxBlockBuildingWaitTime.
// If the VM fails to build a block within that time, we build a block without an inner block,
// so that we can continue making progress and not get stuck waiting for the VM.
func (sm *StateMachine) buildBlockImpatiently(ctx context.Context,
	parentBlock *StateMachineBlock,
	timestamp time.Time,
	simplexMetadata common.ProtocolMetadata,
	simplexBlacklist common.Blacklist,
	simplexEpochInfo SimplexEpochInfo,
	pChainHeight uint64,
	icmEpochInfo ICMEpochInfo,
	auxInfo *AuxiliaryInfoBatch) (*StateMachineBlock, error) {
	impatientContext, cancel := context.WithTimeout(ctx, sm.MaxBlockBuildingWaitTime)
	defer cancel()

	start := sm.GetTime()

	innerBlock, err := sm.BlockBuilder.BuildBlock(impatientContext, icmEpochInfo.PChainEpochHeight)
	if err != nil && impatientContext.Err() == nil {
		// If we got an error building the block, and we didn't time out, log the error but continue building the block without the inner block,
		// so that we can continue making progress and not get stuck on a single block.
		sm.Logger.Error("Error building block, building block without inner block instead", zap.Error(err))
	}
	if impatientContext.Err() != nil {
		sm.Logger.Debug("Timed out waiting for block to be built, building block without inner block instead",
			zap.Duration("elapsed", time.Since(start)), zap.Duration("maxBlockBuildingWaitTime", sm.MaxBlockBuildingWaitTime))
	}

	return wrapBlock(innerBlock, simplexEpochInfo, pChainHeight, simplexMetadata, simplexBlacklist, timestamp, parentBlock.Metadata.Timestamp, icmEpochInfo, auxInfo), nil
}

func (sm *StateMachine) createSealingBlock(ctx context.Context,
	parentBlock *StateMachineBlock,
	timestamp time.Time,
	simplexMetadata common.ProtocolMetadata,
	simplexBlacklist common.Blacklist,
	simplexEpochInfo SimplexEpochInfo,
	epoch uint64,
	pChainHeight uint64,
	icmEpochInfo ICMEpochInfo,
	auxInfo *AuxiliaryInfoBatch) (*StateMachineBlock, error) {
	simplexEpochInfo, err := sm.computeSimplexEpochInfoForSealingBlock(simplexEpochInfo, epoch)
	if err != nil {
		return nil, fmt.Errorf("failed to compute simplex epoch info for sealing block: %w", err)
	}
	return sm.buildBlockImpatiently(ctx, parentBlock, timestamp, simplexMetadata, simplexBlacklist, simplexEpochInfo, pChainHeight, icmEpochInfo, auxInfo)
}

// computeSimplexEpochInfoForSealingBlock completes the epoch info of a sealing block of the given epoch.
// The epoch number is the sequence of the previous epoch's sealing block, which the new sealing block points to.
func (sm *StateMachine) computeSimplexEpochInfoForSealingBlock(simplexEpochInfo SimplexEpochInfo, epoch uint64) (SimplexEpochInfo, error) {
	validators, err := sm.GetValidatorSet(simplexEpochInfo.NextPChainReferenceHeight)
	if err != nil {
		return SimplexEpochInfo{}, err
	}
	if simplexEpochInfo.BlockValidationDescriptor == nil {
		simplexEpochInfo.BlockValidationDescriptor = &BlockValidationDescriptor{}
	}
	simplexEpochInfo.BlockValidationDescriptor.AggregatedMembership.Members = validators

	prevSealingBlock, finalization, err := sm.GetBlock(epoch, common.Digest{})
	if err != nil {
		sm.Logger.Error("Error retrieving previous sealing block", zap.Uint64("seq", epoch), zap.Error(err))
		return SimplexEpochInfo{}, fmt.Errorf("failed to retrieve previous sealing InnerBlock at epoch %d: %w", epoch, err)
	}
	if finalization == nil {
		sm.Logger.Error("Previous sealing block is not finalized", zap.Uint64("seq", epoch))
		return SimplexEpochInfo{}, fmt.Errorf("%w: epoch %d", errPrevSealingBlockNotFinalized, epoch)
	}
	simplexEpochInfo.PrevSealingBlockHash = prevSealingBlock.Digest()

	return simplexEpochInfo, nil
}

// getCurrentValidatorSet retrieves the validator set corresponding to the epoch of the given block.
// The given block must not be a sealing block or a Telock, because the method is expected to be called on the parent block of a block,
// and therefore the validator set of the block is determined by the sealing block, not what is returned from this function.
// Calling this function on a Telock or a sealing block will return an error.
func getCurrentValidatorSet(parentBlock *StateMachineBlock, getBlock BlockRetriever) (NodeBLSMappings, error) {
	if parentBlock.Type() == BlockTypeSealing || parentBlock.Type() == BlockTypeTelock {
		return nil, fmt.Errorf("getCurrentValidatorSet: did not expect block %d to be a sealing block or a Telock",
			parentBlock.Metadata.SimplexProtocolMetadata.Seq)
	}
	epoch := parentBlock.Metadata.SimplexProtocolMetadata.Epoch
	block, finalization, err := getBlock(epoch, common.Digest{}) // The sealing block of the current epoch should be finalized.
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve sealing block %d: %w", epoch, err)
	}

	var bvd *BlockValidationDescriptor
	if block.Type() == BlockTypeZero {
		bvd = block.Metadata.SimplexEpochInfo.BlockValidationDescriptor
	} else {
		if finalization == nil {
			return nil, fmt.Errorf("sealing block %d is not finalized", epoch)
		}
		bvd = block.Metadata.SimplexEpochInfo.BlockValidationDescriptor
	}

	if bvd == nil || len(bvd.AggregatedMembership.Members) == 0 {
		return nil, fmt.Errorf("block %d has no validators", epoch)
	}

	return bvd.AggregatedMembership.Members, nil
}

// wrapBlock creates a new StateMachineBlock by wrapping the VM block (if applicable) and adding the appropriate metadata.
func wrapBlock(
	innerBlock avalanchego.VMBlock,
	newSimplexEpochInfo SimplexEpochInfo,
	pChainHeight uint64,
	simplexMetadata common.ProtocolMetadata,
	simplexBlacklist common.Blacklist,
	timestamp time.Time,
	parentTimestamp uint64,
	icmEpochInfo ICMEpochInfo,
	auxiliaryInfo *AuxiliaryInfoBatch) *StateMachineBlock {

	parentTime := time.UnixMilli(int64(parentTimestamp))

	if timestamp.Before(parentTime) {
		timestamp = parentTime
	}

	return &StateMachineBlock{
		InnerBlock: innerBlock,
		Metadata: StateMachineMetadata{
			Timestamp:               uint64(timestamp.UnixMilli()),
			SimplexProtocolMetadata: simplexMetadata,
			SimplexBlacklist:        simplexBlacklist,
			SimplexEpochInfo:        newSimplexEpochInfo,
			PChainHeight:            pChainHeight,
			ICMEpochInfo:            icmEpochInfo,
			AuxiliaryInfoBatch:      auxiliaryInfo,
		},
	}
}

func (sm *StateMachine) areWeReadyToTransitionEpoch(parentBlock *StateMachineBlock, prevBlockSeq uint64) (bool, uint64, StateMachineBlock, error) {
	sealingBlockSeq := parentBlock.Metadata.SimplexEpochInfo.SealingBlockSeq

	// If the sealing block sequence is still 0, it means previous block was the sealing block.
	if sealingBlockSeq == 0 {
		sealingBlockSeq = prevBlockSeq
	}

	if sealingBlockSeq == 0 {
		return false, 0, StateMachineBlock{}, errSealingBlockSeqUnset
	}

	sealingBlock, finalization, err := sm.GetBlock(sealingBlockSeq, common.Digest{})
	if err != nil {
		return false, 0, StateMachineBlock{}, fmt.Errorf("failed to retrieve sealing block at sequence %d: %w", sealingBlockSeq, err)
	}

	return finalization != nil, sealingBlockSeq, sealingBlock, nil
}

// buildBlockEpochSealed builds a block where the epoch is being sealed due to a sealing block already created in this epoch.
//
// Relevant fields (PCH = PChainReferenceHeight, EN = SimplexProtocolMetadata.Epoch,
// NPCH = NextPChainReferenceHeight, SBS = SealingBlockSeq, BVD = BlockValidationDescriptor):
//
//	parent (sealing block)        sealing block NOT finalized      sealing block IS finalized
//	                              → emit Telock (no inner block)   → first block of new epoch
//	┌──────────────────┐          ┌──────────────────┐             ┌──────────────────────────┐
//	│ Seq  = s         │          │ Seq  = s+1       │             │ Seq  = s+1               │
//	│ PCH  = p         │          │ PCH  = p (copy)  │             │ PCH  = p' (was NPCH)     │
//	│ EN   = e         │   ──►    │ EN   = e (copy)  │      OR     │ EN   = s                 │
//	│ NPCH = p'        │    OR    │ NPCH = p' (copy) │             │ NPCH = 0  (reset)        │
//	│ SBS  = 0         │          │ SBS  = s         │             │ SBS  = 0                 │
//	│ BVD  = vset@p'   │          │ BVD  = nil       │             │ BVD  = nil               │
//	└──────────────────┘          └──────────────────┘             └──────────────────────────┘
//	                              → stays EpochSealed              → NormalOp (new epoch)
func (sm *StateMachine) buildBlockEpochSealed(ctx context.Context, parentBlock *StateMachineBlock, simplexMetadata common.ProtocolMetadata, simplexBlacklist common.Blacklist, prevBlockSeq uint64) (*StateMachineBlock, error) {
	// We check if the sealing block has already been finalized.
	// If not, we build a Telock block.
	readyToTransitionEpoch, sealingBlockSeq, sealingBlock, err := sm.areWeReadyToTransitionEpoch(parentBlock, prevBlockSeq)
	if err != nil {
		return nil, err
	}

	if !readyToTransitionEpoch {
		now := sm.GetTime()
		icmEpochInfo := computeICMEpochInfo(parentBlock, sm.ComputeICMEpoch, now)
		newSimplexEpochInfo := computeSimplexEpochInfoForTelock(parentBlock, sealingBlockSeq, prevBlockSeq)
		pChainHeight := parentBlock.Metadata.PChainHeight
		return wrapBlock(nil, newSimplexEpochInfo, pChainHeight, simplexMetadata, simplexBlacklist, now, parentBlock.Metadata.Timestamp, icmEpochInfo, nil), nil
	}

	// Else, we build a block for the new epoch.
	newSimplexEpochInfo := computeSimplexEpochInfoForNewEpoch(parentBlock, prevBlockSeq)

	return sm.buildBlockOrTransitionEpoch(ctx, &sealingBlock, simplexMetadata, simplexBlacklist, newSimplexEpochInfo)
}

func computeSimplexEpochInfoForNewEpoch(parentBlock *StateMachineBlock, prevBlockSeq uint64) SimplexEpochInfo {
	newSimplexEpochInfo := SimplexEpochInfo{
		// P-chain reference height is previous block's NextPChainReferenceHeight.
		PChainReferenceHeight: parentBlock.Metadata.SimplexEpochInfo.NextPChainReferenceHeight,
		PrevVMBlockSeq:        computePrevVMBlockSeq(parentBlock, prevBlockSeq),
	}
	return newSimplexEpochInfo
}

func computeSimplexEpochInfoForTelock(parentBlock *StateMachineBlock, sealingBlockSeq uint64, prevBlockSeq uint64) SimplexEpochInfo {
	newSimplexEpochInfo := SimplexEpochInfo{
		PChainReferenceHeight:     parentBlock.Metadata.SimplexEpochInfo.PChainReferenceHeight,
		NextPChainReferenceHeight: parentBlock.Metadata.SimplexEpochInfo.NextPChainReferenceHeight,
		SealingBlockSeq:           sealingBlockSeq,
		PrevVMBlockSeq:            computePrevVMBlockSeq(parentBlock, prevBlockSeq),
	}
	return newSimplexEpochInfo
}

func (sm *StateMachine) verifyBlockEpochSealed(ctx context.Context, parentBlock *StateMachineBlock, nextBlock *StateMachineBlock, prevBlockSeq uint64) error {
	isSealingBlockFinalized, sealingBlockSeq, sealingBlock, err := sm.areWeReadyToTransitionEpoch(parentBlock, prevBlockSeq)
	if err != nil {
		return err
	}

	timestamp := time.UnixMilli(int64(nextBlock.Metadata.Timestamp))

	icmEpochInfo := computeICMEpochInfo(parentBlock, sm.ComputeICMEpoch, timestamp)

	newSimplexEpochInfo := computeSimplexEpochInfoForTelock(parentBlock, sealingBlockSeq, prevBlockSeq)

	if !isSealingBlockFinalized {
		return verifyAgainstExpected(ctx, parentBlock, nil, newSimplexEpochInfo, parentBlock.Metadata.SimplexProtocolMetadata.Epoch, nextBlock.Metadata.PChainHeight, nextBlock, timestamp, icmEpochInfo, nil)
	}

	bvd := sealingBlock.Metadata.SimplexEpochInfo.BlockValidationDescriptor
	if bvd == nil || len(bvd.AggregatedMembership.Members) == 0 {
		return fmt.Errorf("sealing block %d has no BlockValidationDescriptor", sealingBlockSeq)
	}

	currentValidatorSet := bvd.AggregatedMembership.Members

	// Else, it's a new epoch.
	newSimplexEpochInfo = computeSimplexEpochInfoForNewEpoch(parentBlock, prevBlockSeq)

	// The first block of the new epoch may itself transition again, so trust and validate
	// the proposed pchain height and (optional) next pchain reference height, mirroring
	// what buildBlockOrTransitionEpoch does on the build side.
	proposedPChainHeight := nextBlock.Metadata.PChainHeight
	currentPChainHeight := sm.GetPChainHeightForVerifying()
	prevPChainHeight := parentBlock.Metadata.PChainHeight
	if err := verifyPChainHeight(proposedPChainHeight, currentPChainHeight, prevPChainHeight); err != nil {
		return fmt.Errorf("failed to verify P-chain height: %w", err)
	}

	if err := sm.verifyNextPChainRefHeightForNewEpoch(newSimplexEpochInfo, nextBlock.Metadata.SimplexEpochInfo, currentValidatorSet); err != nil {
		return fmt.Errorf("failed to verify next P-chain reference height for new epoch block: %w", err)
	}
	newSimplexEpochInfo.NextPChainReferenceHeight = nextBlock.Metadata.SimplexEpochInfo.NextPChainReferenceHeight

	return verifyAgainstExpected(ctx, parentBlock, nextBlock.InnerBlock, newSimplexEpochInfo, sealingBlockSeq, proposedPChainHeight, nextBlock, timestamp, icmEpochInfo, nil)
}

// computeExpectedAuxInfoForApprovalCollection computes the expected AuxiliaryInfo that should be included in the proposed block
// for approval collection, and returns the auxiliary info digest, and whether the auxiliary info history is ready for epoch transition.
func (sm *StateMachine) computeExpectedAuxInfoForApprovalCollection(parentBlock *StateMachineBlock, nextBlock *StateMachineBlock, prevBlockSeq uint64, validators NodeBLSMappings) (*AuxiliaryInfoBatch, common.Digest, bool, error) {
	nextMD := nextBlock.Metadata
	prevMD := parentBlock.Metadata

	auxInfoHistory, err := GetAuxiliaryHistory(parentBlock, prevBlockSeq, sm.GetBlock, sm.AuxiliaryInfoApp.DefaultVersionID())
	if err != nil {
		return nil, common.Digest{}, false, err
	}

	if len(auxInfoHistory.Data) > 0 && nextMD.AuxiliaryInfoBatch == nil {
		// If we have auxiliary info history but the proposed block doesn't include any auxiliary info,
		// it means the block builder has dropped the auxiliary info, which is not allowed.
		return nil, common.Digest{}, false, fmt.Errorf("expected auxiliary info for application %d with history length %d, but got nil", auxInfoHistory.OldestVersionID, len(auxInfoHistory.Data))
	}

	// Else, either len(auxInfoHistory) == 0,
	// or the proposed block includes auxiliary info.
	// Both of these cases are fine, because a node doesn't have to include Auxiliary information.
	// We will verify the legality of the proposed auxiliary info (if any) in the next step.

	var expectedAuxInfo *AuxiliaryInfoBatch
	var proposedAuxInfos []common.AuxiliaryInfo

	if nextMD.AuxiliaryInfoBatch != nil {
		proposedAuxInfos = nextMD.AuxiliaryInfoBatch.data
		expectedAuxInfo = &AuxiliaryInfoBatch{
			data: proposedAuxInfos,
		}
		if prevMD.AuxiliaryInfoBatch != nil {
			expectedAuxInfo.PrevAuxInfoSeq = auxInfoHistory.LastSeq
		}
	}

	// go through all the collected data and return whether proposed datum are legal,
	// checking each datum against the history including the datums before it in the batch
	legalHistory := auxInfoHistory.Data
	for _, info := range proposedAuxInfos {
		if auxInfoHistory.OldestVersionID != info.Version {
			return nil, common.Digest{}, false, fmt.Errorf("proposed auxiliary info does not have the proper version %d", auxInfoHistory.OldestVersionID)
		}
		if err := sm.AuxiliaryInfoApp.IsLegalAppend(auxInfoHistory.OldestVersionID, validators, legalHistory, info.Data); err != nil {
			return nil, common.Digest{}, false, fmt.Errorf("%w for application %d: %w", errAuxInfoIllegalAppend, auxInfoHistory.OldestVersionID, err)
		}

		legalHistory = append(legalHistory, info.Data)
	}

	// We check sufficiency on the on-chain history rather than legalHistory to match
	// the builder, which checks sufficiency before collecting new info.
	auxInfoReady, err := sm.AuxiliaryInfoApp.IsSufficient(auxInfoHistory.OldestVersionID, validators, auxInfoHistory.Data)
	if err != nil {
		return nil, common.Digest{}, false, fmt.Errorf("failed to check if auxiliary info history is final for application %d: %w", auxInfoHistory.OldestVersionID, err)
	}

	var digest common.Digest
	if auxInfoReady {
		digest = auxInfoHistory.LastHistoryDigest()
	}

	return expectedAuxInfo, digest, auxInfoReady, nil
}

// buildAuxInfoBatch builds the AuxiliaryInfoBatch that should be included in the block being built.
func (sm *StateMachine) buildAuxInfoBatch(history AuxInfoHistory, parentBlock *StateMachineBlock, validators NodeBLSMappings, shouldGenerate bool) (*AuxiliaryInfoBatch, error) {
	var prevAuxInfoSeq uint64
	if parentBlock.Metadata.AuxiliaryInfoBatch != nil {
		prevAuxInfoSeq = history.LastSeq
	}

	var info []common.AuxiliaryInfo
	if shouldGenerate {
		info = sm.auxInfoStore.collectAuxInfo(history, validators)
	}

	// Only emit a batch when there's new info to record, or a prior batch in the chain
	// to link back to. An empty batch with PrevAuxInfoSeq == 0 would violate the invariant
	// that an empty batch points to an ancestor with non-empty entries.
	if len(info) == 0 && prevAuxInfoSeq == 0 {
		return nil, nil
	}

	return &AuxiliaryInfoBatch{
		data:           info,
		PrevAuxInfoSeq: prevAuxInfoSeq,
	}, nil
}

// constructSimplexZeroBlockSimplexEpochInfo constructs the SimplexEpochInfo for the zero block, which is the first ever block built by Simplex.
func constructSimplexZeroBlockSimplexEpochInfo(pChainHeight uint64, newValidatorSet NodeBLSMappings, prevVMBlockSeq uint64) SimplexEpochInfo {
	newSimplexEpochInfo := SimplexEpochInfo{
		PChainReferenceHeight: pChainHeight,
		// We treat the zero block as a special case, and we encode in it the block validation descriptor,
		// despite it not actually being a sealing block. This is because the zero block is the first block that introduces the validator set.
		BlockValidationDescriptor: &BlockValidationDescriptor{
			AggregatedMembership: AggregatedMembership{
				Members: newValidatorSet,
			},
		},
		NextEpochApprovals:        nil, // We don't need to collect approvals to seal the first ever epoch.
		PrevVMBlockSeq:            prevVMBlockSeq,
		SealingBlockSeq:           0,               // We don't have a sealing block in the zero block.
		PrevSealingBlockHash:      common.Digest{}, // The zero block has no previous sealing block.
		NextPChainReferenceHeight: 0,
	}
	return newSimplexEpochInfo
}

func computeNewApprovals(
	prevNextEpochApprovals *NextEpochApprovals,
	approvalsFromPeers ValidatorSetApprovals,
	pChainHeight uint64,
	auxInfoDigest common.Digest,
	sigAggr common.SignatureAggregator,
	validators NodeBLSMappings,
	logger common.Logger,
) (*approvals, error) {
	if prevNextEpochApprovals == nil {
		prevNextEpochApprovals = &NextEpochApprovals{}
	}

	oldApprovingNodes := avalanchego.BitmaskFromBytes(prevNextEpochApprovals.NodeIDs)

	nodeID2ValidatorIndex := validators.IndexByNodeID()

	oldApprovalFromPeersCount := len(approvalsFromPeers)
	// We have the approvals obtained from peers, but we need to sanitize them by filtering out approvals that are not valid,
	// such as approvals that do not agree with our candidate auxiliary info digest and P-Chain height,
	// and approvals that are from nodes that are not in the validator set or have already approved in prior blocks.
	approvalsFromPeers = sanitizeApprovals(approvalsFromPeers, pChainHeight, auxInfoDigest, nodeID2ValidatorIndex, oldApprovingNodes, logger)
	logger.Debug("Sanitized approvals after filtering out invalid approvals", zap.Int("numApprovalsBefore", oldApprovalFromPeersCount), zap.Int("numApprovalsAfter", len(approvalsFromPeers)))

	// Next we aggregate both previous and new approvals to compute the new aggregated signatures and the new bitmask of approving nodes.
	aggregatedSignature, newApprovingNodes, err := computeNewApproverSignaturesAndSigners(prevNextEpochApprovals, approvalsFromPeers, oldApprovingNodes, nodeID2ValidatorIndex, sigAggr, logger)
	if err != nil {
		return nil, err
	}

	// we check if we have enough approvals to seal the epoch by computing the relative approval ratio,
	// which is the ratio of the total weight of approving nodes divided by the total weight of all validators.
	canSeal := sigAggr.IsQuorum(validators.SelectSubset(newApprovingNodes))

	return &approvals{
		canSeal:   canSeal,
		signature: aggregatedSignature,
		nodeIDs:   newApprovingNodes.Bytes(),
	}, nil
}

// computeNewApproverSignaturesAndSigners computes the signatures of the nodes that approve the next epoch including the previous aggregated signature,
// and bitmask of nodes that correspond to those signatures, and aggregates all signatures together.
func computeNewApproverSignaturesAndSigners(
	nextEpochApprovals *NextEpochApprovals,
	approvalsFromPeers ValidatorSetApprovals,
	oldApprovingNodes avalanchego.Bitmask,
	nodeID2ValidatorIndex map[avalanchego.NodeID]int,
	sigAggr common.SignatureAggregator,
	logger common.Logger,
) ([]byte, avalanchego.Bitmask, error) {
	if nextEpochApprovals == nil {
		return nil, avalanchego.Bitmask{}, errEmptyNextEpochApprovals
	}
	// Prepare the new signatures from the new approvals that haven't approved yet and that agree with our candidate auxiliary info digest and P-Chain height.
	newSignatures := make([]common.SignatureBytes, 0, len(approvalsFromPeers)+1)

	// We will overwrite the old approving nodes with the new approving nodes, by turning on the bits for the new approvers.
	newApprovingNodes := oldApprovingNodes.Clone()

	logger.Debug("Existing approving nodes bitmask before adding new approvals",
		zap.Int("count", oldApprovingNodes.Len()))

	logger.Debug("New approvals from peers that we will consider for aggregation", zap.Int("count", len(approvalsFromPeers)))

	for _, approval := range approvalsFromPeers {
		approvingNodeIndexOfNewApprover, exists := nodeID2ValidatorIndex[approval.NodeID]
		if !exists {
			// This should not happen, because we have already filtered approvals that are not in the validator set, but we check just in case.
			continue
		}

		// Check if the node has already approved in the past.
		if newApprovingNodes.Contains(approvingNodeIndexOfNewApprover) {
			continue
		}

		// Turn on the bit for the new approver
		newApprovingNodes.Add(approvingNodeIndexOfNewApprover)
		newSignatures = append(newSignatures, approval.Signature)
	}

	// Add the existing signature into the list of signatures to aggregate
	existingSignature := nextEpochApprovals.Signature

	// Finally, we aggregate all signatures together, to compute the new aggregated signature.
	aggregatedSignature, err := sigAggr.AppendSignatures(existingSignature, newSignatures...)
	if err != nil {
		return nil, avalanchego.Bitmask{}, fmt.Errorf("failed to aggregate signatures: %w", err)
	}

	return aggregatedSignature, newApprovingNodes, nil
}

// sanitizeApprovals filters out approvals that are not valid by checking if they agree with our candidate auxiliary info digest and P-Chain height,
// and if they are from the validator set and haven't already been approved.
func sanitizeApprovals(approvals ValidatorSetApprovals, pChainHeight uint64, auxInfoDigest common.Digest, nodeID2ValidatorIndex map[avalanchego.NodeID]int, oldApprovingNodes avalanchego.Bitmask, logger common.Logger) ValidatorSetApprovals {
	filter1 := approvalsThatAgreeWithPChainHeightAndAuxInfoDigest(pChainHeight, auxInfoDigest)
	filter2 := approvalsThatAreInValidatorSetAndHaveNotAlreadyApproved(oldApprovingNodes.Clone(), nodeID2ValidatorIndex)
	return approvals.Filter(filter1, logger).Filter(filter2, logger).UniqueByNodeID()
}

func approvalsThatAgreeWithPChainHeightAndAuxInfoDigest(pChainHeight uint64, auxInfoDigest common.Digest) func(approval common.ValidatorSetApproval, logger common.Logger) bool {
	return func(approval common.ValidatorSetApproval, logger common.Logger) bool {
		// Pick only approvals that agree with our P-Chain height
		ok := approval.PChainHeight == pChainHeight && approval.AuxInfoDigest == auxInfoDigest
		if !ok {
			logger.Debug("Filtering out approval that does not agree with our P-Chain height or auxiliary info digest",
				zap.String("nodeID", fmt.Sprintf("%x", approval.NodeID)),
				zap.Uint64("approvalPChainHeight", approval.PChainHeight),
				zap.Uint64("expectedPChainHeight", pChainHeight),
				zap.Stringer("approvalAuxInfoSeqDigest", approval.AuxInfoDigest))
		}
		return ok
	}
}

func approvalsThatAreInValidatorSetAndHaveNotAlreadyApproved(oldApprovingNodes avalanchego.Bitmask, nodeID2ValidatorIndex map[avalanchego.NodeID]int) func(approval common.ValidatorSetApproval, logger common.Logger) bool {
	return func(approval common.ValidatorSetApproval, logger common.Logger) bool {
		approvingNodeIndexOfNewApprover, exists := nodeID2ValidatorIndex[approval.NodeID]
		if !exists {
			logger.Debug("Filtering out approval from node that is not in the validator set",
				zap.String("nodeID", fmt.Sprintf("%x", approval.NodeID)))
			// If the approving node is not in the validator set, we ignore this approval.
			return false
		}
		// Only pick approvals from nodes that haven't already approved
		return !oldApprovingNodes.Contains(approvingNodeIndexOfNewApprover)
	}
}

// computePrevVMBlockSeq computes the block sequence of the previous VM block (inner block).
// The block sequence of the previous VM block is the number of VM blocks that have been built since genesis.
func computePrevVMBlockSeq(parentBlock *StateMachineBlock, prevBlockSeq uint64) uint64 {
	// Either our parent block has no inner block, in which case we just inherit its previous VM block sequence,
	if parentBlock.InnerBlock == nil {
		return parentBlock.Metadata.SimplexEpochInfo.PrevVMBlockSeq
	}
	// or it has an inner block, in which case it is the previous block sequence.
	return prevBlockSeq
}

func areNextEpochApprovalsSignersSupersetOfApprovalsOfPrevBlock(prev SimplexEpochInfo, next SimplexEpochInfo) error {
	if prev.NextEpochApprovals == nil || len(prev.NextEpochApprovals.NodeIDs) == 0 {
		return nil
	}
	if next.NextEpochApprovals == nil {
		return fmt.Errorf("%w: previous block has next epoch approvals but proposed block doesn't have next epoch approvals", errNextEpochApprovalsShrunk)
	}
	// Make sure that previous signers are still there.
	prevSigners := avalanchego.BitmaskFromBytes(prev.NextEpochApprovals.NodeIDs)
	nextSigners := avalanchego.BitmaskFromBytes(next.NextEpochApprovals.NodeIDs)
	// Remove all bits in nextSigners from prevSigners
	prevSigners.Difference(&nextSigners)
	// If we have some bits left, it means there was a bit in prevSigners that wasn't in nextSigners
	if prevSigners.Len() > 0 {
		return errSignerSetShrunk
	}
	return nil
}

type approvals struct {
	canSeal   bool
	nodeIDs   []byte
	signature []byte
}
