// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package json

import (
	"github.com/ava-labs/avalanchego/utils/math"

	stdmath "math"
)

func SafeAdd(a, b Uint64) Uint64 {
	ret, err := math.Add64(uint64(a), uint64(b))
	if err != nil {
		return stdmath.MaxUint64
	}
	return Uint64(ret)
}
