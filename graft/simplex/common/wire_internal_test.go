// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/StephenButtolph/canoto"
	"github.com/stretchr/testify/require"
)

var errDeserialize = errors.New("deserialize")

type failingBlockDeserializer struct{}

func (failingBlockDeserializer) DeserializeBlock(context.Context, []byte) (Block, error) {
	return nil, errDeserialize
}

type failingQCDeserializer struct{}

func (failingQCDeserializer) DeserializeQuorumCertificate([]byte) (QuorumCertificate, error) {
	return nil, errDeserialize
}

func TestDecodeMessageErrors(t *testing.T) {
	qc := []byte("qc")

	tests := []struct {
		name        string
		msg         []byte
		expectedErr error
	}{
		{
			name:        "empty bytes",
			msg:         nil,
			expectedErr: ErrUnknownMessageType,
		},
		{
			name:        "truncated encoding",
			msg:         []byte{0xff},
			expectedErr: io.ErrUnexpectedEOF,
		},
		{
			name: "block message without block",
			msg: (&wireMessage{
				BlockMessage: &wireBlockMessage{},
			}).MarshalCanoto(),
			expectedErr: ErrEmptyBlock,
		},
		{
			name: "block message with undecodable block",
			msg: (&wireMessage{
				BlockMessage: &wireBlockMessage{Block: []byte("block")},
			}).MarshalCanoto(),
			expectedErr: errDeserialize,
		},
		{
			name: "notarization without QC",
			msg: (&wireMessage{
				Notarization: &wireQuorumCertificate{},
			}).MarshalCanoto(),
			expectedErr: ErrMissingQC,
		},
		{
			name: "notarization with undecodable QC",
			msg: (&wireMessage{
				Notarization: &wireQuorumCertificate{QC: qc},
			}).MarshalCanoto(),
			expectedErr: errDeserialize,
		},
		{
			name: "empty notarization without QC",
			msg: (&wireMessage{
				EmptyNotarization: &wireEmptyNotarization{},
			}).MarshalCanoto(),
			expectedErr: ErrMissingQC,
		},
		{
			name: "finalization without QC",
			msg: (&wireMessage{
				Finalization: &wireQuorumCertificate{},
			}).MarshalCanoto(),
			expectedErr: ErrMissingQC,
		},
		{
			name: "replication response with QC-less notarization",
			msg: (&wireMessage{
				ReplicationResponse: &wireReplicationResponse{
					LatestRound: &wireQuorumRound{Notarization: &wireQuorumCertificate{}},
				},
			}).MarshalCanoto(),
			expectedErr: ErrMissingQC,
		},
		{
			name: "empty replication request",
			msg: (&wireMessage{
				ReplicationRequest: &ReplicationRequest{},
			}).MarshalCanoto(),
		},
		{
			name: "empty replication response",
			msg: (&wireMessage{
				ReplicationResponse: &wireReplicationResponse{},
			}).MarshalCanoto(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeMessage(t.Context(), tt.msg, failingBlockDeserializer{}, failingQCDeserializer{})
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestDecodeMessageRejectsMultipleMessageTypes(t *testing.T) {
	vote := (&wireMessage{Vote: &Vote{}}).MarshalCanoto()
	request := (&wireMessage{ReplicationRequest: &ReplicationRequest{}}).MarshalCanoto()

	_, err := DecodeMessage(t.Context(), append(vote, request...), failingBlockDeserializer{}, failingQCDeserializer{})
	require.ErrorIs(t, err, canoto.ErrDuplicateOneOf)
}

func TestDecodeMessageRejectsInvalidDigestLength(t *testing.T) {
	// A BlockDigestRequest whose digest is a single byte.
	w := canoto.Writer{}
	canoto.Append(&w, canoto.Tag(2, canoto.Len))
	canoto.AppendBytes(&w, []byte{0x01})
	inner := w.B

	w = canoto.Writer{}
	canoto.Append(&w, canoto.Tag(10, canoto.Len))
	canoto.AppendBytes(&w, inner)

	_, err := DecodeMessage(t.Context(), w.B, failingBlockDeserializer{}, failingQCDeserializer{})
	require.ErrorIs(t, err, canoto.ErrInvalidLength)
}

func FuzzDecodeMessage(f *testing.F) {
	f.Add((&wireMessage{Vote: &Vote{}}).MarshalCanoto())
	f.Add((&wireMessage{ReplicationResponse: &wireReplicationResponse{
		Data: []wireQuorumRound{{Block: []byte("block")}},
	}}).MarshalCanoto())

	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = DecodeMessage(t.Context(), b, failingBlockDeserializer{}, failingQCDeserializer{})
	})
}
