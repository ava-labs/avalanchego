// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcsigner

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/blstest"
)

func BenchmarkSign(b *testing.B) {
	signer := NewRPCSigner(require.New(b))
	for _, messageSize := range blstest.BenchmarkSizes {
		b.Run(strconv.Itoa(messageSize), func(b *testing.B) {
			message := utils.RandomBytes(messageSize)

			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				_, _ = signer.Sign(message)
			}
		})
	}
}
