// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utilstest

import (
	"testing"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/graft/evm/message"
)

// ForEachCodec runs fn as a subtest for each supported codec (coreth and subnet-evm).
func ForEachCodec(t *testing.T, fn func(string, codec.Manager)) {
	t.Helper()
	codecs := map[string]codec.Manager{
		"coreth":     message.CorethCodec,
		"subnet-evm": message.SubnetEVMCodec,
	}
	for name, c := range codecs {
		t.Run(name, func(*testing.T) {
			fn(name, c)
		})
	}
}
