// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/units"
)

func TestParseGibberish(t *testing.T) {
	randomBytes := utils.RandomBytes(256 * units.KiB)
	_, err := Parse(randomBytes)
	require.Error(t, err)
}
