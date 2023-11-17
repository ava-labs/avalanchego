// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
)

// Config wraps the common configurations that are needed by a Snow consensus
// engine
type Config struct {
	Ctx     *snow.ConsensusContext
	Beacons validators.Manager

	SampleK          int
	Alpha            uint64
	StartupTracker   tracker.Startup
	Sender           Sender
	Bootstrapable    Bootstrapable
	BootstrapTracker BootstrapTracker
	Timer            Timer

	// This node will only consider the first [AncestorsMaxContainersReceived]
	// containers in an ancestors message it receives.
	AncestorsMaxContainersReceived int

	SharedCfg *SharedConfig
}

// Shared among common.bootstrapper and snowman/avalanche bootstrapper
type SharedConfig struct {
	// Tracks the last requestID that was used in a request
	RequestID uint32

	// True if RestartBootstrap has been called at least once
	Restarted bool
}
