// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/x/sync"
)

var (
	_ ClientIntf = (*Client)(nil)

	stateSyncSummaryKey = []byte("stateSyncSummary")
)

// TODO rename
type ClientIntf interface {
	StateSyncEnabled(context.Context) (bool, error)
	GetOngoingSyncStateSummary(context.Context) (block.StateSummary, error)
	ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error)
}

type ClientConfig struct {
	sync.ManagerConfig
	Enabled bool
}

// [config.TargetRoot] will be set when a summary is accepted.
// It doesn't need to be set here.
func NewClient(
	config ClientConfig,
	metadataDB database.KeyValueReaderWriterDeleter,
) *Client {
	return &Client{
		enabled:       config.Enabled,
		managerConfig: config.ManagerConfig,
	}
}

type Client struct {
	enabled       bool
	manager       *sync.Manager // Set in acceptSyncSummary
	managerConfig sync.ManagerConfig

	metadataDB database.KeyValueReaderWriterDeleter

	// Set in acceptSyncSummary.
	// Calling [c.syncCancel] will stop syncing.
	syncCancel context.CancelFunc
	// Set when syncing is done.
	syncErr error
}

func (c *Client) StateSyncEnabled(context.Context) (bool, error) {
	return c.enabled, nil
}

func (c *Client) GetOngoingSyncStateSummary(context.Context) (block.StateSummary, error) {
	summaryBytes, err := c.metadataDB.Get(stateSyncSummaryKey)
	if err != nil {
		return nil, err // includes the [database.ErrNotFound] case
	}

	return NewSyncSummaryFromBytes(summaryBytes, c.acceptSyncSummary)
}

func (c *Client) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
	return NewSyncSummaryFromBytes(summaryBytes, c.acceptSyncSummary)
}

// Aynschronously starts syncing to the root in [summary].
// Populates [c.manager] and [c.syncCancel].
// Sets [c.syncErr] when syncing is done.
func (c *Client) acceptSyncSummary(summary SyncSummary) (block.StateSyncMode, error) {
	c.managerConfig.TargetRoot = summary.BlockRoot

	manager, err := sync.NewManager(c.managerConfig)
	if err != nil {
		return 0, err
	}
	c.manager = manager

	ctx, cancel := context.WithCancel(context.Background())
	c.syncCancel = cancel

	go func() {
		c.syncErr = c.manager.Start(ctx)

		// TODO initialize the VM with the state on disk.

		// TODO send message to engine that syncing is done.
	}()

	return block.StateSyncStatic, nil
}
