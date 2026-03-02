// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

var (
	initializedKey = []byte{}
	blockPrefix    = []byte{0x00}
	addressPrefix  = []byte{0x01}
	chainPrefix    = []byte{0x02}
	messagePrefix  = []byte{0x03}
)

func Flatten[T any](slices ...[]T) []T {
	var size int
	for _, slice := range slices {
		size += len(slice)
	}

	result := make([]T, 0, size)
	for _, slice := range slices {
		result = append(result, slice...)
	}
	return result
}
