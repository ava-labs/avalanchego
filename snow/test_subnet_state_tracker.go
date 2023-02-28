// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

var _ SubnetStateTracker = (*SubnetStateTrackerTest)(nil)

// SubnetStateTrackerTest is a test subnet
type SubnetStateTrackerTest struct {
	T *testing.T

	CantIsSynced, CantStartState, CantStopState,
	CantGetState, CantIsChainBootstrapped, CantOnSyncCompleted bool

	IsSyncedF            func() bool
	StartStateF          func(chainID ids.ID, state State)
	StopStateF           func(chainID ids.ID, state State)
	GetStateF            func(chainID ids.ID) State
	IsChainBootstrappedF func(chainID ids.ID) bool
	OnSyncCompletedF     func() chan struct{}
}

// Default set the default callable value to [cant]
func (s *SubnetStateTrackerTest) Default(cant bool) {
	s.CantIsSynced = cant
	s.CantStartState = cant
	s.CantStopState = cant
	s.CantGetState = cant
	s.CantIsChainBootstrapped = cant
	s.CantOnSyncCompleted = cant
}

// IsSynced calls IsSyncedF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail. Defaults to returning false.
func (s *SubnetStateTrackerTest) IsSynced() bool {
	if s.IsSyncedF != nil {
		return s.IsSyncedF()
	}
	if s.CantIsSynced && s.T != nil {
		s.T.Fatalf("Unexpectedly called IsSynced")
	}
	return false
}

// SetState calls SetStateF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *SubnetStateTrackerTest) StartState(chainID ids.ID, state State) {
	if s.StartStateF != nil {
		s.StartStateF(chainID, state)
	} else if s.CantStartState && s.T != nil {
		s.T.Fatalf("Unexpectedly called StartState")
	}
}

func (s *SubnetStateTrackerTest) StopState(chainID ids.ID, state State) {
	if s.StopStateF != nil {
		s.StopStateF(chainID, state)
	} else if s.CantStopState && s.T != nil {
		s.T.Fatalf("Unexpectedly called StopState")
	}
}

func (s *SubnetStateTrackerTest) IsChainBootstrapped(chainID ids.ID) bool {
	if s.IsChainBootstrappedF != nil {
		return s.IsChainBootstrappedF(chainID)
	} else if s.CantIsChainBootstrapped && s.T != nil {
		s.T.Fatalf("Unexpectedly called IsChainBootstrapped")
	}
	return false
}

func (s *SubnetStateTrackerTest) GetState(chainID ids.ID) State {
	if s.GetStateF != nil {
		return s.GetStateF(chainID)
	} else if s.CantGetState && s.T != nil {
		s.T.Fatalf("Unexpectedly called GetState")
	}
	return Initializing
}

func (s *SubnetStateTrackerTest) OnSyncCompleted() chan struct{} {
	if s.OnSyncCompletedF != nil {
		return s.OnSyncCompletedF()
	} else if s.CantOnSyncCompleted && s.T != nil {
		s.T.Fatalf("Unexpectedly called OnSyncCompleted")
	}
	return nil
}
