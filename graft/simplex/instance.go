// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/ava-labs/simplex/avalanchego"
	"github.com/ava-labs/simplex/common"
	metadata "github.com/ava-labs/simplex/msm"
	"github.com/ava-labs/simplex/nonvalidator"
	"github.com/ava-labs/simplex/simplex"
	"github.com/ava-labs/simplex/wal"
	"go.uber.org/zap"
)

var errAlreadyStarted = errors.New("instance already started")

const (
	// tickInterval is the interval at which the instance will call AdvanceTime on the current epoch or non-validator.
	tickInterval = time.Millisecond * 100
)

type Config struct {
	// LastNonSimplexInnerBlock is the last non-simplex inner block that was persisted to storage.
	// This is used to determine the current epoch and validator set.
	LastNonSimplexInnerBlock avalanchego.VMBlock
	// ParameterConfig is the configuration for the simplex instance.
	ParameterConfig ParameterConfig
	// PlatformChain is the interface to the P-chain.
	PlatformChain PlatformChain
	// Broadcaster is the interface to broadcast messages to other nodes in the network.
	Broadcaster Broadcaster
	// Sender is an interface to send messages to a specific node in the network
	Sender Sender
	// CryptoOps is the interface to the cryptographic operations needed by the simplex instance.
	CryptoOps CryptoOps
	// WalCreator is the interface to create new write-ahead logs for the simplex instance.
	WalCreator wal.Creator
	// Storage is the interface to the block storage layer for the simplex instance.
	Storage        Storage
	Logger         common.Logger
	WALs           []wal.DeletableWAL
	VM             VM
	ICMETransition metadata.ICMEpochTransition
	ID             common.NodeID
}

type epochChange struct {
	epoch      uint64
	validators common.Nodes
}
type timeAdvancer interface {
	AdvanceTime(t time.Time)
}

type Instance struct {
	Config Config

	lock                       sync.Mutex
	started                    bool
	cs                         *CachedStorage
	transitionListener         *epochTransitionListener
	wal                        *wal.GarbageCollectedWAL
	msm                        *metadata.StateMachine
	e                          *simplex.Epoch
	nv                         *nonvalidator.NonValidator
	epochOrNV                  timeAdvancer
	epochChanges               chan epochChange
	stopCh                     chan struct{}
	futureEpochMessageListener *nonvalidator.FutureEpochListener
}

func NewInstance(config Config) *Instance {
	cs := NewCachedStorage(config.Storage, config.LastNonSimplexInnerBlock.Height())
	// Non-validators have no block builder, so they pass a nil approval handler:
	// they broadcast approvals but do not need to record their own locally.
	transitionListener := newEpochTransitionListener(
		config.Logger,
		config.Sender,
		avalanchego.NodeID(config.ID),
		config.PlatformChain.GetValidatorSet,
		cs.RetrieveBlock,
		config.CryptoOps,
		&NoopAuxiliaryInfoApp{}, // TODO: set this in the config
		nil,
	)

	// Override the PlatformChain in the config with a ValidatorCache that wraps the original PlatformChain.
	// This is done to avoid repeated calls to GetValidatorSet for the same height, which can be expensive.
	config.PlatformChain = &ValidatorCache{
		PlatformChain: config.PlatformChain,
	}

	if config.ParameterConfig.WALGCInterval == 0 {
		config.ParameterConfig.WALGCInterval = DefaultWALGCInterval
	}

	return &Instance{
		Config:             config,
		stopCh:             make(chan struct{}),
		epochChanges:       make(chan epochChange, 1),
		cs:                 cs,
		transitionListener: transitionListener,
	}
}

func (i *Instance) Start(ctx context.Context) error {
	// Hold the lock throughout startup to block HandleMessage from being called in between.
	i.lock.Lock()
	defer i.lock.Unlock()

	if i.started {
		return errAlreadyStarted
	}

	i.started = true

	context.AfterFunc(ctx, i.Stop)

	if err := i.maybeReplicateEpochs(); err != nil {
		return err
	}

	go i.tick()
	go i.listenForEpochChanges()

	return nil
}

func (i *Instance) maybeReplicateEpochs() error {
	i.Config.Logger.Debug("Checking if latest persisted validator set is up to date")
	latestValidatorSet, err := getLatestPlatformChainValidatorSet(i.Config.PlatformChain)
	if err != nil {
		return err
	}

	latestIndexedEpochValidators, err := getLastAcceptedValidatorSet(&i.Config)
	if err != nil {
		return err
	}

	// We have indexed the latest validator set, therefore we can skip epoch replication and start as a validator.
	// Note: this may not be the latest epoch, but a future PR will eventually notice we are behind and transition properly.
	upToDate := latestIndexedEpochValidators.Equal(latestValidatorSet.Nodes())
	inLatestSet := latestValidatorSet.Nodes().Contains(i.Config.ID)
	if upToDate && inLatestSet {
		i.Config.Logger.Debug("Skipping validator set replication, because the latest validator set is up to date with the Platform Chain")
		return i.startValidator(latestIndexedEpochValidators)
	}

	// Start as non-validator if our last indexed validator set does not equal, the latest p-chain validator set
	// Note: the epoch may be transitioning, so the latest p-chain validator set actually points to a future epoch.
	// The non-validator should finish replicating epochs and convert our non-validator to a validator in this case.
	i.Config.Logger.Info("Indexed validator set is behind the Platform Chain, starting as a non-validator",
		zap.Bool("IndexedSetUpToDate", upToDate), zap.Bool("InLatestSet", inLatestSet))
	return i.startNonValidator(false)
}

func (i *Instance) startValidator(validators common.Nodes) error {
	if err := i.maybeInitializeWAL(); err != nil {
		return err
	}

	epochConfig, err := i.createEpochConfig(validators)
	if err != nil {
		return err
	}

	epoch, err := simplex.NewEpoch(epochConfig.EpochConfig)
	if err != nil {
		return fmt.Errorf("error creating simplex epoch: %w", err)
	}

	i.e = epoch
	i.epochOrNV = epoch
	epochConfig.bbw.e = epoch

	if err := epoch.Start(); err != nil {
		return err
	}

	// set the future epoch listener
	i.futureEpochMessageListener = nonvalidator.NewFutureEpochListener(i.Config.Logger, i.Config.Sender, epoch.Epoch, i.latestValidatorSet, epochConfig.SignatureAggregatorCreator, i.onHigherSealingBlock)
	return nil
}

// startNonValidator runs a non-validator. epochsReplicated is true when we already hold the
// newest sealing block, such as when a validator leaves the validator set.
func (i *Instance) startNonValidator(epochsReplicated bool) error {
	config, err := i.createNonValidatorConfig(epochsReplicated)
	if err != nil {
		return err
	}

	nonValidator, err := nonvalidator.NewNonValidator(config)
	if err != nil {
		return fmt.Errorf("error creating non-validator: %w", err)
	}
	i.nv = nonValidator
	i.epochOrNV = nonValidator
	nonValidator.Start()
	return nil
}

func (i *Instance) createNonValidatorConfig(epochsReplicated bool) (nonvalidator.Config, error) {
	source, err := simplex.NewRandomSource()
	if err != nil {
		return nonvalidator.Config{}, err
	}

	// The validator set follows the P-chain tip while we replicate epochs.
	comm := &communication{
		validators:  i.latestValidatorSet,
		Sender:      i.Config.Sender,
		Broadcaster: i.Config.Broadcaster,
	}

	// Plant an artificial MSM. A non-validator never verifies the state machine transition,
	// it only verifies the inner block (see common.OnlyVMVerifyOpt), so this MSM is only
	// used to wire blocks and is never asked to verify them.
	i.msm = &metadata.StateMachine{
		Config: &metadata.Config{},
	}
	i.cs.msm = i.msm
	instanceStorage := NewCallbackStorage(i.cs, i.msm, func(block *ParsedBlock) error {
		switch {
		case block.Type() == metadata.BlockTypeTransitioning:
			if err := i.transitionListener.handleTransitionBlock(block); err != nil {
				return err
			}
		}
		return nil
	})

	config := nonvalidator.Config{
		ID:                         i.Config.ID,
		RandomSource:               source,
		Storage:                    instanceStorage,
		Comm:                       comm,
		Logger:                     i.Config.Logger,
		StartTime:                  time.Now(),
		SignatureAggregatorCreator: i.Config.CryptoOps.CreateSignatureAggregator,
		MaxSequenceWindow:          simplex.DefaultMaxRoundWindow,
		TransitionToValidator:      i.notifyEpochChange,
		EpochsReplicated:           epochsReplicated,
	}
	return config, nil
}

func (i *Instance) latestValidatorSet() common.Nodes {
	validators, err := getLatestPlatformChainValidatorSet(i.Config.PlatformChain)
	if err != nil {
		i.Config.Logger.Error("Failed to retrieve the latest P-chain validator set", zap.Error(err))
		return nil
	}
	return validators.Nodes()
}

func (i *Instance) notifyEpochChange(epoch uint64, validators common.Nodes) {
	i.Config.Logger.Debug("Notifying the instance of an epoch change", zap.Uint64("Epoch", epoch), zap.Stringers("Validators", validators.NodeIDs()))
	ec := epochChange{
		epoch:      epoch,
		validators: validators,
	}

	for {
		select {
		case i.epochChanges <- ec:
			return
		// The slot holds a stale epoch change: take it, keep the newer of the two and retry.
		case pending := <-i.epochChanges:
			if pending.epoch > ec.epoch {
				ec = pending
			}
		case <-i.stopCh:
			// If the instance is stopped, we don't need to notify about epoch changes.
			return
		}
	}
}

func (i *Instance) tick() {
	ticker := time.NewTicker(tickInterval)
	for {
		select {
		case now := <-ticker.C:
			i.lock.Lock()
			timeAdvancer := i.epochOrNV
			i.lock.Unlock()
			if timeAdvancer != nil {
				timeAdvancer.AdvanceTime(now)
			}
		case <-i.stopCh:
			return
		}
	}
}

func (i *Instance) isStopped() bool {
	select {
	case <-i.stopCh:
		return true
	default:
		return false
	}
}

func (i *Instance) Stop() {
	i.lock.Lock()
	defer i.lock.Unlock()

	select {
	case <-i.stopCh:
		// Already stopped, do nothing
		return
	default:
		close(i.stopCh)
	}

	i.stopValidator()
	if err := i.maybeCloseWAL(); err != nil {
		i.Config.Logger.Error("Error closing the WAL", zap.Error(err))
	}

	i.stopNonValidator()
}

// maybeCloseWAL closes the WAL currently in use, if any.
// Must be called under the lock, and only once the epoch using it has been stopped.
func (i *Instance) maybeCloseWAL() error {
	if i.wal == nil {
		return nil
	}

	if err := i.wal.Close(); err != nil {
		return err
	}
	i.wal = nil

	return nil
}

func (i *Instance) stopNonValidator() {
	if i.nv != nil {
		i.nv.Stop()
		i.nv = nil
		i.epochOrNV = nil
	}
}

func (i *Instance) stopValidator() {
	if i.e != nil {
		i.e.Stop()
		i.e = nil
		i.epochOrNV = nil
	}
}

func (i *Instance) HandleMessage(msg *common.Message, from common.NodeID) error {
	i.lock.Lock()
	defer i.lock.Unlock()

	select {
	case <-i.stopCh:
		i.Config.Logger.Debug("Instance is stopped, dropping message")
		return nil
	default:
	}

	if !i.started {
		i.Config.Logger.Debug("Instance has not started, dropping message")
		return nil
	}

	// We need to artificially wire the MSM and the cache to the block,
	// in order to intercept the Verify() call.
	switch {
	case msg.BlockMessage != nil:
		err := i.wireBlockMessage(msg)
		if err != nil {
			i.Config.Logger.Debug("Error wiring block message", zap.Error(err))
			return nil
		}
	case msg.ReplicationResponse != nil:
		err := i.wireReplicationResponse(msg)
		if err != nil {
			i.Config.Logger.Debug("Error wiring replication response message", zap.Error(err))
			return nil
		}
	}

	if i.e != nil {
		return i.handleMessageForEpoch(msg, from)
	}

	if i.nv != nil {
		return i.nv.HandleMessage(msg, from)
	}

	return errors.New("we are not running neither a validator or not validator")
}

func (i *Instance) handleMessageForEpoch(msg *common.Message, from common.NodeID) error {
	i.futureEpochMessageListener.HandleMessage(msg, from)

	switch {
	case msg.AuxiliaryInfo != nil:
		if msg.AuxiliaryInfo.Epoch != i.e.Epoch {
			i.Config.Logger.Debug(
				"Received an auxiliary info from an old epoch",
				zap.Uint64("Aux Info Epoch", msg.AuxiliaryInfo.Epoch),
				zap.Uint64("Our Epoch", i.e.Epoch),
				zap.Stringer("From", from))
			return nil
		}
		i.msm.HandleAuxiliaryInfo(*msg.AuxiliaryInfo, avalanchego.NodeID(from))
	case msg.EpochTransitionApproval != nil:
		if !from.Equals(msg.EpochTransitionApproval.NodeID[:]) {
			i.Config.Logger.Debug("Dropping approval not sent by its signer",
				zap.Stringer("from", from),
				zap.Stringer("signer", common.NodeID(msg.EpochTransitionApproval.NodeID[:])))
			return nil
		}
		i.msm.HandleApproval(msg.EpochTransitionApproval)
		return nil
	}

	return i.e.HandleMessage(msg, from)
}

func (i *Instance) wireReplicationResponse(msg *common.Message) error {
	resp := msg.ReplicationResponse
	if resp.LatestRound != nil && resp.LatestRound.Block != nil {
		block, err := i.wireBlock(resp.LatestRound.Block)
		if err != nil {
			return err
		}
		resp.LatestRound.Block = block
	}
	if resp.LatestSeq != nil && resp.LatestSeq.Block != nil {
		block, err := i.wireBlock(resp.LatestSeq.Block)
		if err != nil {
			return err
		}
		resp.LatestSeq.Block = block
	}
	for j, datum := range resp.Data {
		if datum.Block == nil {
			continue
		}
		block, err := i.wireBlock(datum.Block)
		if err != nil {
			return err
		}
		resp.Data[j].Block = block
	}
	return nil
}

func (i *Instance) wireBlock(block common.Block) (common.Block, error) {
	pb, isParsedBlock := block.(*ParsedBlock)
	if !isParsedBlock {
		return nil, fmt.Errorf("expected ParsedBlock, got %T", block)
	}
	block = &cachedBlock{
		cache:       i.cs,
		ParsedBlock: pb,
	}
	pb.msm = i.msm
	return block, nil
}

func (i *Instance) wireBlockMessage(msg *common.Message) error {
	block, err := i.wireBlock(msg.BlockMessage.Block)
	if err != nil {
		return err
	}
	msg.BlockMessage.Block = block
	return nil
}

func (i *Instance) listenForEpochChanges() {
	for {
		select {
		case epochChange := <-i.epochChanges:
			i.processEpochChange(epochChange)
		case <-i.stopCh:
			return
		}
	}
}

func (i *Instance) processEpochChange(epochChange epochChange) {
	// Hold the lock so the transition cannot interleave with Stop or HandleMessage.
	i.lock.Lock()

	if i.isStopped() {
		i.lock.Unlock()
		i.Config.Logger.Info("instance is already stopped, skipping epoch change")
		return
	}

	// Ensure the epoch is higher before transitioning
	if i.e != nil && i.e.Epoch >= epochChange.epoch {
		defer i.lock.Unlock()
		return
	}

	var err error

	runningNonValidator := i.nv != nil
	runningValidator := i.e != nil

	switch {
	case runningNonValidator && runningValidator:
		i.lock.Unlock()
		i.Config.Logger.Fatal("We are running both a validator or non-validator")
		return
	case runningNonValidator:
		// Stop the non-validator before doing anything else, so that we don't process any more messages while we are changing epochs.
		i.stopNonValidator()
		err = i.startAtEpoch(epochChange.validators)
	case runningValidator && i.Config.Storage.NumBlocks() <= epochChange.epoch:
		// The future epoch listener found a sealing block we have not indexed. A validator restarted
		// from storage would still be in our epoch, so replicate up to it as a non-validator.
		i.stopValidator()
		err = i.startNonValidator(false)
	case runningValidator:
		i.stopValidator()
		err = i.startAtEpoch(epochChange.validators)
	default: // This should never happen, but we log it just in case.
		i.lock.Unlock()
		i.Config.Logger.Fatal("We are not running either a validator or non-validator")
		return
	}
	i.lock.Unlock()

	if err != nil {
		i.Config.Logger.Error("Error transitioning epoch", zap.Error(err))
		i.Stop()
	}
}

// maybeInitializeWAL initializes the WAL if it hasn't been initialized yet.
func (i *Instance) maybeInitializeWAL() error {
	if i.wal == nil {
		gcWAL, err := wal.NewGarbageCollectedWAL(i.Config.WALs, i.Config.WalCreator, &common.WALReader{}, i.Config.ParameterConfig.WALMaxSizeBytes)
		if err != nil {
			return fmt.Errorf("error creating garbage collected wal: %w", err)
		}
		i.wal = gcWAL
	}

	return nil
}

func (i *Instance) createEpochConfig(validators common.Nodes) (*epochConfig, error) {
	numBlocks := i.Config.Storage.NumBlocks()
	if numBlocks == 0 {
		return nil, errors.New("no blocks indexed in storage")
	}

	msm, err := metadata.NewStateMachine(&metadata.Config{
		GetTime:                         time.Now,
		MyNodeID:                        i.Config.ID,
		KeyAggregator:                   i.Config.CryptoOps,
		GetValidatorSet:                 i.Config.PlatformChain.GetValidatorSet,
		SignatureVerifier:               i.Config.CryptoOps,
		PChainProgressListener:          i.Config.PlatformChain,
		LatestPersistedHeight:           numBlocks - 1,
		MaxBlockBuildingWaitTime:        i.Config.ParameterConfig.MaxNetworkDelay,
		Logger:                          i.Config.Logger,
		Signer:                          i.Config.CryptoOps,
		GenesisValidatorSet:             i.Config.PlatformChain.GenesisValidatorSet(),
		LastNonSimplexBlockPChainHeight: i.Config.PlatformChain.LastNonSimplexBlockPChainHeight(),
		SignatureAggregatorCreator:      i.Config.CryptoOps.CreateSignatureAggregator,
		BlockBuilder:                    i.Config.VM,
		LastNonSimplexInnerBlock:        i.Config.LastNonSimplexInnerBlock,
		GetPChainHeightForProposing:     i.Config.PlatformChain.GetMinimumHeight,
		GetPChainHeightForVerifying:     i.Config.PlatformChain.GetCurrentHeight,
		AuxiliaryInfoApp:                &NoopAuxiliaryInfoApp{},
		ComputeICMEpoch:                 i.Config.ICMETransition,
		GetBlock:                        i.cs.RetrieveBlock,
	})
	if err != nil {
		return nil, fmt.Errorf("error creating metadata state machine: %w", err)
	}

	i.msm = msm
	i.cs.msm = msm

	source, err := simplex.NewRandomSource()
	if err != nil {
		return nil, err
	}

	blockBuilder := newBlockBuilderWaiter(msm, i.cs, i.Config.VM)

	comm := &communication{
		validators:  func() common.Nodes { return validators },
		Sender:      i.Config.Sender,
		Broadcaster: i.Config.Broadcaster,
	}

	// set the handle approval method so that the MSM can receive self approvals
	i.transitionListener.handleApproval = msm.HandleApproval
	instanceStorage := NewCallbackStorage(i.cs, msm, func(block *ParsedBlock) error {
		if err := msm.OnBlockIndex(block.StateMachineBlock); err != nil {
			return err
		}
		switch {
		case block.Type() == metadata.BlockTypeTransitioning:
			if err := i.transitionListener.handleTransitionBlock(block); err != nil {
				return err
			}
		case block.Type() == metadata.BlockTypeSealing:
			blockBuilder.stop()

			// The epoch is sealed, so none of its WAL entries are needed anymore.
			if err := i.wal.GarbageCollect(math.MaxUint64); err != nil {
				i.Config.Logger.Error("Failed garbage collecting the WAL upon sealing the epoch",
					zap.Uint64("seq", block.BlockHeader().Seq), zap.Error(err))
				return err
			}

			i.transitionListener.handleApproval = nil
			i.notifyEpochChange(block.BlockHeader().Seq, block.SealingBlockInfo().ValidatorSet)
			return nil
		}
		return i.maybeGarbageCollectWAL(block)
	})

	ec := simplex.EpochConfig{
		ReplicationEnabled: true,
		StartTime:          time.Now(),
		// For simplicity, we use the same value for all timeouts. If needed we can expand the config.
		MaxProposalWait:            i.Config.ParameterConfig.MaxNetworkDelay * 2, // 1 proposal + 1 vote
		MaxRebroadcastWait:         i.Config.ParameterConfig.MaxNetworkDelay * 2,
		MaxRoundWindow:             i.Config.ParameterConfig.MaxRoundWindow,
		MaxBlacklistedRounds:       i.Config.ParameterConfig.MaxBlacklistedRounds,
		ID:                         i.Config.ID,
		RandomSource:               source, // Seed the random source from crypto/rand
		WAL:                        i.wal,
		Logger:                     i.Config.Logger,
		SignatureAggregatorCreator: i.Config.CryptoOps.CreateSignatureAggregator,
		QCDeserializer:             i.Config.CryptoOps,
		Signer:                     i.Config.CryptoOps,
		Verifier:                   i.Config.CryptoOps,
		Storage:                    instanceStorage,
		Comm:                       comm,
		BlockBuilder:               blockBuilder,
		BlockDeserializer:          &blockDeserializer{vm: i.Config.VM, cs: i.cs},
	}
	return &epochConfig{
		EpochConfig: ec,
		bbw:         blockBuilder,
	}, nil
}

// maybeGarbageCollectWAL garbage collects the WAL upon every WALGCInterval blocks, discarding the WAL files
// whose entries all belong to rounds below the one of the indexed block.
func (i *Instance) maybeGarbageCollectWAL(block *ParsedBlock) error {
	interval := i.Config.ParameterConfig.WALGCInterval
	bh := block.BlockHeader()
	if interval == 0 || bh.Seq%interval != 0 {
		return nil
	}
	// If a sequence is indexed, then any rounds below bh.Round can also be collected
	if err := i.wal.GarbageCollect(bh.Round); err != nil {
		i.Config.Logger.Error("Failed garbage collecting the WAL",
			zap.Uint64("seq", bh.Seq), zap.Uint64("round", bh.Round), zap.Error(err))
		return err
	}
	return nil
}

// startAtEpoch starts either a validator or non-validator at `epoch“.
func (i *Instance) startAtEpoch(validators common.Nodes) error {
	if validators.Contains(i.Config.ID) {
		return i.startValidator(validators)
	}

	return i.startNonValidator(true)
}

type epochConfig struct {
	simplex.EpochConfig
	bbw *blockBuilderWaiter
}

func (i *Instance) onHigherSealingBlock(sealingBlock *common.QuorumRound) {
	// this should only be called if we are running a validator
	if i.e == nil {
		i.Config.Logger.Info("Our future epoch message listener triggered a higher epoch while not running a validator", zap.Stringer("Sealing Block", sealingBlock))
		return
	}

	// the sealing block's sequence is the epoch it starts
	epoch := sealingBlock.Block.BlockHeader().Seq

	sealingInfo := sealingBlock.Block.SealingBlockInfo()
	i.notifyEpochChange(epoch, sealingInfo.ValidatorSet)
}
