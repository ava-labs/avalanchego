// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

const (
	unmodified diffValidatorStatus = iota
	added
	deleted
)

type diffValidatorStatus uint8
