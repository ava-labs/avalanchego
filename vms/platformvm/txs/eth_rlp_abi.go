// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"

	ethcommon "github.com/ava-labs/libevm/common"
	ethcrypto "github.com/ava-labs/libevm/crypto"
)

// EthStakingAddress is the system address that eth facade staking calls target.
// It holds no state and runs no code: the calldata is interpreted by the
// P-chain executor, not by an EVM.
var EthStakingAddress = ethcommon.HexToAddress("0x0100000000000000000000000000000000000001")

// Selectors, keccak256(signature)[:4] of:
//
//	delegate(bytes20,uint64)
//	addValidator(bytes20,uint64,bytes,bytes,uint32)
var (
	SelectorDelegate     = ethSelector("delegate(bytes20,uint64)")
	SelectorAddValidator = ethSelector("addValidator(bytes20,uint64,bytes,bytes,uint32)")
)

var (
	ErrUnknownSelector      = errors.New("unknown eth facade selector")
	ErrShortCalldata        = errors.New("calldata too short")
	ErrCalldataNotWordSized = errors.New("calldata arguments are not 32-byte aligned")
	ErrBadABIPadding        = errors.New("non-zero padding in an ABI argument")
	ErrBadABIOffset         = errors.New("bad ABI dynamic argument offset")
)

func ethSelector(signature string) [4]byte {
	var selector [4]byte
	copy(selector[:], ethcrypto.Keccak256([]byte(signature)))
	return selector
}

// EthDelegateArgs is the decoded form of delegate(nodeID, endTime).
type EthDelegateArgs struct {
	NodeID  ids.NodeID
	EndTime uint64
}

// EthAddValidatorArgs is the decoded form of
// addValidator(nodeID, endTime, blsPublicKey, blsPoP, delegationFeeBips).
type EthAddValidatorArgs struct {
	NodeID            ids.NodeID
	EndTime           uint64
	BLSPublicKey      []byte
	BLSPoP            []byte
	DelegationFeeBips uint32
}

// ParseEthDelegate decodes delegate(bytes20 nodeID, uint64 endTime).
func ParseEthDelegate(calldata []byte) (*EthDelegateArgs, error) {
	words, err := abiWords(calldata, 2)
	if err != nil {
		return nil, err
	}
	nodeID, err := abiBytes20(words[0])
	if err != nil {
		return nil, err
	}
	endTime, err := abiUint64(words[1])
	if err != nil {
		return nil, err
	}
	return &EthDelegateArgs{
		NodeID:  ids.NodeID(nodeID),
		EndTime: endTime,
	}, nil
}

// ParseEthAddValidator decodes addValidator(bytes20 nodeID, uint64 endTime,
// bytes blsPublicKey, bytes blsPoP, uint32 delegationFeeBips).
func ParseEthAddValidator(calldata []byte) (*EthAddValidatorArgs, error) {
	words, err := abiWords(calldata, 5)
	if err != nil {
		return nil, err
	}
	nodeID, err := abiBytes20(words[0])
	if err != nil {
		return nil, err
	}
	endTime, err := abiUint64(words[1])
	if err != nil {
		return nil, err
	}
	head := calldata[4:]
	publicKey, err := abiDynamicBytes(head, words[2], bls.PublicKeyLen)
	if err != nil {
		return nil, fmt.Errorf("blsPublicKey: %w", err)
	}
	pop, err := abiDynamicBytes(head, words[3], bls.SignatureLen)
	if err != nil {
		return nil, fmt.Errorf("blsPoP: %w", err)
	}
	feeBips, err := abiUint32(words[4])
	if err != nil {
		return nil, err
	}
	return &EthAddValidatorArgs{
		NodeID:            ids.NodeID(nodeID),
		EndTime:           endTime,
		BLSPublicKey:      publicKey,
		BLSPoP:            pop,
		DelegationFeeBips: feeBips,
	}, nil
}

// abiWords returns the [count] 32-byte head words after the selector. Extra
// trailing data is allowed only as the tail of dynamic arguments.
func abiWords(calldata []byte, count int) ([][32]byte, error) {
	if len(calldata) < 4+32*count {
		return nil, fmt.Errorf("%w: %d bytes, need %d", ErrShortCalldata, len(calldata), 4+32*count)
	}
	if (len(calldata)-4)%32 != 0 {
		return nil, ErrCalldataNotWordSized
	}
	words := make([][32]byte, count)
	for i := range words {
		copy(words[i][:], calldata[4+32*i:])
	}
	return words, nil
}

func abiBytes20(word [32]byte) (ids.ShortID, error) {
	// bytes20 is left-aligned in its word.
	if !allZero(word[20:]) {
		return ids.ShortEmpty, ErrBadABIPadding
	}
	return ids.ShortID(word[:20]), nil
}

func abiUint64(word [32]byte) (uint64, error) {
	if !allZero(word[:24]) {
		return 0, ErrBadABIPadding
	}
	return binary.BigEndian.Uint64(word[24:]), nil
}

func abiUint32(word [32]byte) (uint32, error) {
	if !allZero(word[:28]) {
		return 0, ErrBadABIPadding
	}
	return binary.BigEndian.Uint32(word[28:]), nil
}

// abiDynamicBytes resolves a dynamic `bytes` argument whose offset word is
// [offsetWord], requiring exactly [wantLen] bytes of payload.
func abiDynamicBytes(head []byte, offsetWord [32]byte, wantLen int) ([]byte, error) {
	offset, err := abiUint64(offsetWord)
	if err != nil {
		return nil, err
	}
	if offset%32 != 0 || offset > uint64(len(head)) || uint64(len(head))-offset < 32 {
		return nil, ErrBadABIOffset
	}
	var lengthWord [32]byte
	copy(lengthWord[:], head[offset:])
	length, err := abiUint64(lengthWord)
	if err != nil {
		return nil, err
	}
	if length != uint64(wantLen) {
		return nil, fmt.Errorf("%w: got %d bytes, want %d", ErrShortCalldata, length, wantLen)
	}
	start := offset + 32
	if uint64(len(head))-start < length {
		return nil, ErrBadABIOffset
	}
	payload := head[start : start+length]
	// The tail is zero-padded to a word boundary.
	if padded := (length + 31) / 32 * 32; uint64(len(head))-start >= padded &&
		!allZero(head[start+length:start+padded]) {
		return nil, ErrBadABIPadding
	}
	return payload, nil
}

func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
