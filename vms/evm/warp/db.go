// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// DB stores and retrieves warp messages from the underlying database.
type DB struct {
	db database.Database
}

// NewDB creates a new warp message database.
func NewDB(db database.Database) *DB {
	return &DB{
		db: db,
	}
}

// Add stores a warp message in the database and cache.
func (d *DB) Add(unsignedMsg *warp.UnsignedMessage) error {
	msgID := unsignedMsg.ID()

	if err := d.db.Put(msgID[:], unsignedMsg.Bytes()); err != nil {
		return fmt.Errorf("failed to put warp message in db: %w", err)
	}

	return nil
}

// Get retrieves a warp message for the given msgID from the database.
func (d *DB) Get(msgID ids.ID) (*warp.UnsignedMessage, error) {
	unsignedMessageBytes, err := d.db.Get(msgID[:])
	if err != nil {
		return nil, err
	}

	unsignedMessage, err := warp.ParseUnsignedMessage(unsignedMessageBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse unsigned message %s: %w", msgID.String(), err)
	}

	return unsignedMessage, nil
}
