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

	CantIsSubnetSynced, CantStartState, CantStopState,
	CantGetState, CantIsStateStopped, CantOnSyncCompleted bool

	IsSubnetSyncedF  func() bool
	StartStateF      func(chainID ids.ID, state State)
	StopStateF       func(chainID ids.ID, state State)
	GetStateF        func(chainID ids.ID) State
	IsStateStoppedF  func(chainID ids.ID, state State) bool
	OnSyncCompletedF func() chan struct{}
}

// Default set the default callable value to [cant]
func (s *SubnetStateTrackerTest) Default(cant bool) {
	s.CantIsSubnetSynced = cant
	s.CantStartState = cant
	s.CantStopState = cant
	s.CantGetState = cant
	s.CantIsStateStopped = cant
	s.CantOnSyncCompleted = cant
}

// IsSynced calls IsSyncedF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail. Defaults to returning false.
func (s *SubnetStateTrackerTest) IsSubnetSynced() bool {
	if s.IsSubnetSyncedF != nil {
		return s.IsSubnetSyncedF()
	}
	if s.CantIsSubnetSynced && s.T != nil {
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

func (s *SubnetStateTrackerTest) GetState(chainID ids.ID) State {
	if s.GetStateF != nil {
		return s.GetStateF(chainID)
	} else if s.CantGetState && s.T != nil {
		s.T.Fatalf("Unexpectedly called GetState")
	}
	return Initializing
}

func (s *SubnetStateTrackerTest) IsStateStopped(chainID ids.ID, state State) bool {
	if s.IsStateStoppedF != nil {
		return s.IsStateStoppedF(chainID, state)
	} else if s.CantIsStateStopped && s.T != nil {
		s.T.Fatalf("Unexpectedly called IsStateStoppedF")
	}
	return false
}

func (s *SubnetStateTrackerTest) OnSyncCompleted() chan struct{} {
	if s.OnSyncCompletedF != nil {
		return s.OnSyncCompletedF()
	} else if s.CantOnSyncCompleted && s.T != nil {
		s.T.Fatalf("Unexpectedly called OnBootstrapCompleted")
	}
	return nil
}
