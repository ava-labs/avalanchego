// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package enginetest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

// BootstrapTracker is a test subnet
type BootstrapTracker struct {
	T *testing.T

	CantIsBootstrapped, CantBootstrapped, CantOnBootstrapCompleted bool

	IsBootstrappedF func() bool
	BootstrappedF   func(ids.ID)

	OnBootstrapCompletedF func() chan struct{}
}

// Default set the default callable value to [cant]
func (s *BootstrapTracker) Default(cant bool) {
	s.CantIsBootstrapped = cant
	s.CantBootstrapped = cant
	s.CantOnBootstrapCompleted = cant
}

// IsBootstrapped calls IsBootstrappedF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail. Defaults to returning false.
func (s *BootstrapTracker) IsBootstrapped() bool {
	if s.IsBootstrappedF != nil {
		return s.IsBootstrappedF()
	}
	if s.CantIsBootstrapped && s.T != nil {
		require.FailNow(s.T, "Unexpectedly called IsBootstrapped")
	}
	return false
}

// Bootstrapped calls BootstrappedF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *BootstrapTracker) Bootstrapped(chainID ids.ID) {
	if s.BootstrappedF != nil {
		s.BootstrappedF(chainID)
	} else if s.CantBootstrapped && s.T != nil {
		require.FailNow(s.T, "Unexpectedly called Bootstrapped")
	}
}

func (s *BootstrapTracker) AllBootstrapped() <-chan struct{} {
	if s.OnBootstrapCompletedF != nil {
		return s.OnBootstrapCompletedF()
	} else if s.CantOnBootstrapCompleted && s.T != nil {
		require.FailNow(s.T, "Unexpectedly called OnBootstrapCompleted")
	}
	return nil
}
