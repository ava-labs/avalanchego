// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metadata

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"slices"
	"sync"

	"github.com/ava-labs/simplex/avalanchego"
	"github.com/ava-labs/simplex/common"
	"go.uber.org/zap"
)

type AuxInfoHistory struct {
	Data            [][]byte
	LastSeq         uint64
	OldestVersionID common.VersionID // oldest version id in the histories data, or DefaultVersionID if no history
}

func (aih *AuxInfoHistory) LastHistoryDigest() common.Digest {
	if len(aih.Data) == 0 {
		return common.Digest{}
	}
	last := aih.Data[len(aih.Data)-1]
	return sha256.Sum256(last)
}

// GetAuxiliaryHistory traverses backwards starting from the given block and returns the AuxInfoHistory of all blocks in the chain.
// It returns the collected auxiliary info ordered from oldest to newest, the sequence of the newest block it was collected from,
// and the version ID of the oldest non-empty auxiliary info entry (or defaultVersionID if there was none).
// blockSeq must be the sequence of the given block.
func GetAuxiliaryHistory(block *StateMachineBlock, blockSeq uint64, getBlock BlockRetriever, defaultVersionID common.VersionID) (AuxInfoHistory, error) {
	var lastSeq *uint64
	var history [][]byte
	var versionID = defaultVersionID

	// We traverse the chain of blocks backwards in the following manner:
	// (1) Every block that doesn't have an AuxiliaryInfoBatch, its parents also do not have one.
	// (2) Every block that has an AuxiliaryInfoBatch, its descendants also have one.
	// (3) A block's AuxiliaryInfoBatch may have no entries, but its PrevAuxInfoSeq field must point
	// to a block whose AuxiliaryInfoBatch isn't nil and has non-empty entries.
	// (4) When a block with an empty batch is built on a parent block that has an AuxiliaryInfoBatch,
	// if its parent block's batch has non-empty entries, then the block's PrevAuxInfoSeq points to its parent block.
	// Else, its parent block's batch is also empty, then the block's PrevAuxInfoSeq is inherited from its parent block's PrevAuxInfoSeq.

	batch := block.Metadata.AuxiliaryInfoBatch
	currentSeq := blockSeq
	for batch != nil {
		// Entries within a batch are ordered oldest to newest, so iterate newest-first:
		// the full history is reversed once traversal completes.
		for i := len(batch.data) - 1; i >= 0; i-- {
			entry := batch.data[i]
			if len(entry.Data) == 0 {
				continue
			}
			history = append(history, entry.Data)
			if lastSeq == nil {
				lastSeq = new(uint64)
				*lastSeq = currentSeq
			}
			versionID = entry.Version
		}
		if batch.PrevAuxInfoSeq == 0 {
			// This is the first auxiliary info of the epoch, we can stop traversing back.
			break
		}
		currentSeq = batch.PrevAuxInfoSeq
		prevBlock, _, err := getBlock(batch.PrevAuxInfoSeq, common.Digest{})
		if err != nil {
			return AuxInfoHistory{}, fmt.Errorf("%w: at sequence %d: %w", errAuxInfoBlockRetrieval, batch.PrevAuxInfoSeq, err)
		}
		batch = prevBlock.Metadata.AuxiliaryInfoBatch
	}

	if lastSeq == nil {
		lastSeq = new(uint64)
		*lastSeq = 0
	}

	// Reverse so the history is ordered from oldest to newest.
	slices.Reverse(history)
	return AuxInfoHistory{Data: history, LastSeq: *lastSeq, OldestVersionID: versionID}, nil
}

// auxInfoStore stores auxiliary info that has been received but not yet included in blocks
type auxInfoStore struct {
	app    AuxiliaryInfoGenVerifier
	logger common.Logger

	lock     sync.Mutex
	sentInfo map[avalanchego.NodeID]common.AuxiliaryInfo
}

func newAuxInfoStore(app AuxiliaryInfoGenVerifier, logger common.Logger) *auxInfoStore {
	return &auxInfoStore{
		app:      app,
		sentInfo: make(map[avalanchego.NodeID]common.AuxiliaryInfo),
		logger:   logger,
	}
}

func (a *auxInfoStore) HandleAuxiliaryMessage(info common.AuxiliaryInfo, from avalanchego.NodeID) {
	a.lock.Lock()
	defer a.lock.Unlock()

	// just set the nodes Auxiliary info to the most recent one they sent
	a.sentInfo[from] = info
}

// collectAuxInfo returns the stored entries that are legal appends to the given history.
func (a *auxInfoStore) collectAuxInfo(history AuxInfoHistory, validators NodeBLSMappings) []common.AuxiliaryInfo {
	a.lock.Lock()
	defer a.lock.Unlock()

	// Iterate in node ID order so the returned entries are deterministic.
	nodeIDs := make([]avalanchego.NodeID, 0, len(a.sentInfo))
	for nodeID := range a.sentInfo {
		nodeIDs = append(nodeIDs, nodeID)
	}
	slices.SortFunc(nodeIDs, func(x, y avalanchego.NodeID) int {
		return bytes.Compare(x[:], y[:])
	})

	var legalAppends []common.AuxiliaryInfo
	legalHistory := append([][]byte{}, history.Data...)

	for _, nodeID := range nodeIDs {
		info := a.sentInfo[nodeID]
		if history.OldestVersionID != info.Version {
			continue // keep consistent versions throughout epoch transition
		}

		if err := a.app.IsLegalAppend(info.Version, validators, legalHistory, info.Data); err != nil {
			// we don't remove this info from the mempool. maybe it can be added in a different block
			a.logger.Debug("Could not append auxiliary info when collecting", zap.Uint32("Version", uint32(info.Version)))
			continue
		}

		legalAppends = append(legalAppends, info)
		legalHistory = append(legalHistory, info.Data)
	}

	return legalAppends
}
