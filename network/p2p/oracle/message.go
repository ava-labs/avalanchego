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

const SourceTypeSolana = "solana"

// KnownSourceTypes is the compile-time set of source types this build supports.
// The validator config loader and the sidecar main both cross-check configured
// entries against this set at startup, turning typos into boot failures.
var KnownSourceTypes = map[string]struct{}{
	SourceTypeSolana: {},
}

func IsKnownSourceType(s string) bool {
	_, ok := KnownSourceTypes[s]
	return ok
}

var oracleMessageArgs abi.Arguments

func init() {
	mustType := func(t string) abi.Type {
		typ, err := abi.NewType(t, "", nil)
		if err != nil {
			panic(fmt.Sprintf("invalid ABI type %q: %v", t, err))
		}
		return typ
	}
	stringT := mustType("string")
	addrT := mustType("address")
	uint64T := mustType("uint64")
	bytesT := mustType("bytes")

	oracleMessageArgs = abi.Arguments{
		{Type: stringT, Name: "sourceType"},
		{Type: stringT, Name: "sourceAddress"},
		{Type: addrT, Name: "destContract"},
		{Type: uint64T, Name: "sourceBlockHeight"},
		{Type: uint64T, Name: "nonce"},
		{Type: bytesT, Name: "payload"},
	}
}

// OracleMessage is ABI-encoded as the warp UnsignedMessage payload so
// OracleAdapter.sol can decode it natively.
// Nonce is unique per (SourceType, SourceAddress) for on-chain replay protection.
type OracleMessage struct {
	SourceType        string
	SourceAddress     string
	DestContract      common.Address
	SourceBlockHeight uint64
	Nonce             uint64
	Payload           []byte

	bytes []byte
}

func (m *OracleMessage) Bytes() []byte {
	return m.bytes
}

func (m *OracleMessage) ID() ids.ID {
	return hashing.ComputeHash256Array(m.bytes)
}

func (m *OracleMessage) initialize(b []byte) {
	m.bytes = b
}

func NewOracleMessage(
	sourceType string,
	sourceAddress string,
	destContract common.Address,
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
		DestContract:      addr,
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
		m.DestContract,
		m.SourceBlockHeight,
		m.Nonce,
		m.Payload,
	)
}
