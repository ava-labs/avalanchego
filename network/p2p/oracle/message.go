// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package oracle

import (
	"fmt"

	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

// oracleMessageArgs defines the ABI encoding for OracleMessage.
// The warp payload is abi.encode(sourceType, sourceAddress, destContract,
// sourceBlockHeight, nonce, payload) — identical to abi.encode of the
// individual fields in Solidity, which is what OracleAdapter.sol expects.
var oracleMessageArgs abi.Arguments

func init() {
	stringT, _ := abi.NewType("string", "", nil)
	addrT, _ := abi.NewType("address", "", nil)
	uint64T, _ := abi.NewType("uint64", "", nil)
	bytesT, _ := abi.NewType("bytes", "", nil)

	oracleMessageArgs = abi.Arguments{
		{Type: stringT, Name: "sourceType"},
		{Type: stringT, Name: "sourceAddress"},
		{Type: addrT, Name: "destContract"},
		{Type: uint64T, Name: "sourceBlockHeight"},
		{Type: uint64T, Name: "nonce"},
		{Type: bytesT, Name: "payload"},
	}
}

// OracleMessage is the application-level payload attested by validators.
// It is ABI-encoded as the warp UnsignedMessage payload so that OracleAdapter.sol
// can verify it on-chain without a custom decoder.
type OracleMessage struct {
	// SourceType identifies the external source (e.g. "solana", "bitcoin", "price-feed").
	SourceType string
	// SourceAddress is the program or contract address on the source chain.
	SourceAddress string
	// DestContract is the 20-byte destination contract address on the L1.
	DestContract []byte
	// SourceBlockHeight is the block or slot on the source chain at which the event occurred.
	SourceBlockHeight uint64
	// Nonce is unique per (SourceType, SourceAddress) and is used for replay protection.
	// The on-chain adapter keys processed messages on keccak256(sourceType, sourceAddress, nonce).
	Nonce uint64
	// Payload is the application-level data being transferred.
	Payload []byte

	bytes []byte
}

// Bytes returns the ABI-encoded representation of the message. This is what
// goes into the warp UnsignedMessage payload and what OracleAdapter.sol hashes
// for the payload-binding check.
func (m *OracleMessage) Bytes() []byte {
	return m.bytes
}

// ID returns a hash of the message bytes, used as a content identifier.
func (m *OracleMessage) ID() ids.ID {
	return hashing.ComputeHash256Array(m.bytes)
}

func (m *OracleMessage) initialize(b []byte) {
	m.bytes = b
}

func NewOracleMessage(
	sourceType string,
	sourceAddress string,
	destContract []byte,
	sourceBlockHeight uint64,
	nonce uint64,
	payload []byte,
) (*OracleMessage, error) {
	msg := &OracleMessage{
		SourceType:        sourceType,
		SourceAddress:     sourceAddress,
		DestContract:      destContract,
		SourceBlockHeight: sourceBlockHeight,
		Nonce:             nonce,
		Payload:           payload,
	}
	b, err := msg.encode()
	if err != nil {
		return nil, fmt.Errorf("failed to ABI-encode OracleMessage: %w", err)
	}
	msg.initialize(b)
	return msg, nil
}

func ParseOracleMessage(b []byte) (*OracleMessage, error) {
	vals, err := oracleMessageArgs.Unpack(b)
	if err != nil {
		return nil, fmt.Errorf("failed to ABI-decode OracleMessage: %w", err)
	}
	if len(vals) != 6 {
		return nil, fmt.Errorf("unexpected number of ABI values: got %d, want 6", len(vals))
	}
	addr, ok := vals[2].(common.Address)
	if !ok {
		return nil, fmt.Errorf("unexpected type for destContract: %T", vals[2])
	}
	msg := &OracleMessage{
		SourceType:        vals[0].(string),
		SourceAddress:     vals[1].(string),
		DestContract:      addr.Bytes(),
		SourceBlockHeight: vals[3].(uint64),
		Nonce:             vals[4].(uint64),
		Payload:           vals[5].([]byte),
	}
	msg.initialize(b)
	return msg, nil
}

func (m *OracleMessage) encode() ([]byte, error) {
	return oracleMessageArgs.Pack(
		m.SourceType,
		m.SourceAddress,
		common.BytesToAddress(m.DestContract),
		m.SourceBlockHeight,
		m.Nonce,
		m.Payload,
	)
}
