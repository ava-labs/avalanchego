// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanche-go/database"
	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/snow/consensus/snowman"
	"github.com/ava-labs/avalanche-go/vms/components/state"
)

var errWrongType = errors.New("got unexpected type from database")

// state.Get(Db, IDTypeID, lastAcceptedID) == ID of last accepted block
var lastAcceptedID = ids.NewID([32]byte{'l', 'a', 's', 't'})

// SnowmanState is a wrapper around state.State
// In additions to the methods exposed by state.State,
// SnowmanState exposes a few methods needed for managing
// state in a snowman vm
type SnowmanState interface {
	state.State
	GetBlock(database.Database, ids.ID) (snowman.Block, error)
	PutBlock(database.Database, snowman.Block) error
	GetLastAccepted(database.Database) (ids.ID, error)
	PutLastAccepted(database.Database, ids.ID) error
}

// implements SnowmanState
type snowmanState struct {
	state.State
}

// GetBlock gets the block with ID [ID] from [db]
func (s *snowmanState) GetBlock(db database.Database, ID ids.ID) (snowman.Block, error) {
	blockInterface, err := s.Get(db, state.BlockTypeID, ID)
	if err != nil {
		return nil, err
	}

	if block, ok := blockInterface.(snowman.Block); ok {
		return block, nil
	}
	return nil, errWrongType
}

// PutBlock puts [block] in [db]
func (s *snowmanState) PutBlock(db database.Database, block snowman.Block) error {
	return s.Put(db, state.BlockTypeID, block.ID(), block)
}

// GetLastAccepted returns the ID of the last accepted block in [db]
func (s *snowmanState) GetLastAccepted(db database.Database) (ids.ID, error) {
	lastAccepted, err := s.GetID(db, lastAcceptedID)
	if err != nil {
		return ids.ID{}, err
	}
	return lastAccepted, nil
}

// PutLastAccepted sets the ID of the last accepted block in [db] to [lastAccepted]
func (s *snowmanState) PutLastAccepted(db database.Database, lastAccepted ids.ID) error {
	return s.PutID(db, lastAcceptedID, lastAccepted)
}

// NewSnowmanState returns a new SnowmanState
func NewSnowmanState(unmarshalBlockFunc func([]byte) (snowman.Block, error)) (SnowmanState, error) {
	rawState, err := state.NewState()
	if err != nil {
		return nil, fmt.Errorf("error creating new state: %w", err)
	}
	snowmanState := &snowmanState{State: rawState}
	return snowmanState, rawState.RegisterType(
		state.BlockTypeID,
		func(b interface{}) ([]byte, error) {
			if block, ok := b.(snowman.Block); ok {
				return block.Bytes(), nil
			}
			return nil, errors.New("expected snowman.Block but got unexpected type")
		},
		func(bytes []byte) (interface{}, error) {
			return unmarshalBlockFunc(bytes)
		},
	)
}
