// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"github.com/ava-labs/avalanchego/graft/evm/message"
)

var _ message.SyncableParser = (*summaryParser)(nil)

type summaryParser struct{}

func NewSummaryParser() *summaryParser {
	return &summaryParser{}
}
