// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import "errors"

var (
	errOutOfRange = errors.New("out of range")
)

// Weighted ...
type Weighted interface {
	Initialize([]uint64) error
	Sample(uint64) (int, error)
}
