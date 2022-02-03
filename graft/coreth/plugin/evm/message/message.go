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
	// EthMsgSoftCapSize is the ideal size of encoded transaction bytes we send in
	// any [EthTxs] or [AtomicTx] message. We do not limit inbound messages to
	// this size, however. Max inbound message size is enforced by the codec
	// (512KB).
	EthMsgSoftCapSize = common.StorageSize(64 * units.KiB)
	atomicTxType      = "atomic-tx"
	ethTxsType        = "eth-txs"
)

var (
	_ Message = &AtomicTx{}
	_ Message = &EthTxs{}

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

	// Type returns user-friendly name for this object that can be used for logging
	Type() string
}

type message []byte

func (m *message) initialize(bytes []byte) { *m = bytes }
func (m *message) Bytes() []byte           { return *m }

type AtomicTx struct {
	message

	Tx []byte `serialize:"true"`
}

func (msg *AtomicTx) Handle(handler GossipHandler, nodeID ids.ShortID) error {
	return handler.HandleAtomicTx(nodeID, msg)
}

func (msg *AtomicTx) Type() string {
	return atomicTxType
}

type EthTxs struct {
	message

	Txs []byte `serialize:"true"`
}

func (msg *EthTxs) Handle(handler GossipHandler, nodeID ids.ShortID) error {
	return handler.HandleEthTxs(nodeID, msg)
}

func (msg *EthTxs) Type() string {
	return ethTxsType
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
