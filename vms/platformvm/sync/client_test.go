// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	xsync "github.com/ava-labs/avalanchego/x/sync"
)

func TestNewClient(t *testing.T) {
	var (
		require       = require.New(t)
		metadataDB    = memdb.New()
		managerConfig = xsync.ManagerConfig{
			SimultaneousWorkLimit: 1,
		}
		onDoneCalled bool
		onDone       = func(error) { onDoneCalled = true }
		config       = ClientConfig{
			ManagerConfig: managerConfig,
			Enabled:       true,
			OnDone:        onDone,
		}
		client = NewClient(config, metadataDB)
	)

	require.NotNil(client)
	require.NotNil(config, client.config)
	require.Equal(config.ManagerConfig, managerConfig)
	require.Equal(config.Enabled, client.config.Enabled)
	require.Equal(metadataDB, client.metadataDB)

	// Can't use reflect.Equal to test function equality
	// so do this instead.
	client.config.OnDone(nil)
	require.True(onDoneCalled)
}

func TestClientStateSyncEnabled(t *testing.T) {
	require := require.New(t)

	client := NewClient(ClientConfig{
		Enabled: true,
	}, memdb.New())

	enabled, err := client.StateSyncEnabled(context.Background())
	require.NoError(err)
	require.True(enabled)

	client.config.Enabled = false
	enabled, err = client.StateSyncEnabled(context.Background())
	require.NoError(err)
	require.False(enabled)
}

func TestClientParseStateSummary(t *testing.T) {
	require := require.New(t)

	client := NewClient(ClientConfig{}, memdb.New())

	var (
		blockID     = ids.GenerateTestID()
		blockNumber = uint64(1337)
		rootID      = ids.GenerateTestID()
	)
	summary, err := NewSyncSummary(blockID, blockNumber, rootID)
	require.NoError(err)
	summaryBytes := summary.Bytes()

	parsedSummary, err := client.ParseStateSummary(context.Background(), summary.Bytes())
	require.NoError(err)

	require.Equal(blockNumber, parsedSummary.Height())
	require.Equal(summaryBytes, parsedSummary.Bytes())

}
