// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// DB persists warp message events derived from logs emitted by the warp
// precompile.
type DB struct {
	db database.Database
}

// NewDB creates a new DB.
func NewDB(db database.Database) *DB {
	return &DB{db: db}
}

// Add writes a warp message to the DB.
func (db *DB) Add(unsignedMsg *warp.UnsignedMessage) error {
	msgID := unsignedMsg.ID()

	if err := db.db.Put(msgID[:], unsignedMsg.Bytes()); err != nil {
		return fmt.Errorf("failed to put warp message in db: %w", err)
	}

	return nil
}

// Get gets a warp message from the DB.
func (db *DB) Get(msgID ids.ID) (*warp.UnsignedMessage, error) {
	unsignedMessageBytes, err := db.db.Get(msgID[:])
	if err != nil {
		return nil, err
	}

	unsignedMessage, err := warp.ParseUnsignedMessage(unsignedMessageBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse unsigned message %s: %w", msgID.String(), err)
	}

	return unsignedMessage, nil
}
