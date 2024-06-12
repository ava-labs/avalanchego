// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/x/merkledb"
)

var _ p2p.Handler = (*SyncGetChangeProofHandler)(nil)

func NewSyncGetChangeProofHandler(log logging.Logger, db DB) *SyncGetChangeProofHandler {
	return &SyncGetChangeProofHandler{
		log: log,
		db:  db,
	}
}

type SyncGetChangeProofHandler struct {
	log logging.Logger
	db  DB
}

// AppGossip is not implemented
func (*SyncGetChangeProofHandler) AppGossip(context.Context, ids.NodeID, []byte) {
	return
}

func (s *SyncGetChangeProofHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, error) {
	request := &pb.SyncGetChangeProofRequest{}
	if err := proto.Unmarshal(requestBytes, request); err != nil {
		return nil, fmt.Errorf("failed to unmarshal request: %w", err)
	}

	if err := validateChangeProofRequest(request); err != nil {
		s.log.Debug(
			"dropping invalid change proof request",
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("request", request),
			zap.Error(err),
		)
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// override limits if they exceed caps
	var (
		keyLimit   = min(request.KeyLimit, maxKeyValuesLimit)
		bytesLimit = min(int(request.BytesLimit), maxByteSizeLimit)
		start      = maybeBytesToMaybe(request.StartKey)
		end        = maybeBytesToMaybe(request.EndKey)
	)

	startRoot, err := ids.ToID(request.StartRootHash)
	if err != nil {
		return nil, fmt.Errorf("failed to parse start root hash: %w", err)
	}

	endRoot, err := ids.ToID(request.EndRootHash)
	if err != nil {
		return nil, err
	}

	for keyLimit > 0 {
		changeProof, err := s.db.GetChangeProof(ctx, startRoot, endRoot, start, end, int(keyLimit))
		if err != nil {
			if !errors.Is(err, merkledb.ErrInsufficientHistory) {
				// We should only fail to get a change proof if we have insufficient history.
				// Other errors are unexpected.
				return nil, fmt.Errorf("failed to get change proof: %w", err)
			}
			if errors.Is(err, merkledb.ErrNoEndRoot) {
				// [s.db] doesn't have [endRoot] in its history.
				// We can't generate a change/range proof. Drop this request.
				return nil, fmt.Errorf("failed to get change proof: %w", err)
			}

			// [s.db] doesn't have sufficient history to generate change proof.
			// Generate a range proof for the end root ID instead.
			proofBytes, err := getRangeProof(
				ctx,
				s.db,
				&pb.SyncGetRangeProofRequest{
					RootHash:   request.EndRootHash,
					StartKey:   request.StartKey,
					EndKey:     request.EndKey,
					KeyLimit:   request.KeyLimit,
					BytesLimit: request.BytesLimit,
				},
				func(rangeProof *merkledb.RangeProof) ([]byte, error) {
					return proto.Marshal(&pb.SyncGetChangeProofResponse{
						Response: &pb.SyncGetChangeProofResponse_RangeProof{
							RangeProof: rangeProof.ToProto(),
						},
					})
				},
			)
			if err != nil {
				return nil, fmt.Errorf("failed to get range proof: %w", err)
			}

			return proofBytes, nil
		}

		// We generated a change proof. See if it's small enough.
		proofBytes, err := proto.Marshal(&pb.SyncGetChangeProofResponse{
			Response: &pb.SyncGetChangeProofResponse_ChangeProof{
				ChangeProof: changeProof.ToProto(),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal change proof: %w", err)
		}

		if len(proofBytes) < bytesLimit {
			return proofBytes, nil
		}

		// The proof was too large. Try to shrink it.
		keyLimit = uint32(len(changeProof.KeyChanges)) / 2
	}

	return nil, fmt.Errorf("failed to generate proof: %w", ErrMinProofSizeIsTooLarge)
}

// CrossChainAppRequest is not implemented
func (*SyncGetChangeProofHandler) CrossChainAppRequest(context.Context, ids.ID, time.Time, []byte) ([]byte, error) {
	return nil, nil
}
