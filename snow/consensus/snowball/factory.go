// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import "github.com/ava-labs/avalanchego/ids"

// Factory returns new instances of Consensus
type Factory interface {
	New(params Parameters, choice ids.ID) Consensus
}
