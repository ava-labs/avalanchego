// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package random

// Sampler allows the sampling of integers
type Sampler interface {
	Sample() int
	SampleReplace() int
	CanSample() bool
	Replace()
}
