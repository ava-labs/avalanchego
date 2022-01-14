// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ Bootstrapable = &BootstrapableTest{}

	errForceAccepted = errors.New("unexpectedly called ForceAccepted")
	errClear         = errors.New("unexpectedly called Clear")
)

// BootstrapableTest is a test engine that supports bootstrapping
type BootstrapableTest struct {
	T *testing.T

	CantForceAccepted, CantClear bool

	ClearF         func() error
	ForceAcceptedF func(acceptedContainerIDs []ids.ID) error
}

// Default sets the default on call handling
func (b *BootstrapableTest) Default(cant bool) {
	b.CantForceAccepted = cant
}

// Clear implements the Bootstrapable interface
func (b *BootstrapableTest) Clear() error {
	if b.ClearF != nil {
		return b.ClearF()
	} else if b.CantClear {
		if b.T != nil {
			b.T.Fatalf("Unexpectedly called Clear")
		}
		return errClear
	}
	return nil
}

// ForceAccepted implements the Bootstrapable interface
func (b *BootstrapableTest) ForceAccepted(containerIDs []ids.ID) error {
	if b.ForceAcceptedF != nil {
		return b.ForceAcceptedF(containerIDs)
	} else if b.CantForceAccepted {
		if b.T != nil {
			b.T.Fatalf("Unexpectedly called ForceAccepted")
		}
		return errForceAccepted
	}
	return nil
}
