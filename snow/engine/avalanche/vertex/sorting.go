// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"bytes"
	"sort"

	"github.com/chain4travel/caminogo/utils"
	"github.com/chain4travel/caminogo/utils/hashing"
)

type sortHashOfData [][]byte

func (d sortHashOfData) Less(i, j int) bool {
	return bytes.Compare(
		hashing.ComputeHash256(d[i]),
		hashing.ComputeHash256(d[j]),
	) == -1
}
func (d sortHashOfData) Len() int      { return len(d) }
func (d sortHashOfData) Swap(i, j int) { d[j], d[i] = d[i], d[j] }

func SortHashOf(bytesSlice [][]byte) { sort.Sort(sortHashOfData(bytesSlice)) }
func IsSortedAndUniqueHashOf(bytesSlice [][]byte) bool {
	return utils.IsSortedAndUnique(sortHashOfData(bytesSlice))
}
