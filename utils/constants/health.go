// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import (
	"time"
)

const (
	// DefaultHealthCheckExecutionPeriod is the default time between
	// executions of a health check function
	DefaultHealthCheckExecutionPeriod = 1 * time.Minute

	// DefaultHealthCheckInitialDelay ...
	DefaultHealthCheckInitialDelay = 10 * time.Second

	// MinConnectedStake is the minimum percentage of the Primary Network's
	// that this node must be connected to to be considered healthy
	MinConnectedStake = float64(.80)
)
