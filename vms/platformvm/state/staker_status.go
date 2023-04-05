// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

const (
	unmodified diffStakerStatus = iota
	added
	updated
	deleted
)

type diffStakerStatus uint8
