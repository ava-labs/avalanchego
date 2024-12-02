package main

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/prometheus/client_golang/prometheus"
)

func getStateSummaryMsg(log logging.Logger, chainId ids.ID) (message.OutboundMessage, error) {
	mc, err := message.NewCreator(
		log,
		prometheus.NewRegistry(),
		constants.DefaultNetworkCompressionType,
		10*time.Second,
	)
	if err != nil {
		return nil, err
	}
	return mc.GetAcceptedFrontier(chainId, 1, time.Second*5)
}
