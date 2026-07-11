// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	chainStatePrefix  = []byte("chain")
	blockStatePrefix  = []byte("block")
	heightIndexPrefix = []byte("height")

	// Unprefixed key at the state root recording the unit the stored blocks'
	// timestamps were written with.
	timestampUnitKey = []byte("timestamp_unit")

	errTimestampUnitMismatch = errors.New("proposervm database timestamp unit does not match proposerMillisecondTimestamps in the subnet config")
	errMillisOnExistingChain = errors.New("proposerMillisecondTimestamps must be set from genesis; this chain already has blocks with second-granular timestamps")
)

type State interface {
	ChainState
	BlockState
	HeightIndex

	// VerifyTimestampUnit records the chain's timestamp unit on first use and
	// errors if the database was written with a different unit, so a node with
	// a lost or divergent subnet config fails at startup instead of silently
	// misparsing every stored block.
	VerifyTimestampUnit(millisecondTimestamps bool) error
}

type state struct {
	ChainState
	BlockState
	HeightIndex

	db *versiondb.Database
}

func (s *state) VerifyTimestampUnit(millisecondTimestamps bool) error {
	unit := []byte{0}
	if millisecondTimestamps {
		unit = []byte{1}
	}

	stored, err := s.db.Get(timestampUnitKey)
	switch {
	case errors.Is(err, database.ErrNotFound):
		// First run that tracks the unit. Blocks written before tracking are
		// always second-granular, so enabling millisecond timestamps on a chain
		// that already has history is rejected rather than misread.
		if millisecondTimestamps {
			if _, err := s.GetLastAccepted(); err == nil {
				return errMillisOnExistingChain
			} else if !errors.Is(err, database.ErrNotFound) {
				return err
			}
		}
		return s.db.Put(timestampUnitKey, unit)
	case err != nil:
		return err
	case !bytes.Equal(stored, unit):
		return fmt.Errorf("%w: database has %s, config requests %s",
			errTimestampUnitMismatch, timestampUnitName(stored), timestampUnitName(unit))
	}
	return nil
}

func timestampUnitName(unit []byte) string {
	if bytes.Equal(unit, []byte{1}) {
		return "milliseconds"
	}
	return "seconds"
}

func New(db *versiondb.Database, getInnerBytes func(ids.ID) ([]byte, error), millisecondTimestamps bool, log logging.Logger) State {
	chainDB := prefixdb.New(chainStatePrefix, db)
	blockDB := prefixdb.New(blockStatePrefix, db)
	heightDB := prefixdb.New(heightIndexPrefix, db)

	return &state{
		ChainState:  NewChainState(chainDB),
		BlockState:  NewBlockState(blockDB, getInnerBytes, millisecondTimestamps, log),
		HeightIndex: NewHeightIndex(heightDB, db),
		db:          db,
	}
}

func NewMetered(db *versiondb.Database, namespace string, metrics prometheus.Registerer, getInnerBytes func(ids.ID) ([]byte, error), millisecondTimestamps bool, log logging.Logger) (State, error) {
	chainDB := prefixdb.New(chainStatePrefix, db)
	blockDB := prefixdb.New(blockStatePrefix, db)
	heightDB := prefixdb.New(heightIndexPrefix, db)

	blockState, err := NewMeteredBlockState(blockDB, namespace, metrics, getInnerBytes, millisecondTimestamps, log)
	if err != nil {
		return nil, err
	}

	return &state{
		ChainState:  NewChainState(chainDB),
		BlockState:  blockState,
		HeightIndex: NewHeightIndex(heightDB, db),
		db:          db,
	}, nil
}
