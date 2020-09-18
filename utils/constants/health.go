// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
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
)
