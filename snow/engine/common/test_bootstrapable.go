// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	errForceAccepted = errors.New("unexpectedly called ForceAccepted")

	_ Bootstrapable = &BootstrapableTest{}
)

// BootstrapableTest is a test engine that supports bootstrapping
type BootstrapableTest struct {
	T *testing.T

	CantForceAccepted bool
	ForceAcceptedF    func(acceptedContainerIDs []ids.ID) error
}

// Default sets the default on call handling
func (b *BootstrapableTest) Default(cant bool) {
	b.CantForceAccepted = cant
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
