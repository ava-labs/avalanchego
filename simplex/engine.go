// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/simplex"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"

	pSimplex "github.com/ava-labs/avalanchego/snow/consensus/simplex"
)

var _ common.Engine = (*Engine)(nil)

var errUnknownMessageType = errors.New("unknown message type")

type Engine struct {
	// nonValidator marks that this node is not a validator
	// this is included, but marked as a todo since the e2e tests currently
	// try and bootstrap a node that is not a validator(see func `CheckBootstrapIsPossible`).
	// The bootstrapped node is a non-validator which is currently not supported.
	// TODO: handle non-validators properly
	nonValidator bool

	// list of NoOpsHandler for messages dropped by engine
	common.AllGetsServer
	common.StateSummaryFrontierHandler
	common.AcceptedStateSummaryHandler
	common.AcceptedFrontierHandler
	common.AcceptedHandler
	common.AncestorsHandler
	common.PutHandler
	common.QueryHandler
	common.ChitsHandler

	// Handler that passes application messages to the VM
	common.AppHandler
	validators.Connector

	epoch              *simplex.Epoch
	blockDeserializer  *blockDeserializer
	quorumDeserializer *QCDeserializer
	logger             logging.Logger
	vm                 block.ChainVM
	consensusCtx       *snow.ConsensusContext

	tickInterval time.Duration
	shutdown     chan struct{}
}

// The VM must be initialized before creating the engine
func NewEngine(consensusCtx *snow.ConsensusContext, ctx context.Context, config *Config) (*Engine, error) {
	if isNonValidator(config) {
		config.Log.Info("Out node is not a validator for the subnet",
			zap.Stringer("nodeID", config.Ctx.NodeID),
			zap.Stringer("chainID", config.Ctx.ChainID),
			zap.Stringer("subnetID", config.Ctx.SubnetID),
		)
		return nonValidatingEngine(consensusCtx, config)
	}

	signer, verifier, err := NewBLSAuth(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create BLS auth: %w", err)
	}
	qcDeserializer := &QCDeserializer{
		verifier: &verifier,
	}

	signatureAggregator := &SignatureAggregator{
		verifier: &verifier,
	}
	comm, err := NewComm(config)
	if err != nil {
		return nil, err
	}

	storage, err := newStorage(ctx, config, qcDeserializer, &blockTracker{})
	if err != nil {
		return nil, err
	}

	if storage.NumBlocks() == 0 {
		return nil, errors.New("storage has no blocks")
	}

	lastBlock, _, err := storage.Retrieve(storage.NumBlocks() - 1)
	if err != nil {
		return nil, fmt.Errorf("couldn't find last block at height %d: %w", storage.NumBlocks()-1, err)
	}

	simplexBlock, ok := lastBlock.(*Block)
	if !ok {
		return nil, fmt.Errorf("expected last block to be of type *Block but got %T", lastBlock)
	}

	blockTracker := newBlockTracker(simplexBlock, config.VM)
	blockBuilder := &BlockBuilder{
		vm:           config.VM,
		blockTracker: blockTracker,
		log:          config.Log,
	}
	storage.blockTracker = blockTracker

	blockDeserializer := &blockDeserializer{
		parser:       config.VM,
		blockTracker: blockTracker,
	}

	epochConfig := simplex.EpochConfig{
		MaxProposalWait:     config.Params.MaxProposalWait,
		MaxRebroadcastWait:  config.Params.MaxRebroadcastWait,
		QCDeserializer:      qcDeserializer,
		Logger:              config.Log,
		ID:                  config.Ctx.NodeID[:],
		Signer:              &signer,
		Verifier:            &verifier,
		BlockDeserializer:   blockDeserializer,
		SignatureAggregator: signatureAggregator,
		Comm:                comm,
		Storage:             storage,
		WAL:                 config.WAL,
		BlockBuilder:        blockBuilder,
		Epoch:               simplexBlock.metadata.Epoch,
		StartTime:           time.Now(),
		ReplicationEnabled:  true,
	}

	epoch, err := simplex.NewEpoch(epochConfig)
	if err != nil {
		return nil, err
	}

	return &Engine{
		epoch:                       epoch,
		blockDeserializer:           blockDeserializer,
		quorumDeserializer:          qcDeserializer,
		logger:                      config.Log,
		StateSummaryFrontierHandler: common.NewNoOpStateSummaryFrontierHandler(config.Log),
		AcceptedStateSummaryHandler: common.NewNoOpAcceptedStateSummaryHandler(config.Log),
		AcceptedFrontierHandler:     common.NewNoOpAcceptedFrontierHandler(config.Log),
		AcceptedHandler:             common.NewNoOpAcceptedHandler(config.Log),
		AncestorsHandler:            common.NewNoOpAncestorsHandler(config.Log),
		PutHandler:                  common.NewNoOpPutHandler(config.Log),
		QueryHandler:                common.NewNoOpQueryHandler(config.Log),
		ChitsHandler:                common.NewNoOpChitsHandler(config.Log),
		Connector:                   config.VM,

		tickInterval: getTickInterval(config.Params),
		AppHandler:   config.VM,
		vm:           config.VM,
		consensusCtx: consensusCtx,
	}, nil
}

func (e *Engine) Start(ctx context.Context, _ uint32) error {
	if e.nonValidator {
		e.logger.Info("non-validator cannot start simplex engine")
		return nil
	}

	e.logger.Info("Starting simplex engine")
	err := e.epoch.Start()
	if err != nil {
		return fmt.Errorf("failed to start simplex epoch: %w", err)
	}

	go e.tick()
	e.consensusCtx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_CHAIN,
		State: snow.NormalOp,
	})
	return e.vm.SetState(ctx, snow.NormalOp)
}

func getTickInterval(params *pSimplex.Parameters) time.Duration {
	tick := min(int64(params.MaxProposalWait), int64(params.MaxRebroadcastWait)) / 10
	return time.Duration(tick)
}

// tick periodically advances the engine's time.
func (e *Engine) tick() {
	ticker := time.NewTicker(e.tickInterval)
	defer ticker.Stop()

	for {
		select {
		case tick := <-ticker.C:
			e.epoch.AdvanceTime(tick)
		case <-e.shutdown:
			ticker.Stop()
			return
		}
	}
}

func (e *Engine) SimplexMessage(ctx context.Context, nodeID ids.NodeID, msg *p2p.Simplex) error {
	if e.nonValidator {
		e.logger.Debug("non-validator received simplex message; dropping")
		return nil
	}

	simplexMsg, err := e.p2pToSimplexMessage(ctx, msg)
	if err != nil {
		e.logger.Debug("failed to convert p2p message to simplex message", zap.Error(err))
		return nil
	}

	return e.epoch.HandleMessage(simplexMsg, nodeID[:])
}

func (e *Engine) p2pToSimplexMessage(ctx context.Context, msg *p2p.Simplex) (*simplex.Message, error) {
	if msg == nil {
		return nil, errNilField
	}

	switch {
	case msg.GetBlockProposal() != nil:
		return blockProposalFromP2P(ctx, msg.GetBlockProposal(), e.blockDeserializer)
	case msg.GetEmptyNotarization() != nil:
		return emptyNotarizationMessageFromP2P(msg.GetEmptyNotarization(), e.quorumDeserializer)
	case msg.GetVote() != nil:
		return voteFromP2P(msg.GetVote())
	case msg.GetEmptyVote() != nil:
		return emptyVoteFromP2P(msg.GetEmptyVote())
	case msg.GetNotarization() != nil:
		return notarizationMessageFromP2P(msg.GetNotarization(), e.quorumDeserializer)
	case msg.GetFinalizeVote() != nil:
		return finalizeVoteFromP2P(msg.GetFinalizeVote())
	case msg.GetFinalization() != nil:
		return finalizationMessageFromP2P(msg.GetFinalization(), e.quorumDeserializer)
	case msg.GetReplicationRequest() != nil:
		return replicationRequestFromP2P(msg.GetReplicationRequest()), nil
	case msg.GetReplicationResponse() != nil:
		return replicationResponseFromP2P(ctx, msg.GetReplicationResponse(), e.blockDeserializer, e.quorumDeserializer)
	default:
		return nil, errUnknownMessageType
	}
}

func (e *Engine) Shutdown(_ context.Context) error {
	e.epoch.Stop()
	e.logger.Info("Stopped simplex engine")
	close(e.shutdown)
	return nil
}

func (*Engine) Gossip(_ context.Context) error {
	return nil
}

func (*Engine) Notify(_ context.Context, _ common.Message) error {
	return nil
}

func (e *Engine) HealthCheck(ctx context.Context) (interface{}, error) {
	if e.nonValidator {
		return "non-validator; no health status", nil
	}
	vmIntf, vmErr := e.vm.HealthCheck(ctx)
	intf := map[string]interface{}{
		"consensus": struct{}{},
		"vm":        vmIntf,
	}

	return intf, vmErr
}

var _ common.BootstrapableEngine = (*TODOBootstrapper)(nil)

type TODOBootstrapper struct {
	*Engine
	common.BootstrapTracker
}

func (t *TODOBootstrapper) Start(ctx context.Context, _ uint32) error {
	t.Engine.consensusCtx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_CHAIN,
		State: snow.Bootstrapping,
	})

	if err := t.Engine.vm.SetState(ctx, snow.Bootstrapping); err != nil {
		return fmt.Errorf("failed to notify VM that consensus is starting: %w",
			err)
	}

	// must notify the tracker we have finished bootstrapping
	t.BootstrapTracker.Bootstrapped(t.Engine.consensusCtx.ChainID)

	// Do not start the engine if we are a validator
	return t.Engine.Start(ctx, 0)
}

func (*TODOBootstrapper) Clear(_ context.Context) error {
	return nil
}

func (t *TODOBootstrapper) HealthCheck(ctx context.Context) (interface{}, error) {
	return t.Engine.HealthCheck(ctx)
}

func nonValidatingEngine(consensusCtx *snow.ConsensusContext, config *Config) (*Engine, error) {
	engine := &Engine{
		nonValidator:                true,
		logger:                      config.Log,
		StateSummaryFrontierHandler: common.NewNoOpStateSummaryFrontierHandler(config.Log),
		AcceptedStateSummaryHandler: common.NewNoOpAcceptedStateSummaryHandler(config.Log),
		AcceptedFrontierHandler:     common.NewNoOpAcceptedFrontierHandler(config.Log),
		AcceptedHandler:             common.NewNoOpAcceptedHandler(config.Log),
		AncestorsHandler:            common.NewNoOpAncestorsHandler(config.Log),
		PutHandler:                  common.NewNoOpPutHandler(config.Log),
		QueryHandler:                common.NewNoOpQueryHandler(config.Log),
		ChitsHandler:                common.NewNoOpChitsHandler(config.Log),
		Connector:                   config.VM,
		consensusCtx:                consensusCtx,
		AppHandler:                  common.NewNoOpAppHandler(config.Log),
		vm:                          config.VM,
	}

	return engine, nil
}

func isNonValidator(config *Config) bool {
	for _, node := range config.Params.InitialValidators {
		if node.NodeID == config.Ctx.NodeID {
			return false
		}
	}
	return true
}
