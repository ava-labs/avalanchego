// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	// EthMsgSoftCapSize is the ideal size of encoded transaction bytes we send in
	// any [EthTxsGossip] or [AtomicTxGossip] message. We do not limit inbound messages to
	// this size, however. Max inbound message size is enforced by the codec
	// (512KB).
	EthMsgSoftCapSize = 64 * units.KiB
)

var (
	_ GossipMessage = EthTxsGossip{}

	errUnexpectedCodecVersion = errors.New("unexpected codec version")
)

type GossipMessage interface {
	// types implementing GossipMessage should also implement fmt.Stringer for logging purposes.
	fmt.Stringer

	// Handle this gossip message with the gossip handler.
	Handle(handler GossipHandler, nodeID ids.NodeID) error
}

type EthTxsGossip struct {
	Txs []byte `serialize:"true"`
}

func (msg EthTxsGossip) Handle(handler GossipHandler, nodeID ids.NodeID) error {
	return handler.HandleEthTxs(nodeID, msg)
}

func (msg EthTxsGossip) String() string {
	return fmt.Sprintf("EthTxsGossip(Len=%d)", len(msg.Txs))
}

func ParseGossipMessage(codec codec.Manager, bytes []byte) (GossipMessage, error) {
	var msg GossipMessage
	version, err := codec.Unmarshal(bytes, &msg)
	if err != nil {
		return nil, err
	}
	if version != Version {
		return nil, errUnexpectedCodecVersion
	}
	return msg, nil
}

func BuildGossipMessage(codec codec.Manager, msg GossipMessage) ([]byte, error) {
	bytes, err := codec.Marshal(Version, &msg)
	return bytes, err
}
