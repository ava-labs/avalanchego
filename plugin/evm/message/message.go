// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"

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
	_ Message = &Txs{}

	errUnexpectedCodecVersion = errors.New("unexpected codec version")
)

type Message interface {
	// Handle this message with the correct message handler
	Handle(handler GossipHandler, nodeID ids.ShortID) error

	// initialize should be called whenever a message is built or parsed
	initialize([]byte)

	// Bytes returns the binary representation of this message
	//
	// Bytes should only be called after being initialized
	Bytes() []byte
}

type message []byte

func (m *message) initialize(bytes []byte) { *m = bytes }
func (m *message) Bytes() []byte           { return *m }

type Txs struct {
	message

	Txs []byte `serialize:"true"`
}

func (msg *Txs) Handle(handler GossipHandler, nodeID ids.ShortID) error {
	return handler.HandleTxs(nodeID, msg)
}

func ParseMessage(codec codec.Manager, bytes []byte) (Message, error) {
	var msg Message
	version, err := codec.Unmarshal(bytes, &msg)
	if err != nil {
		return nil, err
	}
	if version != Version {
		return nil, errUnexpectedCodecVersion
	}
	msg.initialize(bytes)
	return msg, nil
}

func BuildMessage(codec codec.Manager, msg Message) ([]byte, error) {
	bytes, err := codec.Marshal(Version, &msg)
	msg.initialize(bytes)
	return bytes, err
}
