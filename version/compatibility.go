// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var (
	errIncompatible = errors.New("peers version is incompatible")

	_ Compatibility = (*compatibility)(nil)
)

// Compatibility a utility for checking the compatibility of peer versions
type Compatibility interface {
	// Returns the local version
	Version() *Application

	// Returns nil if the provided version is compatible with the local version.
	// This means that the version is connectable and that consensus messages
	// can be made to them.
	Compatible(*Application) error
}

type compatibility struct {
	version *Application

	minCompatable     *Application
	minCompatableTime time.Time
	prevMinCompatable *Application

	clock mockable.Clock
}

// NewCompatibility returns a compatibility checker with the provided options
func NewCompatibility(
	version *Application,
	minCompatable *Application,
	minCompatableTime time.Time,
	prevMinCompatable *Application,
) Compatibility {
	return &compatibility{
		version:           version,
		minCompatable:     minCompatable,
		minCompatableTime: minCompatableTime,
		prevMinCompatable: prevMinCompatable,
	}
}

func (c *compatibility) Version() *Application {
	return c.version
}

func (c *compatibility) Compatible(peer *Application) error {
	if err := c.version.Compatible(peer); err != nil {
		return err
	}

	if !peer.Before(c.minCompatable) {
		// The peer is at least the minimum compatible version.
		return nil
	}

	// The peer is going to be marked as incompatible at [c.minCompatableTime].
	now := c.clock.Time()
	if !now.Before(c.minCompatableTime) {
		return errIncompatible
	}

	// The minCompatable check isn't being enforced yet.
	if !peer.Before(c.prevMinCompatable) {
		// The peer is at least the previous minimum compatible version.
		return nil
	}
	return errIncompatible
}
