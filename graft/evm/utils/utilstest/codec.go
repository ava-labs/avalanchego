// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utilstest

import (
	"testing"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/graft/evm/message"
)

func ForEachCodec(t *testing.T, fn func(name string, c codec.Manager)) {
	t.Helper()
	codecs := map[string]codec.Manager{
		"coreth":     message.CorethCodec,
		"subnet-evm": message.SubnetEVMCodec,
	}
	for name, c := range codecs {
		t.Run(name, func(_ *testing.T) {
			fn(name, c)
		})
	}
}
