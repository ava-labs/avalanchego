// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"fmt"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"errors"
)

var (
	errInvalidMessage = errors.New("invalid message")
	errInvalidNetworkID = errors.New("invalid network id")
	errInvalidSourceChainID = errors.New("invalid source chain id")
	errInvalidPayload = errors.New("invalid payload")
)
// OffChainMessages contains messages that explicitly specified to sign.
type OffChainMessages struct {
	offChainMsgs map[ids.ID]*warp.UnsignedMessage
}

func NewOffChainMessages(
	networkID uint32,
	sourceChainID ids.ID,
	offChainMsgs [][]byte,
) (
	OffChainMessages, error,
) {
	offChainAddressedCallMsgs := make(map[ids.ID]*warp.UnsignedMessage)

	for _, msg := range offChainMsgs {
		unsignedMsg, err := warp.ParseUnsignedMessage(msg)
		if err != nil {
			return OffChainMessages{}, fmt.Errorf("%w: %w", errInvalidMessage, err)
		}

		if unsignedMsg.NetworkID != networkID {
			return OffChainMessages{}, fmt.Errorf(
				"%w: got %d but expected %d",
				errInvalidNetworkID,
				unsignedMsg.NetworkID,
				networkID,
			)
		}

		if unsignedMsg.SourceChainID != sourceChainID {
			return OffChainMessages{}, fmt.Errorf(
				"%w: got %d but expected %d",
				errInvalidSourceChainID,
				sourceChainID,
				unsignedMsg.SourceChainID,
			)
		}

		_, err = payload.ParseAddressedCall(unsignedMsg.Payload)
		if err != nil {
			return OffChainMessages{}, fmt.Errorf("%w: %w", errInvalidPayload, err)
		}

		offChainAddressedCallMsgs[unsignedMsg.ID()] = unsignedMsg
	}

	return OffChainMessages{offChainMsgs: offChainAddressedCallMsgs}, nil
}

func (o *OffChainMessages) Get(id ids.ID) (*warp.UnsignedMessage, bool) {
	msg, ok := o.offChainMsgs[id]
	return msg, ok
}
