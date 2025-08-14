// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
	commontracker "github.com/ava-labs/avalanchego/snow/engine/common/tracker"
)

const (
	numDispatchersToClose = 3
	// If a consensus message takes longer than this to process, the handler
	// will log a warning.
	syncProcessingTimeWarnLimit = 30 * time.Second
)

var (
	_ Handler = (*handler)(nil)

	errMissingEngine  = errors.New("missing engine")
	errNoStartingGear = errors.New("failed to select starting gear")
	errorShuttingDown = errors.New("shutting down")
)

type Handler interface {
	health.Checker

	Context() *snow.ConsensusContext
	// ShouldHandle returns true if the node with the given ID is allowed to send
	// messages to this chain. If the node is not allowed to send messages to
	// this chain, the message should be dropped.
	ShouldHandle(nodeID ids.NodeID) bool

	SetEngineManager(engineManager *EngineManager)
	GetEngineManager() *EngineManager

	SetOnStopped(onStopped func())
	Start(ctx context.Context, recoverPanic bool)
	Push(ctx context.Context, msg Message)
	Len() int

	Stop(ctx context.Context)
	StopWithError(ctx context.Context, err error)
	// AwaitStopped returns an error if the call would block and [ctx] is done.
	// Even if [ctx] is done when passed into this function, this function will
	// return a nil error if it will not block.
	AwaitStopped(ctx context.Context) (time.Duration, error)
}

type simplexEngine interface {
	SimplexMessage(ctx context.Context, nodeID ids.NodeID, msg *p2ppb.Simplex) error
}

// handler passes incoming messages from the network to the consensus engine.
// (Actually, it receives the incoming messages from a ChainRouter, but same difference.)
type handler struct {
	haltBootstrapping func()

	metrics *metrics

	nf           *common.NotificationForwarder
	subscription common.Subscription
	cn           *block.ChangeNotifier

	// Useful for faking time in tests
	clock mockable.Clock

	ctx *snow.ConsensusContext
	// TODO: consider using peerTracker instead of validators
	// since peerTracker is already tracking validators
	validators validators.Manager
	// Receives messages from the VM
	msgFromVMChan   chan common.Message
	gossipFrequency time.Duration

	engineManager *EngineManager

	// onStopped is called in a goroutine when this handler finishes shutting
	// down. If it is nil then it is skipped.
	onStopped func()

	// Tracks cpu/disk usage caused by each peer.
	resourceTracker tracker.ResourceTracker

	// Holds messages that [engine] hasn't processed yet.
	// [unprocessedMsgsCond.L] must be held while accessing [syncMessageQueue].
	syncMessageQueue MessageQueue
	// Holds messages that [engine] hasn't processed yet.
	// [unprocessedAsyncMsgsCond.L] must be held while accessing [asyncMessageQueue].
	asyncMessageQueue MessageQueue
	// Worker pool for handling asynchronous consensus messages
	asyncMessagePool errgroup.Group

	closeOnce            sync.Once
	startClosingTime     time.Time
	totalClosingTime     time.Duration
	closingChan          chan struct{}
	numDispatchersClosed atomic.Uint32
	// Closed when this handler and [engine] are done shutting down
	closed chan struct{}

	subnet subnets.Subnet

	// Tracks the peers that are currently connected to this subnet
	peerTracker commontracker.Peers
	p2pTracker  *p2p.PeerTracker
}

// Initialize this consensus handler
// [engine] must be initialized before initializing this handler
func New(
	ctx *snow.ConsensusContext,
	cn *block.ChangeNotifier,
	subscription common.Subscription,
	validators validators.Manager,
	gossipFrequency time.Duration,
	threadPoolSize int,
	resourceTracker tracker.ResourceTracker,
	subnet subnets.Subnet,
	peerTracker commontracker.Peers,
	p2pTracker *p2p.PeerTracker,
	reg prometheus.Registerer,
	haltBootstrapping func(),
) (Handler, error) {
	h := &handler{
		subscription:      subscription,
		cn:                cn,
		msgFromVMChan:     make(chan common.Message),
		haltBootstrapping: haltBootstrapping,
		ctx:               ctx,
		validators:        validators,
		gossipFrequency:   gossipFrequency,
		closingChan:       make(chan struct{}),
		closed:            make(chan struct{}),
		resourceTracker:   resourceTracker,
		subnet:            subnet,
		peerTracker:       peerTracker,
		p2pTracker:        p2pTracker,
	}
	h.asyncMessagePool.SetLimit(threadPoolSize)

	var err error

	h.metrics, err = newMetrics(reg)
	if err != nil {
		return nil, fmt.Errorf("initializing handler metrics errored with: %w", err)
	}
	cpuTracker := resourceTracker.CPUTracker()
	h.syncMessageQueue, err = NewMessageQueue(
		h.ctx.Log,
		h.ctx.SubnetID,
		h.validators,
		cpuTracker,
		"sync",
		reg,
	)
	if err != nil {
		return nil, fmt.Errorf("initializing sync message queue errored with: %w", err)
	}
	h.asyncMessageQueue, err = NewMessageQueue(
		h.ctx.Log,
		h.ctx.SubnetID,
		h.validators,
		cpuTracker,
		"async",
		reg,
	)
	if err != nil {
		return nil, fmt.Errorf("initializing async message queue errored with: %w", err)
	}
	return h, nil
}

func (h *handler) Context() *snow.ConsensusContext {
	return h.ctx
}

func (h *handler) ShouldHandle(nodeID ids.NodeID) bool {
	_, ok := h.validators.GetValidator(h.ctx.SubnetID, nodeID)
	return h.subnet.IsAllowed(nodeID, ok)
}

func (h *handler) SetEngineManager(engineManager *EngineManager) {
	h.engineManager = engineManager
}

func (h *handler) GetEngineManager() *EngineManager {
	return h.engineManager
}

func (h *handler) SetOnStopped(onStopped func()) {
	h.onStopped = onStopped
}

func (h *handler) selectStartingGear(ctx context.Context) (common.Engine, error) {
	state := h.ctx.State.Get()
	engines := h.engineManager.Get(state.Type)
	if engines == nil {
		return nil, errNoStartingGear
	}
	if engines.StateSyncer == nil {
		return engines.Bootstrapper, nil
	}

	stateSyncEnabled, err := engines.StateSyncer.IsEnabled(ctx)
	if err != nil {
		return nil, err
	}

	if !stateSyncEnabled {
		return engines.Bootstrapper, nil
	}

	// drop bootstrap state from previous runs before starting state sync
	return engines.StateSyncer, engines.Bootstrapper.Clear(ctx)
}

func (h *handler) Start(ctx context.Context, recoverPanic bool) {
	gear, err := h.selectStartingGear(ctx)
	if err != nil {
		h.ctx.Log.Error("chain failed to select starting gear",
			zap.Error(err),
		)
		h.shutdown(ctx, h.clock.Time())
		return
	}

	h.nf = common.NewNotificationForwarder(h, h.subscription, h.ctx.Log)
	h.cn.OnChange = h.nf.CheckForEvent

	h.ctx.Lock.Lock()
	err = gear.Start(ctx, 0)
	h.ctx.Lock.Unlock()
	if err != nil {
		h.ctx.Log.Error("chain failed to start",
			zap.Error(err),
		)
		h.shutdown(ctx, h.clock.Time())
		return
	}

	detachedCtx := context.WithoutCancel(ctx)
	dispatchSync := func() {
		h.dispatchSync(detachedCtx)
	}
	dispatchAsync := func() {
		h.dispatchAsync(detachedCtx)
	}
	dispatchChans := func() {
		h.dispatchChans(detachedCtx)
	}
	if recoverPanic {
		go h.ctx.Log.RecoverAndExit(dispatchSync, func() {
			h.ctx.Log.Error("chain was shutdown due to a panic in the sync dispatcher")
		})
		go h.ctx.Log.RecoverAndExit(dispatchAsync, func() {
			h.ctx.Log.Error("chain was shutdown due to a panic in the async dispatcher")
		})
		go h.ctx.Log.RecoverAndExit(dispatchChans, func() {
			h.ctx.Log.Error("chain was shutdown due to a panic in the chan dispatcher")
		})
	} else {
		go h.ctx.Log.RecoverAndPanic(dispatchSync)
		go h.ctx.Log.RecoverAndPanic(dispatchAsync)
		go h.ctx.Log.RecoverAndPanic(dispatchChans)
	}
}

func (h *handler) Notify(ctx context.Context, msg common.Message) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-h.closed:
		return errorShuttingDown
	case h.msgFromVMChan <- msg:
		return nil
	}
}

// Push the message onto the handler's queue
func (h *handler) Push(ctx context.Context, msg Message) {
	switch msg.Op() {
	case message.AppRequestOp, message.AppErrorOp, message.AppResponseOp, message.AppGossipOp:
		h.asyncMessageQueue.Push(ctx, msg)
	default:
		h.syncMessageQueue.Push(ctx, msg)
	}
}

func (h *handler) Len() int {
	return h.syncMessageQueue.Len() + h.asyncMessageQueue.Len()
}

// Note: It is possible for Stop to be called before/concurrently with Start.
//
// Invariant: Stop must never block.
func (h *handler) Stop(_ context.Context) {
	h.closeOnce.Do(func() {
		h.startClosingTime = h.clock.Time()

		// Must hold the locks here to ensure there's no race condition in where
		// we check the value of [h.closing] after the call to [Signal].
		h.syncMessageQueue.Shutdown()
		h.asyncMessageQueue.Shutdown()
		close(h.closingChan)
		h.haltBootstrapping()
	})
}

func (h *handler) StopWithError(ctx context.Context, err error) {
	h.ctx.Log.Fatal("shutting down chain",
		zap.String("reason", "received an unexpected error"),
		zap.Error(err),
	)
	h.Stop(ctx)
}

func (h *handler) AwaitStopped(ctx context.Context) (time.Duration, error) {
	select {
	case <-h.closed:
		return h.totalClosingTime, nil
	default:
	}

	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-h.closed:
		return h.totalClosingTime, nil
	}
}

func (h *handler) dispatchSync(ctx context.Context) {
	defer h.closeDispatcher(ctx)

	// Handle sync messages from the router
	for {
		// Get the next message we should process. If the handler is shutting
		// down, we may fail to pop a message.
		ctx, msg, ok := h.popUnexpiredMsg(h.syncMessageQueue)
		if !ok {
			return
		}

		// If there is an error handling the message, shut down the chain
		if err := h.handleSyncMsg(ctx, msg); err != nil {
			h.StopWithError(ctx, fmt.Errorf(
				"%w while processing sync message: %s from %s",
				err,
				msg.Op(),
				msg.NodeID(),
			))
			return
		}
	}
}

func (h *handler) dispatchAsync(ctx context.Context) {
	defer func() {
		// We never return an error in any of our functions, so it is safe to
		// drop any error here.
		_ = h.asyncMessagePool.Wait()
		h.closeDispatcher(ctx)
	}()

	// Handle async messages from the router
	for {
		// Get the next message we should process. If the handler is shutting
		// down, we may fail to pop a message.
		ctx, msg, ok := h.popUnexpiredMsg(h.asyncMessageQueue)
		if !ok {
			return
		}

		h.handleAsyncMsg(ctx, msg)
	}
}

func (h *handler) dispatchChans(ctx context.Context) {
	gossiper := time.NewTicker(h.gossipFrequency)
	defer func() {
		gossiper.Stop()
		h.closeDispatcher(ctx)
	}()

	// Handle messages generated by the handler and the VM
	for {
		var msg message.InboundMessage
		select {
		case <-h.closingChan:
			return

		case vmMSG := <-h.msgFromVMChan:
			msg = message.InternalVMMessage(h.ctx.NodeID, uint32(vmMSG))

		case <-gossiper.C:
			msg = message.InternalGossipRequest(h.ctx.NodeID)
		}

		if err := h.handleChanMsg(msg); err != nil {
			h.StopWithError(ctx, fmt.Errorf(
				"%w while processing chan message: %s",
				err,
				msg.Op(),
			))
			return
		}
	}
}

// Any returned error is treated as fatal
func (h *handler) handleSyncMsg(ctx context.Context, msg Message) error {
	var (
		nodeID    = msg.NodeID()
		op        = msg.Op().String()
		body      = msg.Message()
		startTime = h.clock.Time()
		// Check if the chain is in normal operation at the start of message
		// execution (may change during execution)
		isNormalOp = h.ctx.State.Get().State == snow.NormalOp
	)
	if h.ctx.Log.Enabled(logging.Verbo) {
		h.ctx.Log.Verbo("forwarding sync message to consensus",
			zap.Stringer("nodeID", nodeID),
			zap.String("messageOp", op),
			zap.Stringer("message", body),
		)
	} else {
		h.ctx.Log.Debug("forwarding sync message to consensus",
			zap.Stringer("nodeID", nodeID),
			zap.String("messageOp", op),
		)
	}
	h.resourceTracker.StartProcessing(nodeID, startTime)
	h.ctx.Lock.Lock()
	lockAcquiredTime := h.clock.Time()
	defer func() {
		h.ctx.Lock.Unlock()

		var (
			endTime      = h.clock.Time()
			lockingTime  = lockAcquiredTime.Sub(startTime)
			handlingTime = endTime.Sub(lockAcquiredTime)
		)
		h.resourceTracker.StopProcessing(nodeID, endTime)
		h.metrics.lockingTime.Add(float64(lockingTime))
		labels := prometheus.Labels{
			opLabel: op,
		}
		h.metrics.messages.With(labels).Inc()
		h.metrics.messageHandlingTime.With(labels).Add(float64(handlingTime))

		msg.OnFinishedHandling()
		h.ctx.Log.Debug("finished handling sync message",
			zap.String("messageOp", op),
		)
		if lockingTime+handlingTime > syncProcessingTimeWarnLimit && isNormalOp {
			h.ctx.Log.Warn("handling sync message took longer than expected",
				zap.Duration("lockingTime", lockingTime),
				zap.Duration("handlingTime", handlingTime),
				zap.Stringer("nodeID", nodeID),
				zap.String("messageOp", op),
				zap.Stringer("message", body),
			)
		}
	}()

	// We will attempt to pass the message to the requested type for the state
	// we are currently in.
	currentState := h.ctx.State.Get()
	if msg.EngineType == p2ppb.EngineType_ENGINE_TYPE_SNOWMAN &&
		currentState.Type == p2ppb.EngineType_ENGINE_TYPE_AVALANCHE {
		// The peer is requesting an engine type that hasn't been initialized
		// yet. This means we know that this isn't a response, so we can safely
		// drop the message.
		h.ctx.Log.Debug("dropping sync message",
			zap.String("reason", "uninitialized engine type"),
			zap.String("messageOp", op),
			zap.Stringer("currentEngineType", currentState.Type),
			zap.Stringer("requestedEngineType", msg.EngineType),
		)
		return nil
	}

	var engineType p2ppb.EngineType
	switch msg.EngineType {
	case p2ppb.EngineType_ENGINE_TYPE_AVALANCHE, p2ppb.EngineType_ENGINE_TYPE_SNOWMAN:
		// The peer is requesting an engine type that has been initialized, so
		// we should attempt to honor the request.
		engineType = msg.EngineType
	default:
		// Note: [msg.EngineType] may have been provided by the peer as an
		// invalid option. I.E. not one of AVALANCHE, SNOWMAN, or UNSPECIFIED.
		// In this case, we treat the value the same way as UNSPECIFIED.
		//
		// If the peer didn't request a specific engine type, we default to the
		// current engine.
		engineType = currentState.Type
	}

	engine, ok := h.engineManager.Get(engineType).Get(currentState.State)
	if !ok {
		// This should only happen if the peer is not following the protocol.
		// This can happen if the chain only has a Snowman engine and the peer
		// requested an Avalanche engine handle the message.
		h.ctx.Log.Debug("dropping sync message",
			zap.String("reason", "uninitialized engine state"),
			zap.String("messageOp", op),
			zap.Stringer("currentEngineType", currentState.Type),
			zap.Stringer("requestedEngineType", msg.EngineType),
			zap.Stringer("engineState", currentState.State),
		)
		return nil
	}

	// Invariant: Response messages can never be dropped here. This is because
	//            the timeout has already been cleared. This means the engine
	//            should be invoked with a failure message if parsing of the
	//            response fails.
	switch msg := body.(type) {
	// State messages should always be sent to the snowman engine
	case *p2ppb.GetStateSummaryFrontier:
		return engine.GetStateSummaryFrontier(ctx, nodeID, msg.RequestId)

	case *p2ppb.StateSummaryFrontier:
		return engine.StateSummaryFrontier(ctx, nodeID, msg.RequestId, msg.Summary)

	case *message.GetStateSummaryFrontierFailed:
		return engine.GetStateSummaryFrontierFailed(ctx, nodeID, msg.RequestID)

	case *p2ppb.GetAcceptedStateSummary:
		return engine.GetAcceptedStateSummary(
			ctx,
			nodeID,
			msg.RequestId,
			set.Of(msg.Heights...),
		)

	case *p2ppb.AcceptedStateSummary:
		summaryIDs, err := getIDs(msg.SummaryIds)
		if err != nil {
			h.ctx.Log.Debug("message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", message.AcceptedStateSummaryOp),
				zap.Uint32("requestID", msg.RequestId),
				zap.String("field", "SummaryIDs"),
				zap.Error(err),
			)
			return engine.GetAcceptedStateSummaryFailed(ctx, nodeID, msg.RequestId)
		}

		return engine.AcceptedStateSummary(ctx, nodeID, msg.RequestId, summaryIDs)

	case *message.GetAcceptedStateSummaryFailed:
		return engine.GetAcceptedStateSummaryFailed(ctx, nodeID, msg.RequestID)

	// Bootstrapping messages may be forwarded to either avalanche or snowman
	// engines, depending on the EngineType field
	case *p2ppb.GetAcceptedFrontier:
		return engine.GetAcceptedFrontier(ctx, nodeID, msg.RequestId)

	case *p2ppb.AcceptedFrontier:
		containerID, err := ids.ToID(msg.ContainerId)
		if err != nil {
			h.ctx.Log.Debug("message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", message.AcceptedFrontierOp),
				zap.Uint32("requestID", msg.RequestId),
				zap.String("field", "ContainerID"),
				zap.Error(err),
			)
			return engine.GetAcceptedFrontierFailed(ctx, nodeID, msg.RequestId)
		}

		return engine.AcceptedFrontier(ctx, nodeID, msg.RequestId, containerID)

	case *message.GetAcceptedFrontierFailed:
		return engine.GetAcceptedFrontierFailed(ctx, nodeID, msg.RequestID)

	case *p2ppb.GetAccepted:
		containerIDs, err := getIDs(msg.ContainerIds)
		if err != nil {
			h.ctx.Log.Debug("message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", message.GetAcceptedOp),
				zap.Uint32("requestID", msg.RequestId),
				zap.String("field", "ContainerIDs"),
				zap.Error(err),
			)
			return nil
		}

		return engine.GetAccepted(ctx, nodeID, msg.RequestId, containerIDs)

	case *p2ppb.Accepted:
		containerIDs, err := getIDs(msg.ContainerIds)
		if err != nil {
			h.ctx.Log.Debug("message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", message.AcceptedOp),
				zap.Uint32("requestID", msg.RequestId),
				zap.String("field", "ContainerIDs"),
				zap.Error(err),
			)
			return engine.GetAcceptedFailed(ctx, nodeID, msg.RequestId)
		}

		return engine.Accepted(ctx, nodeID, msg.RequestId, containerIDs)

	case *message.GetAcceptedFailed:
		return engine.GetAcceptedFailed(ctx, nodeID, msg.RequestID)

	case *p2ppb.GetAncestors:
		containerID, err := ids.ToID(msg.ContainerId)
		if err != nil {
			h.ctx.Log.Debug("dropping message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", message.GetAncestorsOp),
				zap.Uint32("requestID", msg.RequestId),
				zap.String("field", "ContainerID"),
				zap.Error(err),
			)
			return nil
		}

		return engine.GetAncestors(ctx, nodeID, msg.RequestId, containerID)

	case *message.GetAncestorsFailed:
		return engine.GetAncestorsFailed(ctx, nodeID, msg.RequestID)

	case *p2ppb.Ancestors:
		return engine.Ancestors(ctx, nodeID, msg.RequestId, msg.Containers)

	case *p2ppb.Get:
		containerID, err := ids.ToID(msg.ContainerId)
		if err != nil {
			h.ctx.Log.Debug("dropping message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", message.GetOp),
				zap.Uint32("requestID", msg.RequestId),
				zap.String("field", "ContainerID"),
				zap.Error(err),
			)
			return nil
		}

		return engine.Get(ctx, nodeID, msg.RequestId, containerID)

	case *message.GetFailed:
		return engine.GetFailed(ctx, nodeID, msg.RequestID)

	case *p2ppb.Put:
		return engine.Put(ctx, nodeID, msg.RequestId, msg.Container)

	case *p2ppb.PushQuery:
		return engine.PushQuery(ctx, nodeID, msg.RequestId, msg.Container, msg.RequestedHeight)

	case *p2ppb.PullQuery:
		containerID, err := ids.ToID(msg.ContainerId)
		if err != nil {
			h.ctx.Log.Debug("dropping message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", message.PullQueryOp),
				zap.Uint32("requestID", msg.RequestId),
				zap.String("field", "ContainerID"),
				zap.Error(err),
			)
			return nil
		}

		return engine.PullQuery(ctx, nodeID, msg.RequestId, containerID, msg.RequestedHeight)

	case *p2ppb.Chits:
		preferredID, err := ids.ToID(msg.PreferredId)
		if err != nil {
			h.ctx.Log.Debug("message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", message.ChitsOp),
				zap.Uint32("requestID", msg.RequestId),
				zap.String("field", "PreferredID"),
				zap.Error(err),
			)
			return engine.QueryFailed(ctx, nodeID, msg.RequestId)
		}

		preferredIDAtHeight, err := ids.ToID(msg.PreferredIdAtHeight)
		if err != nil {
			h.ctx.Log.Debug("message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", message.ChitsOp),
				zap.Uint32("requestID", msg.RequestId),
				zap.String("field", "PreferredIDAtHeight"),
				zap.Error(err),
			)
			return engine.QueryFailed(ctx, nodeID, msg.RequestId)
		}

		acceptedID, err := ids.ToID(msg.AcceptedId)
		if err != nil {
			h.ctx.Log.Debug("message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", message.ChitsOp),
				zap.Uint32("requestID", msg.RequestId),
				zap.String("field", "AcceptedID"),
				zap.Error(err),
			)
			return engine.QueryFailed(ctx, nodeID, msg.RequestId)
		}

		return engine.Chits(ctx, nodeID, msg.RequestId, preferredID, preferredIDAtHeight, acceptedID, msg.AcceptedHeight)

	case *message.QueryFailed:
		return engine.QueryFailed(ctx, nodeID, msg.RequestID)

	case *p2ppb.Simplex:
		h.ctx.Log.Debug("received simplex message",
			zap.Stringer("nodeID", nodeID),
			zap.String("messageOp", op),
			zap.Stringer("message", body),
		)
		s, ok := engine.(simplexEngine)
		if !ok {
			return fmt.Errorf(
				"attempt to submit simplex message %s from %s to non-simplex engine %s",
				op, nodeID,
				engineType,
			)
		}

		return s.SimplexMessage(ctx, nodeID, msg)
	case *message.Connected:
		err := h.peerTracker.Connected(ctx, nodeID, msg.NodeVersion)
		if err != nil {
			return err
		}
		h.p2pTracker.Connected(nodeID, msg.NodeVersion)
		return engine.Connected(ctx, nodeID, msg.NodeVersion)

	case *message.Disconnected:
		err := h.peerTracker.Disconnected(ctx, nodeID)
		if err != nil {
			return err
		}
		h.p2pTracker.Disconnected(nodeID)
		return engine.Disconnected(ctx, nodeID)
	default:
		return fmt.Errorf(
			"attempt to submit unhandled sync msg %s from %s",
			op, nodeID,
		)
	}
}

func (h *handler) handleAsyncMsg(ctx context.Context, msg Message) {
	h.asyncMessagePool.Go(func() error {
		if err := h.executeAsyncMsg(ctx, msg); err != nil {
			h.StopWithError(ctx, fmt.Errorf(
				"%w while processing async message: %s from %s",
				err,
				msg.Op(),
				msg.NodeID(),
			))
		}
		return nil
	})
}

// Any returned error is treated as fatal
func (h *handler) executeAsyncMsg(ctx context.Context, msg Message) error {
	var (
		nodeID    = msg.NodeID()
		op        = msg.Op().String()
		body      = msg.Message()
		startTime = h.clock.Time()
	)
	if h.ctx.Log.Enabled(logging.Verbo) {
		h.ctx.Log.Verbo("forwarding async message to consensus",
			zap.Stringer("nodeID", nodeID),
			zap.String("messageOp", op),
			zap.Stringer("message", body),
		)
	} else {
		h.ctx.Log.Debug("forwarding async message to consensus",
			zap.Stringer("nodeID", nodeID),
			zap.String("messageOp", op),
		)
	}
	h.resourceTracker.StartProcessing(nodeID, startTime)
	defer func() {
		var (
			endTime      = h.clock.Time()
			handlingTime = endTime.Sub(startTime)
		)
		h.resourceTracker.StopProcessing(nodeID, endTime)
		labels := prometheus.Labels{
			opLabel: op,
		}
		h.metrics.messages.With(labels).Inc()
		h.metrics.messageHandlingTime.With(labels).Add(float64(handlingTime))

		msg.OnFinishedHandling()
		h.ctx.Log.Debug("finished handling async message",
			zap.String("messageOp", op),
		)
	}()

	state := h.ctx.State.Get()
	engine, ok := h.engineManager.Get(state.Type).Get(state.State)
	if !ok {
		return fmt.Errorf(
			"%w %s running %s",
			errMissingEngine,
			state.State,
			state.Type,
		)
	}

	switch m := body.(type) {
	case *p2ppb.AppRequest:
		return engine.AppRequest(
			ctx,
			nodeID,
			m.RequestId,
			msg.Expiration(),
			m.AppBytes,
		)

	case *p2ppb.AppResponse:
		return engine.AppResponse(ctx, nodeID, m.RequestId, m.AppBytes)

	case *p2ppb.AppError:
		err := &common.AppError{
			Code:    m.ErrorCode,
			Message: m.ErrorMessage,
		}

		return engine.AppRequestFailed(
			ctx,
			nodeID,
			m.RequestId,
			err,
		)

	case *p2ppb.AppGossip:
		return engine.AppGossip(ctx, nodeID, m.AppBytes)

	default:
		return fmt.Errorf(
			"attempt to submit unhandled async msg %s from %s",
			op, nodeID,
		)
	}
}

// Any returned error is treated as fatal
func (h *handler) handleChanMsg(msg message.InboundMessage) error {
	var (
		op        = msg.Op().String()
		body      = msg.Message()
		startTime = h.clock.Time()
		// Check if the chain is in normal operation at the start of message
		// execution (may change during execution)
		isNormalOp = h.ctx.State.Get().State == snow.NormalOp
	)
	if h.ctx.Log.Enabled(logging.Verbo) {
		h.ctx.Log.Verbo("forwarding chan message to consensus",
			zap.String("messageOp", op),
			zap.Stringer("message", body),
		)
	} else {
		h.ctx.Log.Debug("forwarding chan message to consensus",
			zap.String("messageOp", op),
		)
	}
	h.ctx.Lock.Lock()
	lockAcquiredTime := h.clock.Time()
	defer func() {
		h.ctx.Lock.Unlock()

		var (
			endTime      = h.clock.Time()
			lockingTime  = lockAcquiredTime.Sub(startTime)
			handlingTime = endTime.Sub(lockAcquiredTime)
		)
		h.metrics.lockingTime.Add(float64(lockingTime))
		labels := prometheus.Labels{
			opLabel: op,
		}
		h.metrics.messages.With(labels).Inc()
		h.metrics.messageHandlingTime.With(labels).Add(float64(handlingTime))

		msg.OnFinishedHandling()
		h.ctx.Log.Debug("finished handling chan message",
			zap.String("messageOp", op),
		)
		if lockingTime+handlingTime > syncProcessingTimeWarnLimit && isNormalOp {
			h.ctx.Log.Warn("handling chan message took longer than expected",
				zap.Duration("lockingTime", lockingTime),
				zap.Duration("handlingTime", handlingTime),
				zap.String("messageOp", op),
				zap.Stringer("message", body),
			)
		}
	}()

	state := h.ctx.State.Get()
	engine, ok := h.engineManager.Get(state.Type).Get(state.State)
	if !ok {
		return fmt.Errorf(
			"%w %s running %s",
			errMissingEngine,
			state.State,
			state.Type,
		)
	}

	switch msg := body.(type) {
	case *message.VMMessage:
		return engine.Notify(context.TODO(), common.Message(msg.Notification))

	case *message.GossipRequest:
		return engine.Gossip(context.TODO())

	default:
		return fmt.Errorf(
			"attempt to submit unhandled chan msg %s",
			op,
		)
	}
}

func (h *handler) popUnexpiredMsg(queue MessageQueue) (context.Context, Message, bool) {
	for {
		// Get the next message we should process. If the handler is shutting
		// down, we may fail to pop a message.
		ctx, msg, ok := queue.Pop()
		if !ok {
			return nil, Message{}, false
		}

		// If this message's deadline has passed, don't process it.
		if expiration := msg.Expiration(); h.clock.Time().After(expiration) {
			op := msg.Op().String()
			h.ctx.Log.Debug("dropping message",
				zap.String("reason", "timeout"),
				zap.Stringer("nodeID", msg.NodeID()),
				zap.String("messageOp", op),
			)
			span := trace.SpanFromContext(ctx)
			span.AddEvent("dropping message", trace.WithAttributes(
				attribute.String("reason", "timeout"),
			))
			h.metrics.expired.With(prometheus.Labels{
				opLabel: op,
			}).Inc()
			msg.OnFinishedHandling()
			continue
		}

		return ctx, msg, true
	}
}

// Invariant: if closeDispatcher is called, Stop has already been called.
func (h *handler) closeDispatcher(ctx context.Context) {
	if h.numDispatchersClosed.Add(1) < numDispatchersToClose {
		return
	}

	h.shutdown(ctx, h.startClosingTime)
}

// Note: shutdown is only called after all message dispatchers have exited or if
// no message dispatchers ever started.
func (h *handler) shutdown(ctx context.Context, startClosingTime time.Time) {
	// If we are shutting down but haven't properly started, we don't need to
	// close the notification forwarder.
	if h.nf != nil {
		h.nf.Close()
	}

	defer func() {
		if h.onStopped != nil {
			go h.onStopped()
		}

		h.totalClosingTime = h.clock.Time().Sub(startClosingTime)
		close(h.closed)
	}()

	state := h.ctx.State.Get()
	engine, ok := h.engineManager.Get(state.Type).Get(state.State)
	if !ok {
		h.ctx.Log.Error("failed fetching current engine during shutdown",
			zap.Stringer("type", state.Type),
			zap.Stringer("state", state.State),
		)
		return
	}

	if err := engine.Shutdown(ctx); err != nil {
		h.ctx.Log.Error("failed while shutting down the chain",
			zap.Error(err),
		)
	}
}
