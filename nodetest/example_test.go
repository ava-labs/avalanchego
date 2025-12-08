// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nodetest

import (
	"testing"
	"github.com/ava-labs/avalanchego/node"
	nodeconfig "github.com/ava-labs/avalanchego/config/node"
	"github.com/stretchr/testify/require"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestFoo(t *testing.T) {
	n, err := node.New(
		&nodeconfig.Config{},
		logging.NewFactory(logging.Config{}),
		logging.NoLog{},
	)
	require.NoError(t, err)

	n.Shutdown(1)
}
