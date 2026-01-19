// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"

	"github.com/ava-labs/libevm/crypto"

	"github.com/ava-labs/avalanchego/ids"
)

type BlockSyncSummaryParser struct{}

func NewBlockSyncSummaryParser() *BlockSyncSummaryParser {
	return &BlockSyncSummaryParser{}
}

func (*BlockSyncSummaryParser) Parse(summaryBytes []byte, acceptImpl AcceptImplFn) (Syncable, error) {
	summary := BlockSyncSummary{}
	if _, err := Codec.Unmarshal(summaryBytes, &summary); err != nil {
		return nil, fmt.Errorf("failed to parse syncable summary: %w", err)
	}

	summary.bytes = summaryBytes
	summaryID, err := ids.ToID(crypto.Keccak256(summaryBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to compute summary ID: %w", err)
	}
	summary.summaryID = summaryID
	summary.acceptImpl = acceptImpl
	return &summary, nil
}

