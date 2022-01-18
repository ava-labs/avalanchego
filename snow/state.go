// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

type State uint8

const (
	Unknown State = iota
	Bootstrapping
	NormalOp
)

func (st State) String() string {
	switch st {
	case Bootstrapping:
		return "Bootstrapping state"
	case NormalOp:
		return "Normal operations state"
	default:
		// State.Unknown treated as default
		return "Unknown state"
	}
}
