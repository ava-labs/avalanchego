// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extension

import (
	"errors"

	"github.com/ava-labs/avalanchego/utils/timer/mockable"

	"github.com/ava-labs/subnet-evm/plugin/evm/message"
	"github.com/ava-labs/subnet-evm/plugin/evm/sync"
)

var (
	errNilConfig              = errors.New("nil extension config")
	errNilSyncSummaryProvider = errors.New("nil sync summary provider")
	errNilSyncableParser      = errors.New("nil syncable parser")
	errNilClock               = errors.New("nil clock")
)

// Config is the configuration for the VM extension
type Config struct {
	// SyncSummaryProvider is the sync summary provider to use
	// for the VM to be used in syncer.
	// It's required and should be non-nil
	SyncSummaryProvider sync.SummaryProvider
	// SyncableParser is to parse summary messages from the network.
	// It's required and should be non-nil
	SyncableParser message.SyncableParser
	// Clock is the clock to use for time related operations.
	// It's optional and can be nil
	Clock *mockable.Clock
}

func (c *Config) Validate() error {
	if c == nil {
		return errNilConfig
	}
	if c.SyncSummaryProvider == nil {
		return errNilSyncSummaryProvider
	}
	if c.SyncableParser == nil {
		return errNilSyncableParser
	}
	if c.Clock == nil {
		return errNilClock
	}
	return nil
}
