// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import "errors"

var ErrUnknownStakerStatus = errors.New("unknown staker status")

const (
	unmodified diffStakerStatus = iota
	added
	updated
	deleted
)

type diffStakerStatus uint8
