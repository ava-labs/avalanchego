// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/coreth/plugin/evm/message"
)

var (
	_ block.StateSyncableVM = (*Client)(nil)

	stateSyncSummaryKey = []byte("stateSyncSummary")
)

type Client struct {
	enabled    bool // TODO set
	metadataDB database.KeyValueReaderWriterDeleter
}

func (c *Client) StateSyncEnabled(context.Context) (bool, error) {
	return c.enabled, nil
}

func (c *Client) GetOngoingSyncStateSummary(context.Context) (block.StateSummary, error) {
	summaryBytes, err := c.metadataDB.Get(stateSyncSummaryKey)
	if err != nil {
		return nil, err // includes the [database.ErrNotFound] case
	}

	summary, err := message.NewSyncSummaryFromBytes(summaryBytes, nil /*c.acceptSyncSummary TODO uncomment*/)
	if err != nil {
		return nil, fmt.Errorf("failed to parse saved state sync summary to SyncSummary: %w", err)
	}
	// c.resumableSummary = summary TODO uncomment
	return summary, nil
}

func (c *Client) GetLastStateSummary(context.Context) (block.StateSummary, error) {
	return nil, errors.New("TODO")
}

func (c *Client) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
	return nil, errors.New("TODO")
}

func (c *Client) GetStateSummary(ctx context.Context, summaryHeight uint64) (block.StateSummary, error) {
	return nil, errors.New("TODO")
}
