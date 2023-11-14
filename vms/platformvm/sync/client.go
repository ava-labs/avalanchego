// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	xsync "github.com/ava-labs/avalanchego/x/sync" // TODO how to alias this?
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
	Shutdown()
}

type ClientConfig struct {
	xsync.ManagerConfig
	Enabled bool
	// Called when syncing is done, where [err] is the result of syncing.
	// Called iff a summary is accepted, regardless of whether syncing succeeds.
	OnDone func(err error)
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
	lock sync.Mutex

	enabled       bool
	onDone        func(err error)
	managerConfig xsync.ManagerConfig

	metadataDB database.KeyValueReaderWriterDeleter

	// Set in acceptSyncSummary.
	manager *xsync.Manager

	// Set in acceptSyncSummary.
	// Calling [c.syncCancel] will stop syncing.
	syncCancel context.CancelFunc
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
// Sets [c.syncErr] and closes [c.syncDone] when syncing is done.
// Must only be called once.
func (c *Client) acceptSyncSummary(summary SyncSummary) (block.StateSyncMode, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.managerConfig.TargetRoot = summary.BlockRoot

	manager, err := xsync.NewManager(c.managerConfig)
	if err != nil {
		return 0, err
	}
	c.manager = manager

	ctx, cancel := context.WithCancel(context.Background())
	c.syncCancel = cancel

	go func() {
		err := c.manager.Start(ctx)
		c.onDone(err)
	}()

	return block.StateSyncStatic, nil
}

func (c *Client) Shutdown() {
	c.lock.Lock()
	defer c.lock.Unlock()

	if c.syncCancel != nil {
		c.syncCancel()
	}
}
