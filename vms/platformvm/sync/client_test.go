// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	xsync "github.com/ava-labs/avalanchego/x/sync"
)

func TestNewClient(t *testing.T) {
	var (
		require    = require.New(t)
		metadataDB = memdb.New()
		config     = ClientConfig{
			ManagerConfig: xsync.ManagerConfig{},
			Enabled:       true,
			OnDone:        func(error) {},
		}
		client = NewClient(config, metadataDB)
	)

	require.NotNil(client)
	require.NotNil(config, client.config)
	require.Equal(metadataDB, client.metadataDB)
}
