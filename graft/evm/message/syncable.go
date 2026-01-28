// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	errParseSyncableSummary = errors.New("cannot parse syncable summary")
	errComputeSummaryID     = errors.New("cannot compute summary ID")
)

type Syncable interface {
	block.StateSummary
	GetBlockHash() common.Hash
	GetBlockRoot() common.Hash
}

// SummaryProvider is an interface for providing state summaries.
type SummaryProvider interface {
	StateSummaryAtBlock(ethBlock *types.Block) (block.StateSummary, error)
}

// SyncableParser parses syncable summaries from bytes.
type SyncableParser interface {
	Parse(summaryBytes []byte, acceptImpl AcceptImplFn) (Syncable, error)
}

type AcceptImplFn func(Syncable) (block.StateSyncMode, error)

// ParseSyncableSummary unmarshals `summaryBytes` into `summary` and returns its ID.
func ParseSyncableSummary(c codec.Manager, summaryBytes []byte, summary any) (ids.ID, error) {
	if _, err := c.Unmarshal(summaryBytes, summary); err != nil {
		return ids.Empty, fmt.Errorf("%w: %w", errParseSyncableSummary, err)
	}

	summaryID, err := ids.ToID(crypto.Keccak256(summaryBytes))
	if err != nil {
		return ids.Empty, fmt.Errorf("%w: %w", errComputeSummaryID, err)
	}
	return summaryID, nil
}
