// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

// PointerTo returns a pointer to the provided value.
//
// Go 1.26's new(expr) supersedes this helper. It lives here so that it is
// deleted along with the graft packages, which are its only remaining users.
func PointerTo[T any](v T) *T {
	return &v
}
