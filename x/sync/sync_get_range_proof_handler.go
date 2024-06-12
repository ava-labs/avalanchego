package sync

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/x/merkledb"
)

var _ p2p.Handler = (*SyncGetRangeProofHandler)(nil)

func NewSyncGetRangeProofHandler(log logging.Logger, db DB) *SyncGetRangeProofHandler {
	return &SyncGetRangeProofHandler{
		log: log,
		db:  db,
	}
}

type SyncGetRangeProofHandler struct {
	log logging.Logger
	db  DB
}

func (*SyncGetRangeProofHandler) AppGossip(context.Context, ids.NodeID, []byte) {}

func (s *SyncGetRangeProofHandler) AppRequest(ctx context.Context, _ ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, error) {
	request := &pb.SyncGetRangeProofRequest{}
	if err := proto.Unmarshal(requestBytes, request); err != nil {
		return nil, fmt.Errorf("failed to unmarshal request: %w", err)
	}

	if err := validateRangeProofRequest(request); err != nil {
		return nil, fmt.Errorf("invalid range proof request: %w", err)
	}

	// override limits if they exceed caps
	request.KeyLimit = min(request.KeyLimit, maxKeyValuesLimit)
	request.BytesLimit = min(request.BytesLimit, maxByteSizeLimit)

	proofBytes, err := getRangeProof(
		ctx,
		s.db,
		request,
		func(rangeProof *merkledb.RangeProof) ([]byte, error) {
			return proto.Marshal(rangeProof.ToProto())
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get range proof: %w", err)
	}

	return proofBytes, nil
}

func (*SyncGetRangeProofHandler) CrossChainAppRequest(context.Context, ids.ID, time.Time, []byte) ([]byte, error) {
	return nil, nil
}
