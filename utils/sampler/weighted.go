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
	StartSearch(uint64) error
	ContinueSearch() (int, bool)
}
