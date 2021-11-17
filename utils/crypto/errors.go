// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import (
	"errors"
)

var (
	errInvalidSigLen = errors.New("invalid signature length")
	errMutatedSig    = errors.New("signature was mutated from its original format")
)
