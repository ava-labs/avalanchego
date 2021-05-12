// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchlist

import (
	"github.com/ava-labs/avalanchego/ids"
)

type Benchable interface {
	Benched(chainID ids.ID, validatorID ids.ShortID)
	Unbenched(chainID ids.ID, validatorID ids.ShortID)
}

type NoBenchable struct{}

func (NoBenchable) Benched(ids.ID, ids.ShortID)   {}
func (NoBenchable) Unbenched(ids.ID, ids.ShortID) {}
