// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

// MaxInt is defined rather than using [math.MaxInt] to support go versions
// < 1.17.
const MaxInt = int(^uint(0) >> 1)
