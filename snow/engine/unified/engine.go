// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package unified

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

var _ common.Engine = (*Engine)(nil)

type OnFinishedFunc func(ctx context.Context, lastReqID uint32) error

type Factory interface {
	ClearBootstrapDB() error

	HasStateSync() bool

	NewStateSyncer(OnFinishedFunc) (common.StateSyncer, error)

	NewSnowman() (common.ConsensusEngine, error)

	NewSnowBootstrapper(OnFinishedFunc) (common.BootstrapableEngine, error)

	NewAvalancheSyncer(OnFinishedFunc) (common.AvalancheBootstrapableEngine, error)

	NewAvalancheAncestorsGetter() common.GetAncestorsHandler

	AllGetServer() common.AllGetsServer
}

func EngineFromEngines(ctx *snow.ConsensusContext, engineFactory Factory, vm common.VM) (*Engine, error) {
	noopStateSyncer := &noopStateSyncer{
		StateSummaryFrontierHandler: common.NewNoOpStateSummaryFrontierHandler(ctx.Log),
		InternalHandler:             common.NewNoOpInternalHandler(ctx.Log),
		AcceptedStateSummaryHandler: common.NewNoOpAcceptedStateSummaryHandler(ctx.Log),
	}

	noopBootstrapper := &noopBootstrapper{
		AcceptedFrontierHandler: common.NewNoOpAcceptedFrontierHandler(ctx.Log),
		InternalHandler:         common.NewNoOpInternalHandler(ctx.Log),
		AncestorsHandler:        common.NewNoOpAncestorsHandler(ctx.Log),
		AcceptedHandler:         common.NewNoOpAcceptedHandler(ctx.Log),
	}

	noopSnowmanEngine := &noopSnowmanEngine{
		PutHandler:      common.NewNoOpPutHandler(ctx.Log),
		InternalHandler: common.NewNoOpInternalHandler(ctx.Log),
		ChitsHandler:    common.NewNoOpChitsHandler(ctx.Log),
		QueryHandler:    common.NewNoOpQueryHandler(ctx.Log),
	}

	return &Engine{
		vm:                       vm,
		noopSnowmanEngine:        noopSnowmanEngine,
		noopStateSyncer:          noopStateSyncer,
		noopBootstrapper:         noopBootstrapper,
		avalancheAncestorsGetter: engineFactory.NewAvalancheAncestorsGetter(),
		AllGetsServer:            engineFactory.AllGetServer(),
		ef:                       engineFactory,
		ctx:                      ctx,
		Log:                      ctx.Log,
	}, nil
}

type Engine struct {
	vm  common.VM
	ctx *snow.ConsensusContext
	Log logging.Logger
	common.AllGetsServer
	avalancheAncestorsGetter common.GetAncestorsHandler
	ef                       Factory
	stateSyncer              common.StateSyncer
	bootstrapper             common.BootstrapableEngine
	avalancheBootstrapper    common.AvalancheBootstrapableEngine
	snowman                  common.ConsensusEngine

	noopStateSyncer   common.StateSyncer
	noopBootstrapper  common.BootstrapableEngine
	noopSnowmanEngine common.ConsensusEngine
}

type noopStateSyncer struct {
	noOpEngine
	common.StateSummaryFrontierHandler
	common.AcceptedStateSummaryHandler
	common.InternalHandler
}

type noOpEngine struct{}

func (noOpEngine) Start(_ context.Context, _ uint32) error {
	return nil
}

func (noOpEngine) HealthCheck(_ context.Context) (interface{}, error) {
	return nil, nil
}

func (noOpEngine) IsEnabled(_ context.Context) (bool, error) {
	return false, nil
}

type noopBootstrapper struct {
	noOpEngine
	common.AcceptedFrontierHandler
	common.AcceptedHandler
	common.AncestorsHandler
	common.InternalHandler
}

func (noopBootstrapper) Clear(context.Context) error {
	return nil
}

type noopSnowmanEngine struct {
	noOpEngine
	common.PutHandler
	common.QueryHandler
	common.ChitsHandler
	common.InternalHandler
}

func (e *Engine) GetAncestors(
	ctx context.Context,
	nodeID ids.NodeID,
	requestID uint32,
	containerID ids.ID,
	engineType p2p.EngineType,
) error {
	switch engineType {
	case p2p.EngineType_ENGINE_TYPE_AVALANCHE:
		return e.avalancheAncestorsGetter.GetAncestors(ctx, nodeID, requestID, containerID, engineType)
	default:
		return e.AllGetsServer.GetAncestors(ctx, nodeID, requestID, containerID, engineType)
	}
}

func (e *Engine) StateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, summary []byte) error {
	return e.stateSyncer.StateSummaryFrontier(ctx, nodeID, requestID, summary)
}

func (e *Engine) GetStateSummaryFrontierFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return e.stateSyncer.GetStateSummaryFrontierFailed(ctx, nodeID, requestID)
}

func (e *Engine) AcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, summaryIDs set.Set[ids.ID]) error {
	return e.stateSyncer.AcceptedStateSummary(ctx, nodeID, requestID, summaryIDs)
}

func (e *Engine) GetAcceptedStateSummaryFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return e.stateSyncer.GetAcceptedStateSummaryFailed(ctx, nodeID, requestID)
}

func (e *Engine) AcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error {
	if e.ctx.State.Get().Type == p2p.EngineType_ENGINE_TYPE_AVALANCHE {
		return e.noopBootstrapper.AcceptedFrontier(ctx, nodeID, requestID, containerID)
	}
	return e.bootstrapper.AcceptedFrontier(ctx, nodeID, requestID, containerID)
}

func (e *Engine) GetAcceptedFrontierFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	if e.ctx.State.Get().Type == p2p.EngineType_ENGINE_TYPE_AVALANCHE {
		return e.noopBootstrapper.GetAcceptedFrontierFailed(ctx, nodeID, requestID)
	}
	return e.bootstrapper.GetAcceptedFrontierFailed(ctx, nodeID, requestID)
}

func (e *Engine) Accepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs set.Set[ids.ID]) error {
	if e.ctx.State.Get().Type == p2p.EngineType_ENGINE_TYPE_AVALANCHE {
		return e.noopBootstrapper.Accepted(ctx, nodeID, requestID, containerIDs)
	}
	return e.bootstrapper.Accepted(ctx, nodeID, requestID, containerIDs)
}

func (e *Engine) GetAcceptedFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	if e.ctx.State.Get().Type == p2p.EngineType_ENGINE_TYPE_AVALANCHE {
		return e.noopBootstrapper.GetAcceptedFailed(ctx, nodeID, requestID)
	}
	return e.bootstrapper.GetAcceptedFailed(ctx, nodeID, requestID)
}

func (e *Engine) Ancestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containers [][]byte) error {
	if e.ctx.State.Get().Type == p2p.EngineType_ENGINE_TYPE_AVALANCHE {
		return e.avalancheBootstrapper.Ancestors(ctx, nodeID, requestID, containers)
	}
	return e.bootstrapper.Ancestors(ctx, nodeID, requestID, containers)
}

func (e *Engine) GetAncestorsFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	if e.ctx.State.Get().Type == p2p.EngineType_ENGINE_TYPE_AVALANCHE {
		return e.avalancheBootstrapper.GetAncestorsFailed(ctx, nodeID, requestID)
	}
	return e.bootstrapper.GetAncestorsFailed(ctx, nodeID, requestID)
}

func (e *Engine) Put(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte) error {
	return e.snowman.Put(ctx, nodeID, requestID, container)
}

func (e *Engine) GetFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return e.snowman.GetFailed(ctx, nodeID, requestID)
}

func (e *Engine) PullQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID, requestedHeight uint64) error {
	return e.snowman.PullQuery(ctx, nodeID, requestID, containerID, requestedHeight)
}

func (e *Engine) PushQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte, requestedHeight uint64) error {
	return e.snowman.PushQuery(ctx, nodeID, requestID, container, requestedHeight)
}

func (e *Engine) Chits(ctx context.Context, nodeID ids.NodeID, requestID uint32, preferredID ids.ID, preferredIDAtHeight ids.ID, acceptedID ids.ID, acceptedHeight uint64) error {
	return e.snowman.Chits(ctx, nodeID, requestID, preferredID, preferredIDAtHeight, acceptedID, acceptedHeight)
}

func (e *Engine) QueryFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return e.snowman.QueryFailed(ctx, nodeID, requestID)
}

func (e *Engine) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	return e.vm.AppRequest(ctx, nodeID, requestID, deadline, request)
}

func (e *Engine) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	return e.vm.AppResponse(ctx, nodeID, requestID, response)
}

func (e *Engine) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *common.AppError) error {
	return e.vm.AppRequestFailed(ctx, nodeID, requestID, appErr)
}

func (e *Engine) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	return e.vm.AppGossip(ctx, nodeID, msg)
}

func (e *Engine) Timeout(ctx context.Context) error {
	return e.bootstrapper.Timeout(ctx)
}

func (e *Engine) Gossip(ctx context.Context) error {
	return e.snowman.Gossip(ctx)
}

func (e *Engine) Shutdown(ctx context.Context) error {
	var err1, err2, err3, err4 error
	if e.stateSyncer != nil {
		err1 = e.stateSyncer.Shutdown(ctx)
	}
	if e.bootstrapper != nil {
		err2 = e.bootstrapper.Shutdown(ctx)
	}
	if e.snowman != nil {
		err3 = e.snowman.Shutdown(ctx)
	}
	if e.avalancheBootstrapper != nil {
		err4 = e.avalancheBootstrapper.Shutdown(ctx)
	}
	return errors.Join(err1, err2, err3, err4)
}

func (e *Engine) HealthCheck(ctx context.Context) (interface{}, error) {
	if e.ctx.State.Get().Type == p2p.EngineType_ENGINE_TYPE_AVALANCHE {
		return e.avalancheBootstrapper.HealthCheck(ctx)
	}

	state := e.ctx.State.Get()

	switch state.State {
	case snow.StateSyncing:
		return e.stateSyncer.HealthCheck(ctx)
	case snow.NormalOp:
		return e.snowman.HealthCheck(ctx)
	case snow.Bootstrapping:
		return e.bootstrapper.HealthCheck(ctx)
	default:
		return nil, errors.New("initializing")
	}
}

func (e *Engine) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	if e.ctx.State.Get().Type == p2p.EngineType_ENGINE_TYPE_AVALANCHE {
		return e.avalancheBootstrapper.Connected(ctx, nodeID, nodeVersion)
	}
	state := e.ctx.State.Get().State
	switch state {
	case snow.NormalOp:
		return e.snowman.Connected(ctx, nodeID, nodeVersion)
	case snow.Bootstrapping:
		return e.bootstrapper.Connected(ctx, nodeID, nodeVersion)
	case snow.StateSyncing:
		return e.stateSyncer.Connected(ctx, nodeID, nodeVersion)
	default:
		return e.noopSnowmanEngine.Connected(ctx, nodeID, nodeVersion)
	}
}

func (e *Engine) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	if e.ctx.State.Get().Type == p2p.EngineType_ENGINE_TYPE_AVALANCHE {
		return e.avalancheBootstrapper.Disconnected(ctx, nodeID)
	}
	state := e.ctx.State.Get().State
	switch state {
	case snow.NormalOp:
		return e.snowman.Disconnected(ctx, nodeID)
	case snow.Bootstrapping:
		return e.bootstrapper.Disconnected(ctx, nodeID)
	case snow.StateSyncing:
		return e.stateSyncer.Disconnected(ctx, nodeID)
	default:
		return e.noopSnowmanEngine.Disconnected(ctx, nodeID)
	}
}

func (e *Engine) Notify(ctx context.Context, message common.Message) error {
	if e.ctx.State.Get().Type == p2p.EngineType_ENGINE_TYPE_AVALANCHE {
		return e.avalancheBootstrapper.Notify(ctx, message)
	}
	state := e.ctx.State.Get().State
	switch state {
	case snow.NormalOp:
		return e.snowman.Notify(ctx, message)
	case snow.Bootstrapping:
		return e.bootstrapper.Notify(ctx, message)
	case snow.StateSyncing:
		return e.stateSyncer.Notify(ctx, message)
	default:
		return e.noopSnowmanEngine.Notify(ctx, message)
	}
}

func (e *Engine) Start(ctx context.Context, startReqID uint32) error {
	e.Log.Info("Starting state sync engine")

	state := e.setNoOpEngines()

	switch state.Type {
	case p2p.EngineType_ENGINE_TYPE_AVALANCHE:
		return e.startAvalancheBootstrapper(ctx, startReqID)
	default:
		switch state.State {
		case snow.StateSyncing:
			return e.startStateSyncer(ctx, startReqID)
		case snow.NormalOp:
			return e.startSnowman(ctx, startReqID)
		case snow.Bootstrapping:
			return e.startSnowBootstrapper(ctx, startReqID)
		default:
			return fmt.Errorf("invalid state: %s", state.State)
		}
	}
}

func (e *Engine) setNoOpEngines() snow.EngineState {
	state := e.ctx.State.Get()

	switch state.Type {
	case p2p.EngineType_ENGINE_TYPE_AVALANCHE:
		e.stateSyncer = e.noopStateSyncer
		e.snowman = e.noopSnowmanEngine
		e.bootstrapper = e.noopBootstrapper
	default:
		switch state.State {
		case snow.StateSyncing:
			e.bootstrapper = e.noopBootstrapper
			e.snowman = e.noopSnowmanEngine
			e.avalancheBootstrapper = e.noopBootstrapper
		case snow.NormalOp:
			e.bootstrapper = e.noopBootstrapper
			e.stateSyncer = e.noopStateSyncer
			e.avalancheBootstrapper = e.noopBootstrapper
		case snow.Bootstrapping:
			e.snowman = e.noopSnowmanEngine
			e.stateSyncer = e.noopStateSyncer
			e.avalancheBootstrapper = e.noopBootstrapper
		}
	}
	return state
}

func (e *Engine) startAvalancheBootstrapper(ctx context.Context, startReqID uint32) error {
	e.ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_AVALANCHE,
		State: snow.Bootstrapping,
	})
	e.setNoOpEngines()

	syncer, err := e.ef.NewAvalancheSyncer(e.startSnowBootstrapper)
	if err != nil {
		return err
	}
	e.avalancheBootstrapper = syncer
	return e.avalancheBootstrapper.Start(ctx, startReqID)
}

func (e *Engine) startStateSyncer(ctx context.Context, startReqID uint32) error {
	e.ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.StateSyncing,
	})
	e.setNoOpEngines()

	syncer, err := e.ef.NewStateSyncer(e.startSnowBootstrapper)
	if err != nil {
		return err
	}
	e.stateSyncer = syncer

	if err := e.ef.ClearBootstrapDB(); err != nil {
		return err
	}

	return e.stateSyncer.Start(ctx, startReqID)
}

func (e *Engine) startSnowBootstrapper(ctx context.Context, lastReqID uint32) error {
	e.ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.Bootstrapping,
	})
	e.setNoOpEngines()

	bs, err := e.ef.NewSnowBootstrapper(e.startSnowman)
	if err != nil {
		return err
	}
	e.bootstrapper = bs
	return e.bootstrapper.Start(ctx, lastReqID)
}

func (e *Engine) startSnowman(ctx context.Context, lastReqID uint32) error {
	e.ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.NormalOp,
	})
	e.setNoOpEngines()

	snowman, err := e.ef.NewSnowman()
	if err != nil {
		return err
	}
	e.snowman = snowman
	return e.snowman.Start(ctx, lastReqID)
}
