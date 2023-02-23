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

	CantIsSynced, CantSetState, CantGetState, CantOnSyncCompleted bool

	IsSyncedF        func() bool
	SetStateF        func(chainID ids.ID, state State)
	GetStateF        func(chainID ids.ID) State
	OnSyncCompletedF func() chan struct{}
}

// Default set the default callable value to [cant]
func (s *SubnetStateTrackerTest) Default(cant bool) {
	s.CantIsSynced = cant
	s.CantSetState = cant
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
func (s *SubnetStateTrackerTest) SetState(chainID ids.ID, state State) {
	if s.SetStateF != nil {
		s.SetStateF(chainID, state)
	} else if s.CantSetState && s.T != nil {
		s.T.Fatalf("Unexpectedly called SetState")
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

func (s *SubnetStateTrackerTest) OnSyncCompleted() chan struct{} {
	if s.OnSyncCompletedF != nil {
		return s.OnSyncCompletedF()
	} else if s.CantOnSyncCompleted && s.T != nil {
		s.T.Fatalf("Unexpectedly called OnBootstrapCompleted")
	}
	return nil
}
