// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
)

// EngineTest is a test engine
type EngineTest struct {
	T *testing.T

	CantStartup,
	CantGossip,
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
	CantGetFailed,
	CantPut,

	CantPushQuery,
	CantPullQuery,
	CantQueryFailed,
	CantChits bool

	StartupF, GossipF, ShutdownF                                                       func()
	ContextF                                                                           func() *snow.Context
	NotifyF                                                                            func(Message)
	GetF, PullQueryF                                                                   func(validatorID ids.ShortID, requestID uint32, containerID ids.ID)
	GetFailedF                                                                         func(validatorID ids.ShortID, requestID uint32)
	PutF, PushQueryF                                                                   func(validatorID ids.ShortID, requestID uint32, containerID ids.ID, container []byte)
	GetAcceptedFrontierF, GetAcceptedFrontierFailedF, GetAcceptedFailedF, QueryFailedF func(validatorID ids.ShortID, requestID uint32)
	AcceptedFrontierF, GetAcceptedF, AcceptedF, ChitsF                                 func(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set)
}

// Default ...
func (e *EngineTest) Default(cant bool) {
	e.CantStartup = cant
	e.CantGossip = cant
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
	e.CantGetFailed = cant
	e.CantPut = cant

	e.CantPushQuery = cant
	e.CantPullQuery = cant
	e.CantQueryFailed = cant
	e.CantChits = cant
}

// Startup ...
func (e *EngineTest) Startup() {
	if e.StartupF != nil {
		e.StartupF()
	} else if e.CantStartup && e.T != nil {
		e.T.Fatalf("Unexpectedly called Startup")
	}
}

// Gossip ...
func (e *EngineTest) Gossip() {
	if e.GossipF != nil {
		e.GossipF()
	} else if e.CantGossip && e.T != nil {
		e.T.Fatalf("Unexpectedly called Gossip")
	}
}

// Shutdown ...
func (e *EngineTest) Shutdown() {
	if e.ShutdownF != nil {
		e.ShutdownF()
	} else if e.CantShutdown && e.T != nil {
		e.T.Fatalf("Unexpectedly called Shutdown")
	}
}

// Context ...
func (e *EngineTest) Context() *snow.Context {
	if e.ContextF != nil {
		return e.ContextF()
	}
	if e.CantContext && e.T != nil {
		e.T.Fatalf("Unexpectedly called Context")
	}
	return nil
}

// Notify ...
func (e *EngineTest) Notify(msg Message) {
	if e.NotifyF != nil {
		e.NotifyF(msg)
	} else if e.CantNotify && e.T != nil {
		e.T.Fatalf("Unexpectedly called Notify")
	}
}

// GetAcceptedFrontier ...
func (e *EngineTest) GetAcceptedFrontier(validatorID ids.ShortID, requestID uint32) {
	if e.GetAcceptedFrontierF != nil {
		e.GetAcceptedFrontierF(validatorID, requestID)
	} else if e.CantGetAcceptedFrontier && e.T != nil {
		e.T.Fatalf("Unexpectedly called GetAcceptedFrontier")
	}
}

// GetAcceptedFrontierFailed ...
func (e *EngineTest) GetAcceptedFrontierFailed(validatorID ids.ShortID, requestID uint32) {
	if e.GetAcceptedFrontierFailedF != nil {
		e.GetAcceptedFrontierFailedF(validatorID, requestID)
	} else if e.CantGetAcceptedFrontierFailed && e.T != nil {
		e.T.Fatalf("Unexpectedly called GetAcceptedFrontierFailed")
	}
}

// AcceptedFrontier ...
func (e *EngineTest) AcceptedFrontier(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) {
	if e.AcceptedFrontierF != nil {
		e.AcceptedFrontierF(validatorID, requestID, containerIDs)
	} else if e.CantAcceptedFrontier && e.T != nil {
		e.T.Fatalf("Unexpectedly called AcceptedFrontierF")
	}
}

// GetAccepted ...
func (e *EngineTest) GetAccepted(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) {
	if e.GetAcceptedF != nil {
		e.GetAcceptedF(validatorID, requestID, containerIDs)
	} else if e.CantGetAccepted && e.T != nil {
		e.T.Fatalf("Unexpectedly called GetAccepted")
	}
}

// GetAcceptedFailed ...
func (e *EngineTest) GetAcceptedFailed(validatorID ids.ShortID, requestID uint32) {
	if e.GetAcceptedFailedF != nil {
		e.GetAcceptedFailedF(validatorID, requestID)
	} else if e.CantGetAcceptedFailed && e.T != nil {
		e.T.Fatalf("Unexpectedly called GetAcceptedFailed")
	}
}

// Accepted ...
func (e *EngineTest) Accepted(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) {
	if e.AcceptedF != nil {
		e.AcceptedF(validatorID, requestID, containerIDs)
	} else if e.CantAccepted && e.T != nil {
		e.T.Fatalf("Unexpectedly called Accepted")
	}
}

// Get ...
func (e *EngineTest) Get(validatorID ids.ShortID, requestID uint32, containerID ids.ID) {
	if e.GetF != nil {
		e.GetF(validatorID, requestID, containerID)
	} else if e.CantGet && e.T != nil {
		e.T.Fatalf("Unexpectedly called Get")
	}
}

// GetFailed ...
func (e *EngineTest) GetFailed(validatorID ids.ShortID, requestID uint32) {
	if e.GetFailedF != nil {
		e.GetFailedF(validatorID, requestID)
	} else if e.CantGetFailed && e.T != nil {
		e.T.Fatalf("Unexpectedly called GetFailed")
	}
}

// Put ...
func (e *EngineTest) Put(validatorID ids.ShortID, requestID uint32, containerID ids.ID, container []byte) {
	if e.PutF != nil {
		e.PutF(validatorID, requestID, containerID, container)
	} else if e.CantPut && e.T != nil {
		e.T.Fatalf("Unexpectedly called Put")
	}
}

// PushQuery ...
func (e *EngineTest) PushQuery(validatorID ids.ShortID, requestID uint32, containerID ids.ID, container []byte) {
	if e.PushQueryF != nil {
		e.PushQueryF(validatorID, requestID, containerID, container)
	} else if e.CantPushQuery && e.T != nil {
		e.T.Fatalf("Unexpectedly called PushQuery")
	}
}

// PullQuery ...
func (e *EngineTest) PullQuery(validatorID ids.ShortID, requestID uint32, containerID ids.ID) {
	if e.PullQueryF != nil {
		e.PullQueryF(validatorID, requestID, containerID)
	} else if e.CantPullQuery && e.T != nil {
		e.T.Fatalf("Unexpectedly called PullQuery")
	}
}

// QueryFailed ...
func (e *EngineTest) QueryFailed(validatorID ids.ShortID, requestID uint32) {
	if e.QueryFailedF != nil {
		e.QueryFailedF(validatorID, requestID)
	} else if e.CantQueryFailed && e.T != nil {
		e.T.Fatalf("Unexpectedly called QueryFailed")
	}
}

// Chits ...
func (e *EngineTest) Chits(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) {
	if e.ChitsF != nil {
		e.ChitsF(validatorID, requestID, containerIDs)
	} else if e.CantChits && e.T != nil {
		e.T.Fatalf("Unexpectedly called Chits")
	}
}
