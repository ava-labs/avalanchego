// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import "errors"

const (
	Initializing State = iota
	StateSyncing
	Bootstrapping
	ExtendingFrontier
	SubnetSynced
)

var ErrUnknownState = errors.New("unknown state")

type State uint8

func (st State) String() string {
	switch st {
	case Initializing:
		return "Initializing state"
	case StateSyncing:
		return "State syncing state"
	case Bootstrapping:
		return "Bootstrapping state"
	case ExtendingFrontier:
		return "Extending frontier state"
	case SubnetSynced:
		return "Subnet synced state"
	default:
		return "Unknown state"
	}
}
