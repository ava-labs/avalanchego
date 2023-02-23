// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

var _ SyncTracker = (*SyncTrackerTest)(nil)

// SyncTrackerTest is a test subnet
type SyncTrackerTest struct {
	T *testing.T

	CantIsSynced, CantBootstrapped, CantOnSyncCompleted bool

	IsSyncedF     func() bool
	BootstrappedF func(ids.ID)

	OnSyncCompletedF func() chan struct{}
}

// Default set the default callable value to [cant]
func (s *SyncTrackerTest) Default(cant bool) {
	s.CantIsSynced = cant
	s.CantBootstrapped = cant
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

// Bootstrapped calls BootstrappedF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *SyncTrackerTest) Bootstrapped(chainID ids.ID) {
	if s.BootstrappedF != nil {
		s.BootstrappedF(chainID)
	} else if s.CantBootstrapped && s.T != nil {
		s.T.Fatalf("Unexpectedly called Bootstrapped")
	}
}

func (s *SyncTrackerTest) OnSyncCompleted() chan struct{} {
	if s.OnSyncCompletedF != nil {
		return s.OnSyncCompletedF()
	} else if s.CantOnSyncCompleted && s.T != nil {
		s.T.Fatalf("Unexpectedly called OnBootstrapCompleted")
	}
	return nil
}
