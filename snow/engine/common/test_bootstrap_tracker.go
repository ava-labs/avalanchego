// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

// BootstrapTrackerTest is a test subnet
type BootstrapTrackerTest struct {
	T *testing.T

	CantIsBootstrapped, CantBootstrapped, CantOnBootstrapCompleted bool

	IsBootstrappedF func() bool
	BootstrappedF   func(ids.ID)

	OnBootstrapCompletedF func() chan struct{}
}

// Default set the default callable value to [cant]
func (s *BootstrapTrackerTest) Default(cant bool) {
	s.CantIsBootstrapped = cant
	s.CantBootstrapped = cant
	s.CantOnBootstrapCompleted = cant
}

// IsBootstrapped calls IsBootstrappedF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail. Defaults to returning false.
func (s *BootstrapTrackerTest) IsBootstrapped() bool {
	if s.IsBootstrappedF != nil {
		return s.IsBootstrappedF()
	}
	if s.CantIsBootstrapped && s.T != nil {
		s.T.Fatalf("Unexpectedly called IsBootstrapped")
	}
	return false
}

// Bootstrapped calls BootstrappedF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *BootstrapTrackerTest) Bootstrapped(chainID ids.ID) {
	if s.BootstrappedF != nil {
		s.BootstrappedF(chainID)
	} else if s.CantBootstrapped && s.T != nil {
		s.T.Fatalf("Unexpectedly called Bootstrapped")
	}
}

func (s *BootstrapTrackerTest) OnBootstrapCompleted() chan struct{} {
	if s.OnBootstrapCompletedF != nil {
		return s.OnBootstrapCompletedF()
	} else if s.CantOnBootstrapCompleted && s.T != nil {
		s.T.Fatalf("Unexpectedly called OnBootstrapCompleted")
	}
	return nil
}
