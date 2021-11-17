// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"bytes"

	"github.com/ava-labs/avalanchego/ids"
)

type validatorData struct {
	id     ids.ShortID
	weight uint64
}

type validatorsSlice []validatorData

func (d validatorsSlice) Len() int      { return len(d) }
func (d validatorsSlice) Swap(i, j int) { d[i], d[j] = d[j], d[i] }

func (d validatorsSlice) Less(i, j int) bool {
	iID := d[i].id
	jID := d[j].id
	return bytes.Compare(iID[:], jID[:]) == -1
}
