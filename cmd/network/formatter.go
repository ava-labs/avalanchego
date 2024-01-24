// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ethereum/go-ethereum/common"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/viper"
)

func createMessage(v *viper.Viper) (message.OutboundMessage, message.Op, error) {
	chainIDStr := v.GetString(ChainIDKey)
	chainID, err := ids.FromString(chainIDStr)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse chainID: %w", err)
	}
	creator, err := message.NewCreator(logging.NoLog{}, prometheus.NewRegistry(), "", compression.TypeNone, v.GetDuration(DeadlineKey))
	if err != nil {
		return nil, 0, err
	}
	deadline := v.GetDuration(DeadlineKey)

	// This blockID is the hardcoded blockID accepted on the C-Chain at the given height.
	blockIDBytes := common.Hex2Bytes("ca6fe4a31c0745c84a953f5d942013c2eb16f8f03d4e5b81b6e627fafbffc13e")
	blockID, err := ids.ToID(blockIDBytes)
	if err != nil {
		return nil, 0, err
	}
	blockHeight := uint64(39896056)
	outboundMsg, err := creator.PullQuery(
		chainID,
		99,
		deadline,
		blockID,
		blockHeight,
		p2p.EngineType_ENGINE_TYPE_SNOWMAN,
	)
	if err != nil {
		return nil, 0, err
	}
	return outboundMsg, message.ChitsOp, nil
}

func getMessageOutputHeaders() []string {
	return []string{"ChainID", "RequestID", "PreferredID", "AcceptedID"}
}

func formatMessageOutput(msg fmt.Stringer) []string {
	chits := msg.(*p2p.Chits)
	return []string{
		ids.ID(chits.ChainId).String(),
		fmt.Sprintf("%d", chits.RequestId),
		ids.ID(chits.PreferredId).String(),
		ids.ID(chits.AcceptedId).String(),
	}
}
