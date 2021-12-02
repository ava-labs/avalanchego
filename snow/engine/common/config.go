// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"time"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
)

// Config wraps the common configurations that are needed by a Snow consensus
// engine
type Config struct {
	Ctx        *snow.ConsensusContext
	Validators validators.Set
	Beacons    validators.Set

	SampleK       int
	StartupAlpha  uint64
	Alpha         uint64
	Sender        Sender
	Bootstrapable Bootstrapable
	Subnet        Subnet
	Timer         Timer

	// Should Bootstrap be retried
	RetryBootstrap bool

	// Max number of times to retry bootstrap before warning the node operator
	RetryBootstrapWarnFrequency int

	// Max time to spend fetching a container and its ancestors when responding
	// to a GetAncestors
	MaxTimeGetAncestors time.Duration

	// Max number of containers in a multiput message sent by this node.
	MultiputMaxContainersSent int

	// This node will only consider the first [MultiputMaxContainersReceived]
	// containers in a multiput it receives.
	MultiputMaxContainersReceived int
}

// Context implements the Engine interface
func (c *Config) Context() *snow.ConsensusContext { return c.Ctx }

// IsBootstrapped returns true iff this chain is done bootstrapping
func (c *Config) IsBootstrapped() bool { return c.Ctx.IsBootstrapped() }
