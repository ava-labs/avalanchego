// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import (
	"errors"
)

var (
	ErrExceedsGasAllowance = errors.New("exceeds gas allowance")
	ErrWriteProtection     = errors.New("cannot modify in read only")
)
