// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/x/sync"
)

var (
	_ block.StateSyncableVM = (*Client)(nil)

	stateSyncSummaryKey = []byte("stateSyncSummary")
)

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
		syncErrChan:   make(chan error),
	}
}

type Client struct {
	enabled       bool
	manager       *sync.Manager // Set in acceptSyncSummary
	managerConfig sync.ManagerConfig

	metadataDB database.KeyValueReaderWriterDeleter

	syncCancel  context.CancelFunc // Set in acceptSyncSummary
	syncErrChan chan error
}

func (c *Client) StateSyncEnabled(context.Context) (bool, error) {
	return c.enabled, nil
}

func (c *Client) GetOngoingSyncStateSummary(context.Context) (block.StateSummary, error) {
	summaryBytes, err := c.metadataDB.Get(stateSyncSummaryKey)
	if err != nil {
		return nil, err // includes the [database.ErrNotFound] case
	}

	summary, err := NewSyncSummaryFromBytes(summaryBytes, c.acceptSyncSummary)
	if err != nil {
		return nil, fmt.Errorf("failed to parse saved state sync summary to SyncSummary: %w", err)
	}

	return summary, nil
}

func (c *Client) GetLastStateSummary(context.Context) (block.StateSummary, error) {
	return nil, errors.New("TODO")
}

func (c *Client) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
	return NewSyncSummaryFromBytes(summaryBytes, c.acceptSyncSummary)
}

func (c *Client) GetStateSummary(ctx context.Context, summaryHeight uint64) (block.StateSummary, error) {
	return nil, errors.New("TODO")
}

// acceptSyncSummary returns true if sync will be performed and launches the state sync process
// in a goroutine.
func (c *Client) acceptSyncSummary(proposedSummary SyncSummary) (block.StateSyncMode, error) {
	c.managerConfig.TargetRoot = proposedSummary.BlockRoot

	manager, err := sync.NewManager(c.managerConfig)
	if err != nil {
		return 0, err
	}
	c.manager = manager

	ctx, cancel := context.WithCancel(context.Background())
	c.syncCancel = cancel

	go func() {
		c.syncErrChan <- c.manager.Start(ctx)
	}()

	return block.StateSyncStatic, nil
}
