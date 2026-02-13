// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import ethtypes "github.com/ava-labs/libevm/core/types"

// EmptyExtDataHash is the known hash of empty extdata bytes.
var EmptyExtDataHash = ethtypes.RLPHash([]byte(nil))
