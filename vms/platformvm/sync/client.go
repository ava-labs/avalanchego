// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"errors"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	xsync "github.com/ava-labs/avalanchego/x/sync" // TODO how to alias this?
)

var (
	stateSyncSummaryKey = []byte("stateSyncSummary")
	errShutdown         = errors.New("client has been shut down")
)

type ClientConfig struct {
	// [config.TargetRoot] will be set by the Client when a summary is accepted.
	xsync.ManagerConfig
	Enabled bool
	// Called when syncing is done, where [err] is the result of syncing.
	// Called iff acceptSyncSummary returns nil, regardless of whether syncing succeeds.
	OnDone func(err error)
}

func NewClient(
	config ClientConfig,
	metadataDB database.KeyValueReaderWriterDeleter,
) *Client {
	return &Client{
		config:     config,
		metadataDB: metadataDB,
	}
}

type Client struct {
	lock sync.Mutex

	config ClientConfig

	shutdown bool

	// Stores the ongoing sync summary ID.
	metadataDB database.KeyValueReaderWriterDeleter

	// Set in acceptSyncSummary.
	// Calling [c.syncCancel] will stop syncing.
	syncCancel context.CancelFunc
}

func (c *Client) StateSyncEnabled(context.Context) (bool, error) {
	return c.config.Enabled, nil
}

func (c *Client) GetOngoingSyncStateSummary(context.Context) (block.StateSummary, error) {
	summaryBytes, err := c.metadataDB.Get(stateSyncSummaryKey)
	if err != nil {
		return nil, err // includes the [database.ErrNotFound] case
	}

	return NewSyncSummaryFromBytes(summaryBytes, c.acceptSyncSummary)
}

func (c *Client) ParseStateSummary(_ context.Context, summaryBytes []byte) (block.StateSummary, error) {
	return NewSyncSummaryFromBytes(summaryBytes, c.acceptSyncSummary)
}

// Starts asynchronously syncing to the root in [summary].
// Populates [c.syncCancel].
// [c.onDone] is guaranteed to eventually be called iff this function returns nil.
// Must only be called once.
func (c *Client) acceptSyncSummary(summary SyncSummary) (block.StateSyncMode, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if c.shutdown {
		return 0, errShutdown
	}

	c.config.TargetRoot = summary.BlockRoot

	if err := c.metadataDB.Put(stateSyncSummaryKey, summary.Bytes()); err != nil {
		return 0, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	c.syncCancel = cancel

	manager, err := xsync.NewManager(c.config.ManagerConfig)
	if err != nil {
		return 0, err
	}

	go func() {
		var err error
		if err = manager.Start(ctx); err == nil {
			err = manager.Wait(ctx)
		}

		if err == nil {
			// TODO what to do with this error?
			_ = c.metadataDB.Delete(stateSyncSummaryKey)
		}

		c.config.OnDone(err)
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
