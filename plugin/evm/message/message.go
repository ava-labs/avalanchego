// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"

	"github.com/ethereum/go-ethereum/common"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	// TxMsgSoftCapSize is the ideal size of encoded transaction bytes we send in
	// any [Txs] message. We do not limit inbound messages to
	// this size, however. Max inbound message size is enforced by the codec
	// (512KB).
	TxMsgSoftCapSize = common.StorageSize(64 * units.KiB)
)

var (
	_ GossipMessage = TxsGossip{}

	errUnexpectedCodecVersion = errors.New("unexpected codec version")
)

type GossipMessage interface {
	// types implementing GossipMessage should also implement fmt.Stringer for logging purposes.
	fmt.Stringer

	// Handle this gossip message with the gossip handler.
	Handle(handler GossipHandler, nodeID ids.ShortID) error
}

type TxsGossip struct {
	Txs []byte `serialize:"true"`
}

func (msg TxsGossip) Handle(handler GossipHandler, nodeID ids.ShortID) error {
	return handler.HandleTxs(nodeID, msg)
}

func (msg TxsGossip) String() string {
	return fmt.Sprintf("TxsGossip(Len=%d)", len(msg.Txs))
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
