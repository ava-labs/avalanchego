// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import "errors"

type State uint8

var ErrUnknownState = errors.New("unknown state")

const (
	Initializing = iota
	StateSyncing
	Bootstrapping
	NormalOp
)

func (st State) String() string {
	switch st {
	case Initializing:
		return "Initializing state"
	case StateSyncing:
		return "State syncing state"
	case Bootstrapping:
		return "Bootstrapping state"
	case NormalOp:
		return "Normal operations state"
	default:
		return "Unknown state"
	}
}
