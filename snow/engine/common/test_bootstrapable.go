// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanche-go/ids"
)

// BootstrapableTest is a test engine that supports bootstrapping
type BootstrapableTest struct {
	T *testing.T

	CantCurrentAcceptedFrontier,
	CantFilterAccepted,
	CantForceAccepted bool

	CurrentAcceptedFrontierF func() (acceptedContainerIDs ids.Set)
	FilterAcceptedF          func(containerIDs ids.Set) (acceptedContainerIDs ids.Set)
	ForceAcceptedF           func(acceptedContainerIDs ids.Set) error
}

// Default sets the default on call handling
func (b *BootstrapableTest) Default(cant bool) {
	b.CantCurrentAcceptedFrontier = cant
	b.CantFilterAccepted = cant
	b.CantForceAccepted = cant
}

// CurrentAcceptedFrontier implements the Bootstrapable interface
func (b *BootstrapableTest) CurrentAcceptedFrontier() ids.Set {
	if b.CurrentAcceptedFrontierF != nil {
		return b.CurrentAcceptedFrontierF()
	}
	if b.CantCurrentAcceptedFrontier && b.T != nil {
		b.T.Fatalf("Unexpectedly called CurrentAcceptedFrontier")
	}
	return ids.Set{}
}

// FilterAccepted implements the Bootstrapable interface
func (b *BootstrapableTest) FilterAccepted(containerIDs ids.Set) ids.Set {
	if b.FilterAcceptedF != nil {
		return b.FilterAcceptedF(containerIDs)
	}
	if b.CantFilterAccepted && b.T != nil {
		b.T.Fatalf("Unexpectedly called FilterAccepted")
	}
	return ids.Set{}
}

// ForceAccepted implements the Bootstrapable interface
func (b *BootstrapableTest) ForceAccepted(containerIDs ids.Set) error {
	if b.ForceAcceptedF != nil {
		return b.ForceAcceptedF(containerIDs)
	} else if b.CantForceAccepted {
		if b.T != nil {
			b.T.Fatalf("Unexpectedly called ForceAccepted")
		}
		return errors.New("unexpectedly called ForceAccepted")
	}
	return nil
}
