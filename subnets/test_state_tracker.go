// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

var _ StateTracker = (*SyncTrackerTest)(nil)

// SyncTrackerTest is a test subnet
type SyncTrackerTest struct {
	T *testing.T

	CantIsSynced, CantSetState, CantGetState, CantOnSyncCompleted bool

	IsSyncedF        func() bool
	SetStateF        func(chainID ids.ID, state snow.State)
	GetStateF        func(chainID ids.ID) snow.State
	OnSyncCompletedF func() chan struct{}
}

// Default set the default callable value to [cant]
func (s *SyncTrackerTest) Default(cant bool) {
	s.CantIsSynced = cant
	s.CantSetState = cant
	s.CantOnSyncCompleted = cant
}

// IsSynced calls IsSyncedF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail. Defaults to returning false.
func (s *SyncTrackerTest) IsSynced() bool {
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
func (s *SyncTrackerTest) SetState(chainID ids.ID, state snow.State) {
	if s.SetStateF != nil {
		s.SetStateF(chainID, state)
	} else if s.CantSetState && s.T != nil {
		s.T.Fatalf("Unexpectedly called SetState")
	}
}

func (s *SyncTrackerTest) GetState(chainID ids.ID) snow.State {
	if s.GetStateF != nil {
		s.GetStateF(chainID)
	} else if s.CantGetState && s.T != nil {
		s.T.Fatalf("Unexpectedly called GetState")
	}
	return snow.Initializing
}

func (s *SyncTrackerTest) OnSyncCompleted() chan struct{} {
	if s.OnSyncCompletedF != nil {
		return s.OnSyncCompletedF()
	} else if s.CantOnSyncCompleted && s.T != nil {
		s.T.Fatalf("Unexpectedly called OnBootstrapCompleted")
	}
	return nil
}
