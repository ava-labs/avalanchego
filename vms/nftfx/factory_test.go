// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nftfx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestFactory(t *testing.T) {
	require := require.New(t)

	factory := Factory{}
	fx, err := factory.New(logging.NoLog{})
	require.NoError(err)
	require.NotNil(fx)
}
