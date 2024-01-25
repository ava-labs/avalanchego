// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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
	"github.com/spf13/viper"
)

func createMessage(v *viper.Viper) (message.OutboundMessage, message.Op, error) {
	chainID, err := ids.FromString("2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5")
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse chainID: %w", err)
	}
	creator, err := message.NewCreator(logging.NoLog{}, prometheus.NewRegistry(), "", compression.TypeNone, 3*time.Second)
	if err != nil {
		return nil, 0, err
	}

	msg, err := creator.GetStateSummaryFrontier(chainID, 0, 3*time.Second)
	if err != nil {
		return nil, 0, err
	}
	return msg, message.StateSummaryFrontierOp, nil
}

func getMessageOutputHeaders() []string {
	return []string{"OuterStateSummaryID", "BlockNumber", "AtomicRoot", "Error"}
}

func formatMessageOutput(result interface{}) ([]string, error) {
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
