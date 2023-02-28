// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package status

import "github.com/ava-labs/avalanchego/snow"

func DoneBootstraping(state snow.State) bool {
	return state == snow.ExtendingFrontier || state == snow.SubnetSynced
}
