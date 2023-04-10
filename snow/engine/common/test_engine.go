// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/version"
)

var (
	errTimeout                       = errors.New("unexpectedly called Timeout")
	errGossip                        = errors.New("unexpectedly called Gossip")
	errNotify                        = errors.New("unexpectedly called Notify")
	errGetStateSummaryFrontier       = errors.New("unexpectedly called GetStateSummaryFrontier")
	errGetStateSummaryFrontierFailed = errors.New("unexpectedly called GetStateSummaryFrontierFailed")
	errStateSummaryFrontier          = errors.New("unexpectedly called StateSummaryFrontier")
	errGetAcceptedStateSummary       = errors.New("unexpectedly called GetAcceptedStateSummary")
	errGetAcceptedStateSummaryFailed = errors.New("unexpectedly called GetAcceptedStateSummaryFailed")
	errAcceptedStateSummary          = errors.New("unexpectedly called AcceptedStateSummary")
	errGetAcceptedFrontier           = errors.New("unexpectedly called GetAcceptedFrontier")
	errGetAcceptedFrontierFailed     = errors.New("unexpectedly called GetAcceptedFrontierFailed")
	errAcceptedFrontier              = errors.New("unexpectedly called AcceptedFrontier")
	errGetAccepted                   = errors.New("unexpectedly called GetAccepted")
	errGetAcceptedFailed             = errors.New("unexpectedly called GetAcceptedFailed")
	errAccepted                      = errors.New("unexpectedly called Accepted")
	errGet                           = errors.New("unexpectedly called Get")
	errGetAncestors                  = errors.New("unexpectedly called GetAncestors")
	errGetFailed                     = errors.New("unexpectedly called GetFailed")
	errGetAncestorsFailed            = errors.New("unexpectedly called GetAncestorsFailed")
	errPut                           = errors.New("unexpectedly called Put")
	errAncestors                     = errors.New("unexpectedly called Ancestors")
	errPushQuery                     = errors.New("unexpectedly called PushQuery")
	errPullQuery                     = errors.New("unexpectedly called PullQuery")
	errQueryFailed                   = errors.New("unexpectedly called QueryFailed")
	errChits                         = errors.New("unexpectedly called Chits")
	errStart                         = errors.New("unexpectedly called Start")

	_ Engine = (*EngineTest)(nil)
)

// EngineTest is a test engine
type EngineTest struct {
	T *testing.T

	CantStart,

	CantIsBootstrapped,
	CantTimeout,
	CantGossip,
	CantHalt,
	CantShutdown,

	CantContext,

	CantNotify,

	CantGetStateSummaryFrontier,
	CantGetStateSummaryFrontierFailed,
	CantStateSummaryFrontier,

	CantGetAcceptedStateSummary,
	CantGetAcceptedStateSummaryFailed,
	CantAcceptedStateSummary,

	CantGetAcceptedFrontier,
	CantGetAcceptedFrontierFailed,
	CantAcceptedFrontier,

	CantGetAccepted,
	CantGetAcceptedFailed,
	CantAccepted,

	CantGet,
	CantGetAncestors,
	CantGetFailed,
	CantGetAncestorsFailed,
	CantPut,
	CantAncestors,

	CantPushQuery,
	CantPullQuery,
	CantQueryFailed,
	CantChits,

	CantConnected,
	CantDisconnected,

	CantHealth,

	CantCrossChainAppRequest,
	CantCrossChainAppRequestFailed,
	CantCrossChainAppResponse,

	CantAppRequest,
	CantAppResponse,
	CantAppGossip,
	CantAppRequestFailed,

	CantGetVM bool

	StartF                                     func(ctx context.Context, startReqID uint32) error
	IsBootstrappedF                            func() bool
	ContextF                                   func() *snow.ConsensusContext
	HaltF                                      func(context.Context)
	TimeoutF, GossipF, ShutdownF               func(context.Context) error
	NotifyF                                    func(context.Context, Message) error
	GetF, GetAncestorsF, PullQueryF            func(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error
	PutF, PushQueryF                           func(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte) error
	AncestorsF                                 func(ctx context.Context, nodeID ids.NodeID, requestID uint32, containers [][]byte) error
	AcceptedFrontierF, GetAcceptedF, AcceptedF func(ctx context.Context, nodeID ids.NodeID, requestID uint32, preferredIDs []ids.ID) error
	ChitsF                                     func(ctx context.Context, nodeID ids.NodeID, requestID uint32, preferredIDs []ids.ID, acceptedIDs []ids.ID) error
	GetStateSummaryFrontierF, GetStateSummaryFrontierFailedF, GetAcceptedStateSummaryFailedF,
	GetAcceptedFrontierF, GetFailedF, GetAncestorsFailedF,
	QueryFailedF, GetAcceptedFrontierFailedF, GetAcceptedFailedF func(ctx context.Context, nodeID ids.NodeID, requestID uint32) error
	AppRequestFailedF           func(ctx context.Context, nodeID ids.NodeID, requestID uint32) error
	StateSummaryFrontierF       func(ctx context.Context, nodeID ids.NodeID, requestID uint32, summary []byte) error
	GetAcceptedStateSummaryF    func(ctx context.Context, nodeID ids.NodeID, requestID uint32, keys []uint64) error
	AcceptedStateSummaryF       func(ctx context.Context, nodeID ids.NodeID, requestID uint32, summaryIDs []ids.ID) error
	ConnectedF                  func(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error
	DisconnectedF               func(ctx context.Context, nodeID ids.NodeID) error
	HealthF                     func(context.Context) (interface{}, error)
	GetVMF                      func() VM
	AppRequestF                 func(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, msg []byte) error
	AppResponseF                func(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error
	AppGossipF                  func(ctx context.Context, nodeID ids.NodeID, msg []byte) error
	CrossChainAppRequestF       func(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, msg []byte) error
	CrossChainAppResponseF      func(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error
	CrossChainAppRequestFailedF func(ctx context.Context, chainID ids.ID, requestID uint32) error
}

func (e *EngineTest) Default(cant bool) {
	e.CantStart = cant
	e.CantIsBootstrapped = cant
	e.CantTimeout = cant
	e.CantGossip = cant
	e.CantHalt = cant
	e.CantShutdown = cant
	e.CantContext = cant
	e.CantNotify = cant
	e.CantGetStateSummaryFrontier = cant
	e.CantGetStateSummaryFrontierFailed = cant
	e.CantStateSummaryFrontier = cant
	e.CantGetAcceptedStateSummary = cant
	e.CantGetAcceptedStateSummaryFailed = cant
	e.CantAcceptedStateSummary = cant
	e.CantGetAcceptedFrontier = cant
	e.CantGetAcceptedFrontierFailed = cant
	e.CantAcceptedFrontier = cant
	e.CantGetAccepted = cant
	e.CantGetAcceptedFailed = cant
	e.CantAccepted = cant
	e.CantGet = cant
	e.CantGetAncestors = cant
	e.CantGetAncestorsFailed = cant
	e.CantGetFailed = cant
	e.CantPut = cant
	e.CantAncestors = cant
	e.CantPushQuery = cant
	e.CantPullQuery = cant
	e.CantQueryFailed = cant
	e.CantChits = cant
	e.CantConnected = cant
	e.CantDisconnected = cant
	e.CantHealth = cant
	e.CantAppRequest = cant
	e.CantAppRequestFailed = cant
	e.CantAppResponse = cant
	e.CantAppGossip = cant
	e.CantGetVM = cant
	e.CantCrossChainAppRequest = cant
	e.CantCrossChainAppRequestFailed = cant
	e.CantCrossChainAppResponse = cant
}

func (e *EngineTest) Start(ctx context.Context, startReqID uint32) error {
	if e.StartF != nil {
		return e.StartF(ctx, startReqID)
	}
	if !e.CantStart {
		return nil
	}
	if e.T != nil {
		e.T.Fatalf("Unexpectedly called Start")
	}
	return errStart
}

func (e *EngineTest) Context() *snow.ConsensusContext {
	if e.ContextF != nil {
		return e.ContextF()
	}
	if !e.CantContext {
		return nil
	}
	if e.T != nil {
		e.T.Fatalf("Unexpectedly called Context")
	}
	return nil
}

func (e *EngineTest) Timeout(ctx context.Context) error {
	if e.TimeoutF != nil {
		return e.TimeoutF(ctx)
	}
	if !e.CantTimeout {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errTimeout)
	}
	return errTimeout
}

func (e *EngineTest) Gossip(ctx context.Context) error {
	if e.GossipF != nil {
		return e.GossipF(ctx)
	}
	if !e.CantGossip {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errGossip)
	}
	return errGossip
}

func (e *EngineTest) Halt(ctx context.Context) {
	if e.HaltF != nil {
		e.HaltF(ctx)
		return
	}
	if e.CantHalt && e.T != nil {
		e.T.Fatalf("Unexpectedly called Halt")
	}
}

func (e *EngineTest) Shutdown(ctx context.Context) error {
	if e.ShutdownF != nil {
		return e.ShutdownF(ctx)
	}
	if !e.CantShutdown {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errShutdown)
	}
	return errShutdown
}

func (e *EngineTest) Notify(ctx context.Context, msg Message) error {
	if e.NotifyF != nil {
		return e.NotifyF(ctx, msg)
	}
	if !e.CantNotify {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errNotify)
	}
	return errNotify
}

func (e *EngineTest) GetStateSummaryFrontier(ctx context.Context, validatorID ids.NodeID, requestID uint32) error {
	if e.GetStateSummaryFrontierF != nil {
		return e.GetStateSummaryFrontierF(ctx, validatorID, requestID)
	}
	if !e.CantGetStateSummaryFrontier {
		return nil
	}
	if e.T != nil {
		e.T.Fatalf("Unexpectedly called GetStateSummaryFrontier")
	}
	return errGetStateSummaryFrontier
}

func (e *EngineTest) StateSummaryFrontier(ctx context.Context, validatorID ids.NodeID, requestID uint32, summary []byte) error {
	if e.StateSummaryFrontierF != nil {
		return e.StateSummaryFrontierF(ctx, validatorID, requestID, summary)
	}
	if !e.CantStateSummaryFrontier {
		return nil
	}
	if e.T != nil {
		e.T.Fatalf("Unexpectedly called CantStateSummaryFrontier")
	}
	return errStateSummaryFrontier
}

func (e *EngineTest) GetStateSummaryFrontierFailed(ctx context.Context, validatorID ids.NodeID, requestID uint32) error {
	if e.GetStateSummaryFrontierFailedF != nil {
		return e.GetStateSummaryFrontierFailedF(ctx, validatorID, requestID)
	}
	if !e.CantGetStateSummaryFrontierFailed {
		return nil
	}
	if e.T != nil {
		e.T.Fatalf("Unexpectedly called GetStateSummaryFrontierFailed")
	}
	return errGetStateSummaryFrontierFailed
}

func (e *EngineTest) GetAcceptedStateSummary(ctx context.Context, validatorID ids.NodeID, requestID uint32, keys []uint64) error {
	if e.GetAcceptedStateSummaryF != nil {
		return e.GetAcceptedStateSummaryF(ctx, validatorID, requestID, keys)
	}
	if !e.CantGetAcceptedStateSummary {
		return nil
	}
	if e.T != nil {
		e.T.Fatalf("Unexpectedly called GetAcceptedStateSummary")
	}
	return errGetAcceptedStateSummary
}

func (e *EngineTest) AcceptedStateSummary(ctx context.Context, validatorID ids.NodeID, requestID uint32, summaryIDs []ids.ID) error {
	if e.AcceptedStateSummaryF != nil {
		return e.AcceptedStateSummaryF(ctx, validatorID, requestID, summaryIDs)
	}
	if !e.CantAcceptedStateSummary {
		return nil
	}
	if e.T != nil {
		e.T.Fatalf("Unexpectedly called AcceptedStateSummary")
	}
	return errAcceptedStateSummary
}

func (e *EngineTest) GetAcceptedStateSummaryFailed(ctx context.Context, validatorID ids.NodeID, requestID uint32) error {
	if e.GetAcceptedStateSummaryFailedF != nil {
		return e.GetAcceptedStateSummaryFailedF(ctx, validatorID, requestID)
	}
	if !e.CantGetAcceptedStateSummaryFailed {
		return nil
	}
	if e.T != nil {
		e.T.Fatalf("Unexpectedly called GetAcceptedStateSummaryFailed")
	}
	return errGetAcceptedStateSummaryFailed
}

func (e *EngineTest) GetAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	if e.GetAcceptedFrontierF != nil {
		return e.GetAcceptedFrontierF(ctx, nodeID, requestID)
	}
	if !e.CantGetAcceptedFrontier {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errGetAcceptedFrontier)
	}
	return errGetAcceptedFrontier
}

func (e *EngineTest) GetAcceptedFrontierFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	if e.GetAcceptedFrontierFailedF != nil {
		return e.GetAcceptedFrontierFailedF(ctx, nodeID, requestID)
	}
	if !e.CantGetAcceptedFrontierFailed {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errGetAcceptedFrontierFailed)
	}
	return errGetAcceptedFrontierFailed
}

func (e *EngineTest) AcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) error {
	if e.AcceptedFrontierF != nil {
		return e.AcceptedFrontierF(ctx, nodeID, requestID, containerIDs)
	}
	if !e.CantAcceptedFrontier {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errAcceptedFrontier)
	}
	return errAcceptedFrontier
}

func (e *EngineTest) GetAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) error {
	if e.GetAcceptedF != nil {
		return e.GetAcceptedF(ctx, nodeID, requestID, containerIDs)
	}
	if !e.CantGetAccepted {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errGetAccepted)
	}
	return errGetAccepted
}

func (e *EngineTest) GetAcceptedFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	if e.GetAcceptedFailedF != nil {
		return e.GetAcceptedFailedF(ctx, nodeID, requestID)
	}
	if !e.CantGetAcceptedFailed {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errGetAcceptedFailed)
	}
	return errGetAcceptedFailed
}

func (e *EngineTest) Accepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) error {
	if e.AcceptedF != nil {
		return e.AcceptedF(ctx, nodeID, requestID, containerIDs)
	}
	if !e.CantAccepted {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errAccepted)
	}
	return errAccepted
}

func (e *EngineTest) Get(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error {
	if e.GetF != nil {
		return e.GetF(ctx, nodeID, requestID, containerID)
	}
	if !e.CantGet {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errGet)
	}
	return errGet
}

func (e *EngineTest) GetAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error {
	if e.GetAncestorsF != nil {
		return e.GetAncestorsF(ctx, nodeID, requestID, containerID)
	}
	if !e.CantGetAncestors {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errGetAncestors)
	}
	return errGetAncestors
}

func (e *EngineTest) GetFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	if e.GetFailedF != nil {
		return e.GetFailedF(ctx, nodeID, requestID)
	}
	if !e.CantGetFailed {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errGetFailed)
	}
	return errGetFailed
}

func (e *EngineTest) GetAncestorsFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	if e.GetAncestorsFailedF != nil {
		return e.GetAncestorsFailedF(ctx, nodeID, requestID)
	}
	if e.CantGetAncestorsFailed {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errGetAncestorsFailed)
	}
	return errGetAncestorsFailed
}

func (e *EngineTest) Put(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte) error {
	if e.PutF != nil {
		return e.PutF(ctx, nodeID, requestID, container)
	}
	if !e.CantPut {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errPut)
	}
	return errPut
}

func (e *EngineTest) Ancestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containers [][]byte) error {
	if e.AncestorsF != nil {
		return e.AncestorsF(ctx, nodeID, requestID, containers)
	}
	if !e.CantAncestors {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errAncestors)
	}
	return errAncestors
}

func (e *EngineTest) PushQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte) error {
	if e.PushQueryF != nil {
		return e.PushQueryF(ctx, nodeID, requestID, container)
	}
	if !e.CantPushQuery {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errPushQuery)
	}
	return errPushQuery
}

func (e *EngineTest) PullQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error {
	if e.PullQueryF != nil {
		return e.PullQueryF(ctx, nodeID, requestID, containerID)
	}
	if !e.CantPullQuery {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errPullQuery)
	}
	return errPullQuery
}

func (e *EngineTest) QueryFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	if e.QueryFailedF != nil {
		return e.QueryFailedF(ctx, nodeID, requestID)
	}
	if !e.CantQueryFailed {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errQueryFailed)
	}
	return errQueryFailed
}

func (e *EngineTest) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, request []byte) error {
	if e.CrossChainAppRequestF != nil {
		return e.CrossChainAppRequestF(ctx, chainID, requestID, deadline, request)
	}
	if !e.CantCrossChainAppRequest {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errCrossChainAppRequest)
	}
	return errCrossChainAppRequest
}

func (e *EngineTest) CrossChainAppRequestFailed(ctx context.Context, chainID ids.ID, requestID uint32) error {
	if e.CrossChainAppRequestFailedF != nil {
		return e.CrossChainAppRequestFailedF(ctx, chainID, requestID)
	}
	if !e.CantCrossChainAppRequestFailed {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errCrossChainAppRequestFailed)
	}
	return errCrossChainAppRequestFailed
}

func (e *EngineTest) CrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, response []byte) error {
	if e.CrossChainAppResponseF != nil {
		return e.CrossChainAppResponseF(ctx, chainID, requestID, response)
	}
	if !e.CantCrossChainAppResponse {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errCrossChainAppResponse)
	}
	return errCrossChainAppResponse
}

func (e *EngineTest) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	if e.AppRequestF != nil {
		return e.AppRequestF(ctx, nodeID, requestID, deadline, request)
	}
	if !e.CantAppRequest {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errAppRequest)
	}
	return errAppRequest
}

func (e *EngineTest) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	if e.AppResponseF != nil {
		return e.AppResponseF(ctx, nodeID, requestID, response)
	}
	if !e.CantAppResponse {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errAppResponse)
	}
	return errAppResponse
}

func (e *EngineTest) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	if e.AppRequestFailedF != nil {
		return e.AppRequestFailedF(ctx, nodeID, requestID)
	}
	if !e.CantAppRequestFailed {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errAppRequestFailed)
	}
	return errAppRequestFailed
}

func (e *EngineTest) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	if e.AppGossipF != nil {
		return e.AppGossipF(ctx, nodeID, msg)
	}
	if !e.CantAppGossip {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errAppGossip)
	}
	return errAppGossip
}

func (e *EngineTest) Chits(ctx context.Context, nodeID ids.NodeID, requestID uint32, preferredIDs []ids.ID, acceptedIDs []ids.ID) error {
	if e.ChitsF != nil {
		return e.ChitsF(ctx, nodeID, requestID, preferredIDs, acceptedIDs)
	}
	if !e.CantChits {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errChits)
	}
	return errChits
}

func (e *EngineTest) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	if e.ConnectedF != nil {
		return e.ConnectedF(ctx, nodeID, nodeVersion)
	}
	if !e.CantConnected {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errConnected)
	}
	return errConnected
}

func (e *EngineTest) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	if e.DisconnectedF != nil {
		return e.DisconnectedF(ctx, nodeID)
	}
	if !e.CantDisconnected {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errDisconnected)
	}
	return errDisconnected
}

func (e *EngineTest) HealthCheck(ctx context.Context) (interface{}, error) {
	if e.HealthF != nil {
		return e.HealthF(ctx)
	}
	if !e.CantHealth {
		return nil, nil
	}
	if e.T != nil {
		e.T.Fatal(errHealthCheck)
	}
	return nil, errHealthCheck
}

func (e *EngineTest) GetVM() VM {
	if e.GetVMF != nil {
		return e.GetVMF()
	}
	if e.CantGetVM && e.T != nil {
		e.T.Fatalf("Unexpectedly called GetVM")
	}
	return nil
}
