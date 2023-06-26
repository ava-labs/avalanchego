// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"time"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
)

// Config wraps the common configurations that are needed by a Snow consensus
// engine
type Config struct {
	Ctx     *snow.ConsensusContext
	Beacons validators.Set

	SampleK          int
	Alpha            uint64
	StartupTracker   tracker.Startup
	Sender           Sender
	Bootstrapable    Bootstrapable
	BootstrapTracker BootstrapTracker
	Timer            Timer

	// Should Bootstrap be retried
	RetryBootstrap bool

	// Max number of times to retry bootstrap before warning the node operator
	RetryBootstrapWarnFrequency int

	// Max time to spend fetching a container and its ancestors when responding
	// to a GetAncestors
	MaxTimeGetAncestors time.Duration

	// Max number of containers in an ancestors message sent by this node.
	AncestorsMaxContainersSent int

	// This node will only consider the first [AncestorsMaxContainersReceived]
	// containers in an ancestors message it receives.
	AncestorsMaxContainersReceived int

	SharedCfg *SharedConfig
}

func (c *Config) Context() *snow.ConsensusContext {
	return c.Ctx
}

// IsBootstrapped returns true iff this chain is done bootstrapping
func (c *Config) IsBootstrapped() bool {
	return c.Ctx.State.Get().State == snow.NormalOp
}

// Shared among common.bootstrapper and snowman/avalanche bootstrapper
type SharedConfig struct {
	// Tracks the last requestID that was used in a request
	RequestID uint32

	// True if RestartBootstrap has been called at least once
	Restarted bool
}
