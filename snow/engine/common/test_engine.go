// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"errors"
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
)

// EngineTest is a test engine
type EngineTest struct {
	T *testing.T

	CantIsBootstrapped,
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
	CantGetAncestors,
	CantGetFailed,
	CantGetAncestorsFailed,
	CantPut,
	CantMultiPut,

	CantPushQuery,
	CantPullQuery,
	CantQueryFailed,
	CantChits bool

	IsBootstrappedF                                    func() bool
	ContextF                                           func() *snow.Context
	StartupF, GossipF, ShutdownF                       func() error
	NotifyF                                            func(Message) error
	GetF, GetAncestorsF, PullQueryF                    func(validatorID ids.ShortID, requestID uint32, containerID ids.ID) error
	PutF, PushQueryF                                   func(validatorID ids.ShortID, requestID uint32, containerID ids.ID, container []byte) error
	MultiPutF                                          func(validatorID ids.ShortID, requestID uint32, containers [][]byte) error
	AcceptedFrontierF, GetAcceptedF, AcceptedF, ChitsF func(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) error
	GetAcceptedFrontierF, GetFailedF, GetAncestorsFailedF,
	QueryFailedF, GetAcceptedFrontierFailedF, GetAcceptedFailedF func(validatorID ids.ShortID, requestID uint32) error
}

var _ Engine = &EngineTest{}

// Default ...
func (e *EngineTest) Default(cant bool) {
	e.CantIsBootstrapped = cant

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
	e.CantGetAncestors = cant
	e.CantGetAncestorsFailed = cant
	e.CantGetFailed = cant
	e.CantPut = cant
	e.CantMultiPut = cant

	e.CantPushQuery = cant
	e.CantPullQuery = cant
	e.CantQueryFailed = cant
	e.CantChits = cant
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

// Startup ...
func (e *EngineTest) Startup() error {
	if e.StartupF != nil {
		return e.StartupF()
	} else if e.CantStartup {
		if e.T != nil {
			e.T.Fatalf("Unexpectedly called Startup")
		}
		return errors.New("Unexpectedly called Startup")
	}
	return nil
}

// Gossip ...
func (e *EngineTest) Gossip() error {
	if e.GossipF != nil {
		return e.GossipF()
	} else if e.CantGossip {
		if e.T != nil {
			e.T.Fatalf("Unexpectedly called Gossip")
		}
		return errors.New("Unexpectedly called Gossip")
	}
	return nil
}

// Shutdown ...
func (e *EngineTest) Shutdown() error {
	if e.ShutdownF != nil {
		return e.ShutdownF()
	} else if e.CantShutdown {
		if e.T != nil {
			e.T.Fatalf("Unexpectedly called Shutdown")
		}
		return errors.New("Unexpectedly called Shutdown")
	}
	return nil
}

// Notify ...
func (e *EngineTest) Notify(msg Message) error {
	if e.NotifyF != nil {
		return e.NotifyF(msg)
	} else if e.CantNotify {
		if e.T != nil {
			e.T.Fatalf("Unexpectedly called Notify")
		}
		return errors.New("Unexpectedly called Notify")
	}
	return nil
}

// GetAcceptedFrontier ...
func (e *EngineTest) GetAcceptedFrontier(validatorID ids.ShortID, requestID uint32) error {
	if e.GetAcceptedFrontierF != nil {
		return e.GetAcceptedFrontierF(validatorID, requestID)
	} else if e.CantGetAcceptedFrontier {
		if e.T != nil {
			e.T.Fatalf("Unexpectedly called GetAcceptedFrontier")
		}
		return errors.New("Unexpectedly called GetAcceptedFrontier")
	}
	return nil
}

// GetAcceptedFrontierFailed ...
func (e *EngineTest) GetAcceptedFrontierFailed(validatorID ids.ShortID, requestID uint32) error {
	if e.GetAcceptedFrontierFailedF != nil {
		return e.GetAcceptedFrontierFailedF(validatorID, requestID)
	} else if e.CantGetAcceptedFrontierFailed {
		if e.T != nil {
			e.T.Fatalf("Unexpectedly called GetAcceptedFrontierFailed")
		}
		return errors.New("Unexpectedly called GetAcceptedFrontierFailed")
	}
	return nil
}

// AcceptedFrontier ...
func (e *EngineTest) AcceptedFrontier(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) error {
	if e.AcceptedFrontierF != nil {
		return e.AcceptedFrontierF(validatorID, requestID, containerIDs)
	} else if e.CantAcceptedFrontier {
		if e.T != nil {
			e.T.Fatalf("Unexpectedly called AcceptedFrontierF")
		}
		return errors.New("Unexpectedly called AcceptedFrontierF")
	}
	return nil
}

// GetAccepted ...
func (e *EngineTest) GetAccepted(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) error {
	if e.GetAcceptedF != nil {
		return e.GetAcceptedF(validatorID, requestID, containerIDs)
	} else if e.CantGetAccepted {
		if e.T != nil {
			e.T.Fatalf("Unexpectedly called GetAccepted")
		}
		return errors.New("Unexpectedly called GetAccepted")
	}
	return nil
}

// GetAcceptedFailed ...
func (e *EngineTest) GetAcceptedFailed(validatorID ids.ShortID, requestID uint32) error {
	if e.GetAcceptedFailedF != nil {
		return e.GetAcceptedFailedF(validatorID, requestID)
	} else if e.CantGetAcceptedFailed {
		if e.T != nil {
			e.T.Fatalf("Unexpectedly called GetAcceptedFailed")
		}
		return errors.New("Unexpectedly called GetAcceptedFailed")
	}
	return nil
}

// Accepted ...
func (e *EngineTest) Accepted(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) error {
	if e.AcceptedF != nil {
		return e.AcceptedF(validatorID, requestID, containerIDs)
	} else if e.CantAccepted {
		if e.T != nil {
			e.T.Fatalf("Unexpectedly called Accepted")
		}
		return errors.New("Unexpectedly called Accepted")
	}
	return nil
}

// Get ...
func (e *EngineTest) Get(validatorID ids.ShortID, requestID uint32, containerID ids.ID) error {
	if e.GetF != nil {
		return e.GetF(validatorID, requestID, containerID)
	} else if e.CantGet {
		if e.T != nil {
			e.T.Fatalf("Unexpectedly called Get")
		}
		return errors.New("Unexpectedly called Get")
	}
	return nil
}

// GetAncestors ...
func (e *EngineTest) GetAncestors(validatorID ids.ShortID, requestID uint32, containerID ids.ID) error {
	if e.GetAncestorsF != nil {
		e.GetAncestorsF(validatorID, requestID, containerID)
	} else if e.CantGetAncestors && e.T != nil {
		e.T.Fatalf("Unexpectedly called GetAncestors")
	}
	return nil
}

// GetFailed ...
func (e *EngineTest) GetFailed(validatorID ids.ShortID, requestID uint32) error {
	if e.GetFailedF != nil {
		return e.GetFailedF(validatorID, requestID)
	} else if e.CantGetFailed {
		if e.T != nil {
			e.T.Fatalf("Unexpectedly called GetFailed")
		}
		return errors.New("Unexpectedly called GetFailed")
	}
	return nil
}

// GetAncestorsFailed ...
func (e *EngineTest) GetAncestorsFailed(validatorID ids.ShortID, requestID uint32) error {
	if e.GetAncestorsFailedF != nil {
		return e.GetAncestorsFailedF(validatorID, requestID)
	} else if e.CantGetAncestorsFailed {
		if e.T != nil {
			e.T.Fatalf("Unexpectedly called GetAncestorsFailed")
		}
		return errors.New("Unexpectedly called GetAncestorsFailed")
	}
	return nil
}

// Put ...
func (e *EngineTest) Put(validatorID ids.ShortID, requestID uint32, containerID ids.ID, container []byte) error {
	if e.PutF != nil {
		return e.PutF(validatorID, requestID, containerID, container)
	} else if e.CantPut {
		if e.T != nil {
			e.T.Fatalf("Unexpectedly called Put")
		}
		return errors.New("Unexpectedly called Put")
	}
	return nil
}

// MultiPut ...
func (e *EngineTest) MultiPut(validatorID ids.ShortID, requestID uint32, containers [][]byte) error {
	if e.MultiPutF != nil {
		return e.MultiPutF(validatorID, requestID, containers)
	} else if e.CantMultiPut {
		if e.T != nil {
			e.T.Fatalf("Unexpectedly called MultiPut")
		}
		return errors.New("Unexpectedly called MultiPut")
	}
	return nil
}

// PushQuery ...
func (e *EngineTest) PushQuery(validatorID ids.ShortID, requestID uint32, containerID ids.ID, container []byte) error {
	if e.PushQueryF != nil {
		return e.PushQueryF(validatorID, requestID, containerID, container)
	} else if e.CantPushQuery {
		if e.T != nil {
			e.T.Fatalf("Unexpectedly called PushQuery")
		}
		return errors.New("Unexpectedly called PushQuery")
	}
	return nil
}

// PullQuery ...
func (e *EngineTest) PullQuery(validatorID ids.ShortID, requestID uint32, containerID ids.ID) error {
	if e.PullQueryF != nil {
		return e.PullQueryF(validatorID, requestID, containerID)
	} else if e.CantPullQuery {
		if e.T != nil {
			e.T.Fatalf("Unexpectedly called PullQuery")
		}
		return errors.New("Unexpectedly called PullQuery")
	}
	return nil
}

// QueryFailed ...
func (e *EngineTest) QueryFailed(validatorID ids.ShortID, requestID uint32) error {
	if e.QueryFailedF != nil {
		return e.QueryFailedF(validatorID, requestID)
	} else if e.CantQueryFailed {
		if e.T != nil {
			e.T.Fatalf("Unexpectedly called QueryFailed")
		}
		return errors.New("Unexpectedly called QueryFailed")
	}
	return nil
}

// Chits ...
func (e *EngineTest) Chits(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) error {
	if e.ChitsF != nil {
		return e.ChitsF(validatorID, requestID, containerIDs)
	} else if e.CantChits {
		if e.T != nil {
			e.T.Fatalf("Unexpectedly called Chits")
		}
		return errors.New("Unexpectedly called Chits")
	}
	return nil
}

// IsBootstrapped ...
func (e *EngineTest) IsBootstrapped() bool {
	if e.IsBootstrappedF != nil {
		return e.IsBootstrappedF()
	} else if e.CantIsBootstrapped {
		if e.T != nil {
			e.T.Fatalf("Unexpectedly called IsBootstrapped")
		}
		return false
	}
	return false
}
