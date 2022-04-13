// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"testing"

	"github.com/chain4travel/caminogo/ids"
)

// SubnetTest is a test subnet
type SubnetTest struct {
	T *testing.T

	CantIsBootstrapped, CantBootstrapped bool

	IsBootstrappedF func() bool
	BootstrappedF   func(ids.ID)
}

// Default set the default callable value to [cant]
func (s *SubnetTest) Default(cant bool) {
	s.CantIsBootstrapped = cant
	s.CantBootstrapped = cant
}

// IsBootstrapped calls IsBootstrappedF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail. Defaults to returning false.
func (s *SubnetTest) IsBootstrapped() bool {
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
func (s *SubnetTest) Bootstrapped(chainID ids.ID) {
	if s.BootstrappedF != nil {
		s.BootstrappedF(chainID)
	} else if s.CantBootstrapped && s.T != nil {
		s.T.Fatalf("Unexpectedly called Bootstrapped")
	}
}
