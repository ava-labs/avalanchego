// Copyright (C) 20192022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

// type sortByHashData [][]byte

// func (d sortByHashData) Less(i, j int) bool {
// 	return bytes.Compare(
// 		hashing.ComputeHash256(d[i]),
// 		hashing.ComputeHash256(d[j]),
// 	) == -1
// }
// func (d sortByHashData) Len() int      { return len(d) }
// func (d sortByHashData) Swap(i, j int) { d[j], d[i] = d[i], d[j] }

// func SortByHash(s [][]byte) { sort.Sort(sortByHashData(s)) }

// func IsSortedAndUniqueByHash(s [][]byte) bool {
// 	return utils.IsSortedAndUnique(sortByHashData(s))
// }
