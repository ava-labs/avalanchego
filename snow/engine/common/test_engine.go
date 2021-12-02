// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

var (
	errTimeout                   = errors.New("unexpectedly called Timeout")
	errGossip                    = errors.New("unexpectedly called Gossip")
	errNotify                    = errors.New("unexpectedly called Notify")
	errGetAcceptedFrontier       = errors.New("unexpectedly called GetAcceptedFrontier")
	errGetAcceptedFrontierFailed = errors.New("unexpectedly called GetAcceptedFrontierFailed")
	errAcceptedFrontier          = errors.New("unexpectedly called AcceptedFrontier")
	errGetAccepted               = errors.New("unexpectedly called GetAccepted")
	errGetAcceptedFailed         = errors.New("unexpectedly called GetAcceptedFailed")
	errAccepted                  = errors.New("unexpectedly called Accepted")
	errGet                       = errors.New("unexpectedly called Get")
	errGetAncestors              = errors.New("unexpectedly called GetAncestors")
	errGetFailed                 = errors.New("unexpectedly called GetFailed")
	errGetAncestorsFailed        = errors.New("unexpectedly called GetAncestorsFailed")
	errPut                       = errors.New("unexpectedly called Put")
	errMultiPut                  = errors.New("unexpectedly called MultiPut")
	errPushQuery                 = errors.New("unexpectedly called PushQuery")
	errPullQuery                 = errors.New("unexpectedly called PullQuery")
	errQueryFailed               = errors.New("unexpectedly called QueryFailed")
	errChits                     = errors.New("unexpectedly called Chits")

	_ Engine = &EngineTest{}
)

// EngineTest is a test engine
type EngineTest struct {
	T *testing.T

	CantIsBootstrapped,
	CantTimeout,
	CantGossip,
	CantHalt,
	CantShutdown,

	CantContext,

	CantNotify,

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
	CantMultiPut,

	CantPushQuery,
	CantPullQuery,
	CantQueryFailed,
	CantChits,

	CantConnected,
	CantDisconnected,

	CantHealth,

	CantAppRequest,
	CantAppResponse,
	CantAppGossip,
	CantAppRequestFailed,

	CantGetVM bool

	IsBootstrappedF                                    func() bool
	ContextF                                           func() *snow.ConsensusContext
	HaltF                                              func()
	TimeoutF, GossipF, ShutdownF                       func() error
	NotifyF                                            func(Message) error
	GetF, GetAncestorsF, PullQueryF                    func(nodeID ids.ShortID, requestID uint32, containerID ids.ID) error
	PutF, PushQueryF                                   func(nodeID ids.ShortID, requestID uint32, containerID ids.ID, container []byte) error
	MultiPutF                                          func(nodeID ids.ShortID, requestID uint32, containers [][]byte) error
	AcceptedFrontierF, GetAcceptedF, AcceptedF, ChitsF func(nodeID ids.ShortID, requestID uint32, containerIDs []ids.ID) error
	GetAcceptedFrontierF, GetFailedF, GetAncestorsFailedF,
	QueryFailedF, GetAcceptedFrontierFailedF, GetAcceptedFailedF, AppRequestFailedF func(nodeID ids.ShortID, requestID uint32) error
	ConnectedF, DisconnectedF func(nodeID ids.ShortID) error
	HealthF                   func() (interface{}, error)
	GetVMF                    func() VM
	AppRequestF, AppResponseF func(nodeID ids.ShortID, requestID uint32, msg []byte) error
	AppGossipF                func(nodeID ids.ShortID, msg []byte) error
}

func (e *EngineTest) Default(cant bool) {
	e.CantIsBootstrapped = cant
	e.CantTimeout = cant
	e.CantGossip = cant
	e.CantHalt = cant
	e.CantShutdown = cant
	e.CantContext = cant
	e.CantNotify = cant
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
	e.CantMultiPut = cant
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
}

func (e *EngineTest) Context() *snow.ConsensusContext {
	if e.ContextF != nil {
		return e.ContextF()
	}
	if e.CantContext && e.T != nil {
		e.T.Fatalf("Unexpectedly called Context")
	}
	return nil
}

func (e *EngineTest) Timeout() error {
	if e.TimeoutF != nil {
		return e.TimeoutF()
	}
	if !e.CantTimeout {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errTimeout)
	}
	return errTimeout
}

func (e *EngineTest) Gossip() error {
	if e.GossipF != nil {
		return e.GossipF()
	}
	if !e.CantGossip {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errGossip)
	}
	return errGossip
}

func (e *EngineTest) Halt() {
	if e.HaltF != nil {
		e.HaltF()
	} else if e.CantHalt && e.T != nil {
		e.T.Fatalf("Unexpectedly called Halt")
	}
}

func (e *EngineTest) Shutdown() error {
	if e.ShutdownF != nil {
		return e.ShutdownF()
	}
	if !e.CantShutdown {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errShutdown)
	}
	return errShutdown
}

func (e *EngineTest) Notify(msg Message) error {
	if e.NotifyF != nil {
		return e.NotifyF(msg)
	}
	if !e.CantNotify {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errNotify)
	}
	return errNotify
}

func (e *EngineTest) GetAcceptedFrontier(nodeID ids.ShortID, requestID uint32) error {
	if e.GetAcceptedFrontierF != nil {
		return e.GetAcceptedFrontierF(nodeID, requestID)
	}
	if !e.CantGetAcceptedFrontier {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errGetAcceptedFrontier)
	}
	return errGetAcceptedFrontier
}

func (e *EngineTest) GetAcceptedFrontierFailed(nodeID ids.ShortID, requestID uint32) error {
	if e.GetAcceptedFrontierFailedF != nil {
		return e.GetAcceptedFrontierFailedF(nodeID, requestID)
	}
	if !e.CantGetAcceptedFrontierFailed {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errGetAcceptedFrontierFailed)
	}
	return errGetAcceptedFrontierFailed
}

func (e *EngineTest) AcceptedFrontier(nodeID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	if e.AcceptedFrontierF != nil {
		return e.AcceptedFrontierF(nodeID, requestID, containerIDs)
	}
	if !e.CantAcceptedFrontier {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errAcceptedFrontier)
	}
	return errAcceptedFrontier
}

func (e *EngineTest) GetAccepted(nodeID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	if e.GetAcceptedF != nil {
		return e.GetAcceptedF(nodeID, requestID, containerIDs)
	}
	if !e.CantGetAccepted {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errGetAccepted)
	}
	return errGetAccepted
}

func (e *EngineTest) GetAcceptedFailed(nodeID ids.ShortID, requestID uint32) error {
	if e.GetAcceptedFailedF != nil {
		return e.GetAcceptedFailedF(nodeID, requestID)
	}
	if !e.CantGetAcceptedFailed {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errGetAcceptedFailed)
	}
	return errGetAcceptedFailed
}

func (e *EngineTest) Accepted(nodeID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	if e.AcceptedF != nil {
		return e.AcceptedF(nodeID, requestID, containerIDs)
	}
	if !e.CantAccepted {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errAccepted)
	}
	return errAccepted
}

func (e *EngineTest) Get(nodeID ids.ShortID, requestID uint32, containerID ids.ID) error {
	if e.GetF != nil {
		return e.GetF(nodeID, requestID, containerID)
	}
	if !e.CantGet {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errGet)
	}
	return errGet
}

func (e *EngineTest) GetAncestors(nodeID ids.ShortID, requestID uint32, containerID ids.ID) error {
	if e.GetAncestorsF != nil {
		return e.GetAncestorsF(nodeID, requestID, containerID)
	}
	if !e.CantGetAncestors {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errGetAncestors)
	}
	return errGetAncestors
}

func (e *EngineTest) GetFailed(nodeID ids.ShortID, requestID uint32) error {
	if e.GetFailedF != nil {
		return e.GetFailedF(nodeID, requestID)
	}
	if !e.CantGetFailed {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errGetFailed)
	}
	return errGetFailed
}

func (e *EngineTest) GetAncestorsFailed(nodeID ids.ShortID, requestID uint32) error {
	if e.GetAncestorsFailedF != nil {
		return e.GetAncestorsFailedF(nodeID, requestID)
	}
	if e.CantGetAncestorsFailed {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errGetAncestorsFailed)
	}
	return errGetAncestorsFailed
}

func (e *EngineTest) Put(nodeID ids.ShortID, requestID uint32, containerID ids.ID, container []byte) error {
	if e.PutF != nil {
		return e.PutF(nodeID, requestID, containerID, container)
	}
	if !e.CantPut {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errPut)
	}
	return errPut
}

func (e *EngineTest) MultiPut(nodeID ids.ShortID, requestID uint32, containers [][]byte) error {
	if e.MultiPutF != nil {
		return e.MultiPutF(nodeID, requestID, containers)
	}
	if !e.CantMultiPut {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errMultiPut)
	}
	return errMultiPut
}

func (e *EngineTest) PushQuery(nodeID ids.ShortID, requestID uint32, containerID ids.ID, container []byte) error {
	if e.PushQueryF != nil {
		return e.PushQueryF(nodeID, requestID, containerID, container)
	}
	if !e.CantPushQuery {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errPushQuery)
	}
	return errPushQuery
}

func (e *EngineTest) PullQuery(nodeID ids.ShortID, requestID uint32, containerID ids.ID) error {
	if e.PullQueryF != nil {
		return e.PullQueryF(nodeID, requestID, containerID)
	}
	if !e.CantPullQuery {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errPullQuery)
	}
	return errPullQuery
}

func (e *EngineTest) QueryFailed(nodeID ids.ShortID, requestID uint32) error {
	if e.QueryFailedF != nil {
		return e.QueryFailedF(nodeID, requestID)
	}
	if !e.CantQueryFailed {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errQueryFailed)
	}
	return errQueryFailed
}

func (e *EngineTest) AppRequest(nodeID ids.ShortID, requestID uint32, deadline time.Time, request []byte) error {
	if e.AppRequestF != nil {
		return e.AppRequestF(nodeID, requestID, request)
	}
	if !e.CantAppRequest {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errAppRequest)
	}
	return errAppRequest
}

func (e *EngineTest) AppResponse(nodeID ids.ShortID, requestID uint32, response []byte) error {
	if e.AppResponseF != nil {
		return e.AppResponseF(nodeID, requestID, response)
	}
	if !e.CantAppResponse {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errAppResponse)
	}
	return errAppResponse
}

func (e *EngineTest) AppRequestFailed(nodeID ids.ShortID, requestID uint32) error {
	if e.AppRequestFailedF != nil {
		return e.AppRequestFailedF(nodeID, requestID)
	}
	if !e.CantAppRequestFailed {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errAppRequestFailed)
	}
	return errAppRequestFailed
}

func (e *EngineTest) AppGossip(nodeID ids.ShortID, msg []byte) error {
	if e.AppGossipF != nil {
		return e.AppGossipF(nodeID, msg)
	}
	if !e.CantAppGossip {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errAppGossip)
	}
	return errAppGossip
}

func (e *EngineTest) Chits(nodeID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	if e.ChitsF != nil {
		return e.ChitsF(nodeID, requestID, containerIDs)
	}
	if !e.CantChits {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errChits)
	}
	return errChits
}

func (e *EngineTest) Connected(nodeID ids.ShortID) error {
	if e.ConnectedF != nil {
		return e.ConnectedF(nodeID)
	}
	if !e.CantConnected {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errConnected)
	}
	return errConnected
}

func (e *EngineTest) Disconnected(nodeID ids.ShortID) error {
	if e.DisconnectedF != nil {
		return e.DisconnectedF(nodeID)
	}
	if !e.CantDisconnected {
		return nil
	}
	if e.T != nil {
		e.T.Fatal(errDisconnected)
	}
	return errDisconnected
}

func (e *EngineTest) IsBootstrapped() bool {
	if e.IsBootstrappedF != nil {
		return e.IsBootstrappedF()
	}
	if e.CantIsBootstrapped && e.T != nil {
		e.T.Fatalf("Unexpectedly called IsBootstrapped")
	}
	return false
}

func (e *EngineTest) HealthCheck() (interface{}, error) {
	if e.HealthF != nil {
		return e.HealthF()
	}
	if e.CantHealth && e.T != nil {
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
