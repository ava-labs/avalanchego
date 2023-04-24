// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
)

func TestParseGibberish(t *testing.T) {
	randomBytes := []byte{0, 1, 2, 3, 4, 5}
	_, err := Parse(randomBytes)
	require.ErrorIs(t, err, codec.ErrUnknownVersion)
}
