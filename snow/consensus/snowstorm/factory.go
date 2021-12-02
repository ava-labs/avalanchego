// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

// Factory returns new instances of Consensus
type Factory interface {
	New() Consensus
}
