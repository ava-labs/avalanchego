// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package random_network

import (
	"github.com/ava-labs/simplex/common"
)

func NewProtocolMetadata(round, seq uint64, prev common.Digest) common.ProtocolMetadata {
	return common.ProtocolMetadata{
		Round: round,
		Seq:   seq,
		Prev:  prev,
	}
}
