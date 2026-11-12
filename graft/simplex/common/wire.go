// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"errors"
	"fmt"
)

//go:generate go run github.com/StephenButtolph/canoto/canoto wire.go

var (
	ErrUnknownMessageType = errors.New("unknown message type")
	ErrMissingQC          = errors.New("missing quorum certificate")
	ErrEmptyBlock         = errors.New("empty block")
)

// wireMessage is the wire representation of a [Message]. Exactly one field is
// set.
//
// Messages without blocks or quorum certificates are encoded directly. The
// others hold interfaces that can only be serialized to and from bytes by the
// application, so they go through the wire* types below.
type wireMessage struct {
	BlockMessage            *wireBlockMessage        `canoto:"pointer,1,Message"`
	Vote                    *Vote                    `canoto:"pointer,2,Message"`
	EmptyVote               *EmptyVote               `canoto:"pointer,3,Message"`
	FinalizeVote            *FinalizeVote            `canoto:"pointer,4,Message"`
	Notarization            *wireQuorumCertificate   `canoto:"pointer,5,Message"`
	EmptyNotarization       *wireEmptyNotarization   `canoto:"pointer,6,Message"`
	Finalization            *wireQuorumCertificate   `canoto:"pointer,7,Message"`
	ReplicationRequest      *ReplicationRequest      `canoto:"pointer,8,Message"`
	ReplicationResponse     *wireReplicationResponse `canoto:"pointer,9,Message"`
	BlockDigestRequest      *BlockDigestRequest      `canoto:"pointer,10,Message"`
	AuxiliaryInfo           *AuxiliaryInfo           `canoto:"pointer,11,Message"`
	EpochTransitionApproval *ValidatorSetApproval    `canoto:"pointer,12,Message"`

	canotoData canotoData_wireMessage
}

type wireBlockMessage struct {
	Block []byte `canoto:"bytes,1"`
	Vote  Vote   `canoto:"value,2"`

	canotoData canotoData_wireBlockMessage
}

// wireQuorumCertificate is the wire representation of both notarizations and
// finalizations.
type wireQuorumCertificate struct {
	BlockHeader BlockHeader `canoto:"value,1"`
	QC          []byte      `canoto:"bytes,2"`

	canotoData canotoData_wireQuorumCertificate
}

type wireEmptyNotarization struct {
	Metadata EmptyVoteMetadata `canoto:"value,1"`
	QC       []byte            `canoto:"bytes,2"`

	canotoData canotoData_wireEmptyNotarization
}

type wireQuorumRound struct {
	Block             []byte                 `canoto:"bytes,1"`
	Notarization      *wireQuorumCertificate `canoto:"pointer,2"`
	Finalization      *wireQuorumCertificate `canoto:"pointer,3"`
	EmptyNotarization *wireEmptyNotarization `canoto:"pointer,4"`

	canotoData canotoData_wireQuorumRound
}

type wireReplicationResponse struct {
	Data        []wireQuorumRound `canoto:"repeated value,1"`
	LatestRound *wireQuorumRound  `canoto:"pointer,2"`
	LatestSeq   *wireQuorumRound  `canoto:"pointer,3"`

	canotoData canotoData_wireReplicationResponse
}

// EncodeMessage returns the wire representation of [msg].
//
// Blocks are only encodable through the verified message variants
// (VerifiedBlockMessage and VerifiedReplicationResponse), as unverified blocks
// do not expose their bytes.
func EncodeMessage(msg *Message) ([]byte, error) {
	// Directly encoded fields are shallow-copied so that marshalling doesn't
	// write canoto's size cache into structs the caller may share.
	var (
		w   wireMessage
		err error
	)
	switch {
	case msg.VerifiedBlockMessage != nil:
		w.BlockMessage = &wireBlockMessage{
			Block: msg.VerifiedBlockMessage.VerifiedBlock.Bytes(),
			Vote:  msg.VerifiedBlockMessage.Vote,
		}
	case msg.VoteMessage != nil:
		v := *msg.VoteMessage
		w.Vote = &v
	case msg.EmptyVoteMessage != nil:
		v := *msg.EmptyVoteMessage
		w.EmptyVote = &v
	case msg.FinalizeVote != nil:
		v := *msg.FinalizeVote
		w.FinalizeVote = &v
	case msg.Notarization != nil:
		w.Notarization, err = qcToWire(msg.Notarization.Vote.BlockHeader, msg.Notarization.QC)
	case msg.EmptyNotarization != nil:
		w.EmptyNotarization, err = emptyNotarizationToWire(msg.EmptyNotarization)
	case msg.Finalization != nil:
		w.Finalization, err = qcToWire(msg.Finalization.Finalization.BlockHeader, msg.Finalization.QC)
	case msg.ReplicationRequest != nil:
		r := *msg.ReplicationRequest
		w.ReplicationRequest = &r
	case msg.VerifiedReplicationResponse != nil:
		w.ReplicationResponse, err = replicationResponseToWire(msg.VerifiedReplicationResponse)
	case msg.BlockDigestRequest != nil:
		r := *msg.BlockDigestRequest
		w.BlockDigestRequest = &r
	case msg.AuxiliaryInfo != nil:
		a := msg.AuxiliaryInfo.Clone()
		w.AuxiliaryInfo = &a
	case msg.EpochTransitionApproval != nil:
		a := *msg.EpochTransitionApproval
		w.EpochTransitionApproval = &a
	default:
		return nil, ErrUnknownMessageType
	}
	if err != nil {
		return nil, err
	}
	return w.MarshalCanoto(), nil
}

// DecodeMessage parses a message produced by [EncodeMessage].
//
// Blocks are deserialized with [blockDeserializer] and quorum certificates with
// [qcDeserializer]. Messages that carry blocks are returned as their unverified
// variants (BlockMessage and ReplicationResponse).
func DecodeMessage(
	ctx context.Context,
	b []byte,
	blockDeserializer BlockDeserializer,
	qcDeserializer QCDeserializer,
) (*Message, error) {
	var w wireMessage
	if err := w.UnmarshalCanoto(b); err != nil {
		return nil, err
	}
	if w.canotoData.MessageOneOf == uint32(canotoOneOfUnset_wireMessage__Message) {
		return nil, ErrUnknownMessageType
	}

	var (
		msg = &Message{
			VoteMessage:             w.Vote,
			EmptyVoteMessage:        w.EmptyVote,
			FinalizeVote:            w.FinalizeVote,
			ReplicationRequest:      w.ReplicationRequest,
			BlockDigestRequest:      w.BlockDigestRequest,
			AuxiliaryInfo:           w.AuxiliaryInfo,
			EpochTransitionApproval: w.EpochTransitionApproval,
		}
		err error
	)
	switch {
	case w.BlockMessage != nil:
		var block Block
		block, err = blockFromWire(ctx, w.BlockMessage.Block, blockDeserializer)
		msg.BlockMessage = &BlockMessage{Block: block, Vote: w.BlockMessage.Vote}
	case w.Notarization != nil:
		msg.Notarization, err = w.Notarization.toNotarization(qcDeserializer)
	case w.EmptyNotarization != nil:
		msg.EmptyNotarization, err = w.EmptyNotarization.toEmptyNotarization(qcDeserializer)
	case w.Finalization != nil:
		msg.Finalization, err = w.Finalization.toFinalization(qcDeserializer)
	case w.ReplicationResponse != nil:
		msg.ReplicationResponse, err = w.ReplicationResponse.toReplicationResponse(ctx, blockDeserializer, qcDeserializer)
	}
	if err != nil {
		return nil, err
	}
	return msg, nil
}

// Encoding helpers

func qcToWire(header BlockHeader, qc QuorumCertificate) (*wireQuorumCertificate, error) {
	if qc == nil {
		return nil, ErrMissingQC
	}
	return &wireQuorumCertificate{
		BlockHeader: header,
		QC:          qc.Bytes(),
	}, nil
}

func emptyNotarizationToWire(en *EmptyNotarization) (*wireEmptyNotarization, error) {
	if en.QC == nil {
		return nil, ErrMissingQC
	}
	return &wireEmptyNotarization{
		Metadata: en.Vote.EmptyVoteMetadata,
		QC:       en.QC.Bytes(),
	}, nil
}

// quorumRoundToWire returns the wire representation of [qr]. It returns
// (nil, nil) if [qr] contains the genesis finalization, which has no QC and
// therefore can't be sent.
func quorumRoundToWire(qr *VerifiedQuorumRound) (*wireQuorumRound, error) {
	if qr == nil || (qr.Finalization != nil && qr.Finalization.QC == nil) {
		return nil, nil
	}

	var (
		w   wireQuorumRound
		err error
	)
	if qr.VerifiedBlock != nil {
		w.Block = qr.VerifiedBlock.Bytes()
	}
	if qr.Notarization != nil {
		if w.Notarization, err = qcToWire(qr.Notarization.Vote.BlockHeader, qr.Notarization.QC); err != nil {
			return nil, fmt.Errorf("notarization: %w", err)
		}
	}
	if qr.Finalization != nil {
		if w.Finalization, err = qcToWire(qr.Finalization.Finalization.BlockHeader, qr.Finalization.QC); err != nil {
			return nil, fmt.Errorf("finalization: %w", err)
		}
	}
	if qr.EmptyNotarization != nil {
		if w.EmptyNotarization, err = emptyNotarizationToWire(qr.EmptyNotarization); err != nil {
			return nil, fmt.Errorf("empty notarization: %w", err)
		}
	}
	return &w, nil
}

func replicationResponseToWire(resp *VerifiedReplicationResponse) (*wireReplicationResponse, error) {
	w := &wireReplicationResponse{
		Data: make([]wireQuorumRound, 0, len(resp.Data)),
	}
	for i := range resp.Data {
		qr, err := quorumRoundToWire(&resp.Data[i])
		if err != nil {
			return nil, err
		}
		if qr != nil {
			w.Data = append(w.Data, *qr)
		}
	}

	var err error
	if w.LatestRound, err = quorumRoundToWire(resp.LatestRound); err != nil {
		return nil, err
	}
	if w.LatestSeq, err = quorumRoundToWire(resp.LatestFinalizedSeq); err != nil {
		return nil, err
	}
	return w, nil
}

// Decoding helpers

func blockFromWire(ctx context.Context, b []byte, blockDeserializer BlockDeserializer) (Block, error) {
	if len(b) == 0 {
		return nil, ErrEmptyBlock
	}
	block, err := blockDeserializer.DeserializeBlock(ctx, b)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize block: %w", err)
	}
	return block, nil
}

func qcFromWire(b []byte, qcDeserializer QCDeserializer) (QuorumCertificate, error) {
	if len(b) == 0 {
		return nil, ErrMissingQC
	}
	qc, err := qcDeserializer.DeserializeQuorumCertificate(b)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize quorum certificate: %w", err)
	}
	return qc, nil
}

func (w *wireQuorumCertificate) toNotarization(qcDeserializer QCDeserializer) (*Notarization, error) {
	qc, err := qcFromWire(w.QC, qcDeserializer)
	if err != nil {
		return nil, err
	}
	return &Notarization{
		Vote: ToBeSignedVote{BlockHeader: w.BlockHeader},
		QC:   qc,
	}, nil
}

func (w *wireQuorumCertificate) toFinalization(qcDeserializer QCDeserializer) (*Finalization, error) {
	qc, err := qcFromWire(w.QC, qcDeserializer)
	if err != nil {
		return nil, err
	}
	return &Finalization{
		Finalization: ToBeSignedFinalization{BlockHeader: w.BlockHeader},
		QC:           qc,
	}, nil
}

func (w *wireEmptyNotarization) toEmptyNotarization(qcDeserializer QCDeserializer) (*EmptyNotarization, error) {
	qc, err := qcFromWire(w.QC, qcDeserializer)
	if err != nil {
		return nil, err
	}
	return &EmptyNotarization{
		Vote: ToBeSignedEmptyVote{EmptyVoteMetadata: w.Metadata},
		QC:   qc,
	}, nil
}

func (w *wireQuorumRound) toQuorumRound(
	ctx context.Context,
	blockDeserializer BlockDeserializer,
	qcDeserializer QCDeserializer,
) (*QuorumRound, error) {
	var (
		qr  QuorumRound
		err error
	)
	if len(w.Block) != 0 {
		if qr.Block, err = blockFromWire(ctx, w.Block, blockDeserializer); err != nil {
			return nil, err
		}
	}
	if w.Notarization != nil {
		if qr.Notarization, err = w.Notarization.toNotarization(qcDeserializer); err != nil {
			return nil, fmt.Errorf("notarization: %w", err)
		}
	}
	if w.Finalization != nil {
		if qr.Finalization, err = w.Finalization.toFinalization(qcDeserializer); err != nil {
			return nil, fmt.Errorf("finalization: %w", err)
		}
	}
	if w.EmptyNotarization != nil {
		if qr.EmptyNotarization, err = w.EmptyNotarization.toEmptyNotarization(qcDeserializer); err != nil {
			return nil, fmt.Errorf("empty notarization: %w", err)
		}
	}
	return &qr, nil
}

func (w *wireReplicationResponse) toReplicationResponse(
	ctx context.Context,
	blockDeserializer BlockDeserializer,
	qcDeserializer QCDeserializer,
) (*ReplicationResponse, error) {
	resp := &ReplicationResponse{
		Data: make([]QuorumRound, 0, len(w.Data)),
	}
	for i := range w.Data {
		qr, err := w.Data[i].toQuorumRound(ctx, blockDeserializer, qcDeserializer)
		if err != nil {
			return nil, fmt.Errorf("data[%d]: %w", i, err)
		}
		resp.Data = append(resp.Data, *qr)
	}

	var err error
	if w.LatestRound != nil {
		if resp.LatestRound, err = w.LatestRound.toQuorumRound(ctx, blockDeserializer, qcDeserializer); err != nil {
			return nil, fmt.Errorf("latest round: %w", err)
		}
	}
	if w.LatestSeq != nil {
		if resp.LatestSeq, err = w.LatestSeq.toQuorumRound(ctx, blockDeserializer, qcDeserializer); err != nil {
			return nil, fmt.Errorf("latest seq: %w", err)
		}
	}
	return resp, nil
}
