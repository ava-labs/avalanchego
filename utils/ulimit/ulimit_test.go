// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ulimit

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// Test_SetDefault performs sanity checks for the os default.
func Test_SetDefault(t *testing.T) {
	require := require.New(t)
	err := Set(DefaultFDLimit, logging.NoLog{})
	require.NoErrorf(err, "default fd-limit failed %v", err)
}
