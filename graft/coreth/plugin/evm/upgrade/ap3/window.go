// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// AP3 defines the dynamic fee window used after the Apricot Phase 3 upgrade.
package ap3

import "github.com/ava-labs/avalanchego/utils/wrappers"

const (
	// WindowLen is the number of seconds of gas consumption to track.
	WindowLen = 10

	// WindowSize is the number of bytes that are used to encode the window.
	WindowSize = wrappers.LongLen * WindowLen
)
