// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	// EthMsgSoftCapSize is the ideal size of encoded transaction bytes we send in
	// any [EthTxs] or [AtomicTx] message. We do not limit inbound messages to
	// this size, however. Max inbound message size is enforced by the codec
	// (512KB).
	EthMsgSoftCapSize = common.StorageSize(64 * units.KiB)
)

var (
	_ Message = &AtomicTx{}
	_ Message = &EthTxs{}

	errUnexpectedCodecVersion = errors.New("unexpected codec version")
)

type Message interface {
	// Handle this message with the correct message handler
	Handle(handler Handler, nodeID ids.ShortID, requestID uint32) error

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

type AtomicTx struct {
	message

	Tx []byte `serialize:"true"`
}

func (msg *AtomicTx) Handle(handler Handler, nodeID ids.ShortID, requestID uint32) error {
	return handler.HandleAtomicTx(nodeID, requestID, msg)
}

type EthTxs struct {
	message

	Txs []byte `serialize:"true"`
}

func (msg *EthTxs) Handle(handler Handler, nodeID ids.ShortID, requestID uint32) error {
	return handler.HandleEthTxs(nodeID, requestID, msg)
}

func Parse(bytes []byte) (Message, error) {
	var msg Message
	version, err := c.Unmarshal(bytes, &msg)
	if err != nil {
		return nil, err
	}
	if version != codecVersion {
		return nil, errUnexpectedCodecVersion
	}
	msg.initialize(bytes)
	return msg, nil
}

func Build(msg Message) ([]byte, error) {
	bytes, err := c.Marshal(codecVersion, &msg)
	msg.initialize(bytes)
	return bytes, err
}
