// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

// Join merges the provided slices into a single slice.
//
// TODO: Use slices.Concat once the minimum go version is 1.22.
func Join[T any](slices ...[]T) []T {
	size := 0
	for _, s := range slices {
		size += len(s)
	}
	newSlice := make([]T, 0, size)
	for _, s := range slices {
		newSlice = append(newSlice, s...)
	}
	return newSlice
}
