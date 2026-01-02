// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fx

// Factory returns an instance of a feature extension
type Factory interface {
	New() any
}
