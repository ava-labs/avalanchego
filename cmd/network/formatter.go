// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/logging"
	proposerSummary "github.com/ava-labs/avalanchego/vms/proposervm/summary"
	evmMessage "github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	creator message.Creator
	_       QueryFormatter = cChainStateSummary{}
	_       QueryFormatter = pullQuery{}
)

func init() {
	var err error
	creator, err = message.NewCreator(logging.NoLog{}, prometheus.NewRegistry(), "", compression.TypeNone, 3*time.Second)
	if err != nil {
		panic(err)
	}
}

type QueryFormatter interface {
	CreateMessage() (message.OutboundMessage, message.Op, error)
	OutputHeaders() []string
	FormatOutput(result interface{}) ([]string, error)
}

type cChainStateSummary struct{}

func (c cChainStateSummary) CreateMessage() (message.OutboundMessage, message.Op, error) {
	chainID, err := ids.FromString("2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5")
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse chainID: %w", err)
	}

	msg, err := creator.GetStateSummaryFrontier(chainID, 0, 3*time.Second)
	if err != nil {
		return nil, 0, err
	}
	return msg, message.StateSummaryFrontierOp, nil
}

func (c cChainStateSummary) OutputHeaders() []string {
	return []string{"OuterStateSummaryID", "BlockNumber", "AtomicRoot", "Error"}
}

func (c cChainStateSummary) FormatOutput(result interface{}) ([]string, error) {
	switch res := result.(type) {
	case error:
		return []string{"", "", "", res.Error()}, nil
	default:
	}
	res := result.(*p2p.StateSummaryFrontier)
	proposerVMSummary, err := proposerSummary.Parse(res.Summary)
	if err != nil {
		return nil, err
	}

	parsedSummary, err := evmMessage.NewSyncSummaryFromBytes(proposerVMSummary.InnerSummaryBytes(), nil)
	if err != nil {
		return nil, err
	}
	return []string{
		proposerVMSummary.ID().String(),
		fmt.Sprintf("%d", parsedSummary.BlockNumber),
		ids.ID(parsedSummary.AtomicRoot).String(),
		"",
	}, nil
}

type pullQuery struct{}

func (p pullQuery) CreateMessage() (message.OutboundMessage, message.Op, error) {
	chainID, err := ids.FromString("11111111111111111111111111111111LpoYY")
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse chainID: %w", err)
	}
	blockID, err := ids.FromString("ZksmS6bf4HDvk2DtUqz5b3dNKhSYxw9D8gSW1YKFYtm4P519N")
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse P-Chain blockID: %w", err)
	}
	msg, err := creator.PullQuery(chainID, 99, 3*time.Second, blockID, 11310039, p2p.EngineType_ENGINE_TYPE_SNOWMAN)
	if err != nil {
		return nil, 0, err
	}
	return msg, message.ChitsOp, nil
}

func (p pullQuery) OutputHeaders() []string {
	return []string{"PreferredID", "PreferredIDAtHeight", "AcceptedID", "Error"}
}

func (p pullQuery) FormatOutput(result interface{}) ([]string, error) {
	switch res := result.(type) {
	case error:
		return []string{"", "", "", res.Error()}, nil
	default:
	}
	res := result.(*p2p.Chits)
	return []string{
		ids.ID(res.PreferredId).String(),
		ids.ID(res.PreferredIdAtHeight).String(),
		ids.ID(res.AcceptedId).String(),
		"",
	}, nil
}
