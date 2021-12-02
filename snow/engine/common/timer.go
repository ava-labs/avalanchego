// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"time"
)

// Timer describes the standard interface for specifying a timeout
type Timer interface {
	// RegisterTimeout specifies how much time to delay the next timeout message
	// by. If the subnet has been bootstrapped, the timeout will fire
	// immediately.
	RegisterTimeout(time.Duration)
}
