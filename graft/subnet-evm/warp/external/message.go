// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package external

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

// ExternalMessage is encoded as the inner payload of a warp AddressedCall for
// external chain attestation messages. It represents the event being attested
// to and is included in the signed output.
//
// Lookup hints needed to verify the event (e.g. a Solana transaction signature)
// travel in the ACP-118 justification field and are never part of the signed
// output.
type ExternalMessage struct {
	// identifies the external chain (e.g. "solana", "bitcoin").
	SourceChainType string `serialize:"true" json:"sourceChainType"`

	// SourceAddress is the program or contract address on the source chain that
	// emitted the event. Must be present in the per-chain allowlist configured
	// on each validator's ExternalChainVerifier.
	SourceAddress string `serialize:"true" json:"sourceAddress"`

	// DestContract is the 20-byte destination contract address on the L1.
	DestContract []byte `serialize:"true" json:"destContract"`

	// SourceBlockHeight is the block or slot number on the source chain at
	// which the attested event occurred.
	SourceBlockHeight uint64 `serialize:"true" json:"sourceBlockHeight"`

	// Payload is the application-level data being transferred.
	Payload []byte `serialize:"true" json:"payload"`

	bytes []byte
}

// Bytes returns the canonical binary encoding of this message.
func (m *ExternalMessage) Bytes() []byte {
	return m.bytes
}

// MessageID returns a hash of the message bytes. It serves as the
// replay-protection key in the on-chain ExternalChainAdapter.
func (m *ExternalMessage) MessageID() ids.ID {
	return hashing.ComputeHash256Array(m.bytes)
}

func (m *ExternalMessage) initialize(b []byte) {
	m.bytes = b
}

// Q: Do I need netowrkID for message protection?
// NewExternalMessage creates and initializes an ExternalMessage.
func NewExternalMessage(
	sourceChainType string,
	sourceAddress string,
	destContract []byte,
	sourceBlockHeight uint64,
	payload []byte,
) (*ExternalMessage, error) {
	msg := &ExternalMessage{
		SourceChainType:   sourceChainType,
		SourceAddress:     sourceAddress,
		DestContract:      destContract,
		SourceBlockHeight: sourceBlockHeight,
		Payload:           payload,
	}
	b, err := Codec.Marshal(CodecVersion, msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ExternalMessage: %w", err)
	}
	msg.initialize(b)
	return msg, nil
}

// ParseExternalMessage deserializes an ExternalMessage from its binary encoding.
func ParseExternalMessage(b []byte) (*ExternalMessage, error) {
	var msg ExternalMessage
	if _, err := Codec.Unmarshal(b, &msg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ExternalMessage: %w", err)
	}
	msg.initialize(b)
	return &msg, nil
}
