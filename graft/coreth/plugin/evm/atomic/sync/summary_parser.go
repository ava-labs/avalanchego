// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"fmt"

	"github.com/ava-labs/avalanchego/graft/evm/message"

	"github.com/ava-labs/libevm/crypto"

	"github.com/ava-labs/avalanchego/ids"
)

var _ message.SyncableParser = (*summaryParser)(nil)

type summaryParser struct{}

func NewSummaryParser() *summaryParser {
	return &summaryParser{}
}

func (*summaryParser) Parse(summaryBytes []byte, acceptImpl message.AcceptImplFn) (message.Syncable, error) {
	summary := Summary{}
	if _, err := message.Codec.Unmarshal(summaryBytes, &summary); err != nil {
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
