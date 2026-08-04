// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

func word(v uint64) []byte {
	b := make([]byte, 32)
	binary.BigEndian.PutUint64(b[24:], v)
	return b
}

func leftAligned(b []byte) []byte {
	w := make([]byte, 32)
	copy(w, b)
	return w
}

// Selectors must match the published signatures exactly, so a wallet or dapp
// computing them independently produces the same bytes.
func TestEthSelectors(t *testing.T) {
	require.Equal(t, "bd0ab2c0", hex.EncodeToString(SelectorDelegate[:]))
	require.Equal(t, "3adfc867", hex.EncodeToString(SelectorAddValidator[:]))
}

func TestParseEthDelegate(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()

	calldata := append(SelectorDelegate[:], leftAligned(nodeID[:])...)
	calldata = append(calldata, word(1700000000)...)

	args, err := ParseEthDelegate(calldata)
	require.NoError(t, err)
	require.Equal(t, nodeID, args.NodeID)
	require.Equal(t, uint64(1700000000), args.EndTime)
}

func TestParseEthDelegateRejections(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	good := append(SelectorDelegate[:], leftAligned(nodeID[:])...)
	good = append(good, word(1700000000)...)

	tests := []struct {
		name     string
		calldata []byte
		err      error
	}{
		{
			name:     "selector only",
			calldata: SelectorDelegate[:],
			err:      ErrShortCalldata,
		},
		{
			name:     "one argument short",
			calldata: good[:len(good)-32],
			err:      ErrShortCalldata,
		},
		{
			name:     "unaligned trailing byte",
			calldata: append(append([]byte{}, good...), 0x01),
			err:      ErrCalldataNotWordSized,
		},
		{
			name: "dirty nodeID padding",
			calldata: func() []byte {
				bad := append([]byte{}, good...)
				bad[4+31] = 0x01 // beyond the 20 bytes of bytes20
				return bad
			}(),
			err: ErrBadABIPadding,
		},
		{
			name: "endTime above uint64",
			calldata: func() []byte {
				bad := append([]byte{}, good...)
				bad[4+32] = 0x01 // high byte of the uint64 word
				return bad
			}(),
			err: ErrBadABIPadding,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseEthDelegate(tt.calldata)
			require.ErrorIs(t, err, tt.err)
		})
	}
}

func addValidatorCalldataFor(nodeID ids.NodeID, endTime uint64, pk, pop []byte, feeBips uint32) []byte {
	calldata := append(SelectorAddValidator[:], leftAligned(nodeID[:])...)
	calldata = append(calldata, word(endTime)...)
	pkOffset := uint64(5 * 32)
	popOffset := pkOffset + 32 + uint64((len(pk)+31)/32*32)
	calldata = append(calldata, word(pkOffset)...)
	calldata = append(calldata, word(popOffset)...)
	calldata = append(calldata, word(uint64(feeBips))...)
	calldata = append(calldata, word(uint64(len(pk)))...)
	calldata = append(calldata, pad32(pk)...)
	calldata = append(calldata, word(uint64(len(pop)))...)
	return append(calldata, pad32(pop)...)
}

func pad32(b []byte) []byte {
	padded := make([]byte, (len(b)+31)/32*32)
	copy(padded, b)
	return padded
}

func TestParseEthAddValidator(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	pk := make([]byte, bls.PublicKeyLen)
	pk[0] = 0xaa
	pop := make([]byte, bls.SignatureLen)
	pop[0] = 0xbb

	calldata := addValidatorCalldataFor(nodeID, 1700000000, pk, pop, 20000)
	args, err := ParseEthAddValidator(calldata)
	require.NoError(t, err)
	require.Equal(t, nodeID, args.NodeID)
	require.Equal(t, uint64(1700000000), args.EndTime)
	require.Equal(t, pk, args.BLSPublicKey)
	require.Equal(t, pop, args.BLSPoP)
	require.Equal(t, uint32(20000), args.DelegationFeeBips)
}

func TestParseEthAddValidatorRejections(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	pk := make([]byte, bls.PublicKeyLen)
	pop := make([]byte, bls.SignatureLen)

	tests := []struct {
		name     string
		calldata []byte
		err      error
	}{
		{
			name:     "wrong public key length",
			calldata: addValidatorCalldataFor(nodeID, 1, pk[:bls.PublicKeyLen-1], pop, 20000),
			err:      ErrShortCalldata,
		},
		{
			name:     "wrong proof of possession length",
			calldata: addValidatorCalldataFor(nodeID, 1, pk, pop[:bls.SignatureLen-1], 20000),
			err:      ErrShortCalldata,
		},
		{
			name: "offset past the end",
			calldata: func() []byte {
				bad := addValidatorCalldataFor(nodeID, 1, pk, pop, 20000)
				copy(bad[4+2*32:], word(1<<20))
				return bad
			}(),
			err: ErrBadABIOffset,
		},
		{
			name: "unaligned offset",
			calldata: func() []byte {
				bad := addValidatorCalldataFor(nodeID, 1, pk, pop, 20000)
				copy(bad[4+2*32:], word(5*32+1))
				return bad
			}(),
			err: ErrBadABIOffset,
		},
		{
			name: "dirty tail padding",
			calldata: func() []byte {
				// bls.PublicKeyLen is 48, so its tail has 16 padding bytes.
				bad := addValidatorCalldataFor(nodeID, 1, pk, pop, 20000)
				bad[4+5*32+32+bls.PublicKeyLen] = 0x01
				return bad
			}(),
			err: ErrBadABIPadding,
		},
		{
			name: "fee above uint32",
			calldata: func() []byte {
				bad := addValidatorCalldataFor(nodeID, 1, pk, pop, 20000)
				bad[4+4*32+27] = 0x01
				return bad
			}(),
			err: ErrBadABIPadding,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseEthAddValidator(tt.calldata)
			require.ErrorIs(t, err, tt.err)
		})
	}
}
