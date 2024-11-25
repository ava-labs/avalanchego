package main

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"go.uber.org/zap"
)

var (
	_ message.OutboundMessage = &pingMessage{}
)

type pingMessage struct {
	Msg     string 
}

func (m *pingMessage) BypassThrottling() bool {
	return true
}

func (m *pingMessage) Op() message.Op {
	return message.PingOp
}

func (m *pingMessage) Bytes() []byte {
	return []byte(m.Msg)
}

func (m *pingMessage) BytesSavedCompression() int {
	return 0
}

func sendPingMsg(log logging.Logger, network network.Network, nodeIds set.Set[ids.NodeID]) {
	
	outboundMsg := &pingMessage{
		Msg: "ping",
	}
	config := common.SendConfig{
		NodeIDs: nodeIds,
	}
	subnetId, err := ids.FromString("11111111111111111111111111111111LpoYY")
	if err != nil {
		log.Fatal(
			"failed to create subnet ID",
			zap.Error(err),
		)
		return
	}

	allower := alwaysAllower{}

	log.Info("sending message to network")
	network.Send(
		outboundMsg,
		config,
		subnetId,
		allower,
	)
	
	log.Info("message sent to network")
}
