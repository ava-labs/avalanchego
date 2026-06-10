package statesync

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
)

const syncCommitInterval = 4096

var _ adaptor.SummaryProperties = (*Summary)(nil)

//go:generate go run github.com/StephenButtolph/canoto/canoto $GOFILE

// Summary is a canoto-encoded state sync summary. It identifies the block at
// which a syncing node should begin fetching state, along with the state root
// that the synced state must hash to.
//
//nolint:revive // struct-tag: canoto allows unexported fields
type Summary struct {
	height    uint64      `canoto:"uint,1"`
	blockHash common.Hash `canoto:"fixed bytes,2"`

	canotoData canotoData_Summary
}

// ParseStateSummary implements [adaptor.StateSyncable].
func (*SummaryHandler) ParseStateSummary(_ context.Context, summaryBytes []byte) (*Summary, error) {
	s := new(Summary)
	if err := s.UnmarshalCanoto(summaryBytes); err != nil {
		return nil, fmt.Errorf("parsing state summary: %w", err)
	}
	return s, nil
}

// Bytes implements [adaptor.SummaryProperties].
func (s *Summary) Bytes() []byte {
	return s.MarshalCanoto()
}

// Height implements [adaptor.SummaryProperties].
func (s *Summary) Height() uint64 {
	return s.height
}

// ID implements [adaptor.SummaryProperties].
func (s *Summary) ID() ids.ID {
	return ids.ID(s.blockHash)
}
