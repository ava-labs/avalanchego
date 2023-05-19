// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/x/merkledb"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const (
	// Maximum number of key-value pairs to return in a proof.
	// This overrides any other Limit specified in a RangeProofRequest
	// or ChangeProofRequest if the given Limit is greater.
	maxKeyValuesLimit = 2048
	// Estimated max overhead, in bytes, of putting a proof into a message.
	// We use this to ensure that the proof we generate is not too large to fit in a message.
	// TODO: refine this estimate. This is almost certainly a large overestimate.
	estimatedMessageOverhead = 4 * units.KiB
	maxByteSizeLimit         = constants.DefaultMaxMessageSize - estimatedMessageOverhead
	endProofSizeBufferAmount = 2 * units.KiB
)

var ErrMinProofSizeIsTooLarge = errors.New("cannot generate any proof within the requested limit")

type NetworkServer struct {
	appSender common.AppSender // Used to respond to peer requests via AppResponse.
	db        *merkledb.Database
	log       logging.Logger
}

func NewNetworkServer(appSender common.AppSender, db *merkledb.Database, log logging.Logger) *NetworkServer {
	return &NetworkServer{
		appSender: appSender,
		db:        db,
		log:       log,
	}
}

// AppRequest is called by avalanchego -> VM when there is an incoming AppRequest from a peer.
// Never returns errors as they are considered fatal.
// Sends a response back to the sender if length of response returned by the handler > 0.
func (s *NetworkServer) AppRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	requestID uint32,
	deadline time.Time,
	request []byte,
) error {
	var req syncpb.Request
	if err := proto.Unmarshal(request, &req); err != nil {
		s.log.Debug(
			"failed to unmarshal AppRequest",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Int("requestLen", len(request)),
			zap.Error(err),
		)
		return nil
	}
	s.log.Debug(
		"processing AppRequest from node",
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)

	// bufferedDeadline is half the time till actual deadline so that the message has a
	// reasonable chance of completing its processing and sending the response to the peer.
	timeTillDeadline := time.Until(deadline)
	bufferedDeadline := time.Now().Add(timeTillDeadline / 2)

	// check if we have enough time to handle this request.
	// TODO danlaine: Do we need this? Why?
	if time.Until(bufferedDeadline) < minRequestHandlingDuration {
		// Drop the request if we already missed the deadline to respond.
		s.log.Info(
			"deadline to process AppRequest has expired, skipping",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	ctx, cancel := context.WithDeadline(ctx, bufferedDeadline)
	defer cancel()

	var err error
	switch req := req.GetMessage().(type) {
	case *syncpb.Request_ChangeProofRequest:
		err = s.HandleChangeProofRequest(ctx, nodeID, requestID, req.ChangeProofRequest)
	case *syncpb.Request_RangeProofRequest:
		err = s.HandleRangeProofRequest(ctx, nodeID, requestID, req.RangeProofRequest)
	default:
		s.log.Debug(
			"unknown AppRequest type",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Int("requestLen", len(request)),
			zap.String("requestType", fmt.Sprintf("%T", req)),
		)
		return nil
	}

	if err != nil && !isTimeout(err) {
		// log unexpected errors instead of returning them, since they are fatal.
		s.log.Warn(
			"unexpected error handling AppRequest",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Error(err),
		)
	}
	return nil
}

// isTimeout returns true if err is a timeout from a context cancellation
// or a context cancellation over grpc.
func isTimeout(err error) bool {
	// handle grpc wrapped DeadlineExceeded
	if e, ok := status.FromError(err); ok {
		if e.Code() == codes.DeadlineExceeded {
			return true
		}
	}
	// otherwise, check for context.DeadlineExceeded directly
	return errors.Is(err, context.DeadlineExceeded)
}

// Generates a change proof and sends it to [nodeID].
func (s *NetworkServer) HandleChangeProofRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	requestID uint32,
	req *syncpb.ChangeProofRequest,
) error {
	if req.BytesLimit == 0 ||
		req.KeyLimit == 0 ||
		len(req.StartRoot) != hashing.HashLen ||
		len(req.EndRoot) != hashing.HashLen ||
		(len(req.End) > 0 && bytes.Compare(req.Start, req.End) > 0) {
		s.log.Debug(
			"dropping invalid change proof request",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("req", req),
		)
		return nil // dropping request
	}

	// override limit if it is greater than maxKeyValuesLimit
	keyLimit := req.KeyLimit
	if keyLimit > maxKeyValuesLimit {
		keyLimit = maxKeyValuesLimit
	}
	bytesLimit := int(req.BytesLimit)
	if bytesLimit > maxByteSizeLimit {
		bytesLimit = maxByteSizeLimit
	}

	// attempt to get a proof within the bytes limit
	for keyLimit > 0 {
		startRoot, err := ids.ToID(req.StartRoot)
		if err != nil {
			return err
		}
		endRoot, err := ids.ToID(req.EndRoot)
		if err != nil {
			return err
		}
		changeProof, err := s.db.GetChangeProof(ctx, startRoot, endRoot, req.Start, req.End, int(keyLimit))
		if err != nil {
			// handle expected errors so clients cannot cause servers to spam warning logs.
			if errors.Is(err, merkledb.ErrRootIDNotPresent) || errors.Is(err, merkledb.ErrStartRootNotFound) {
				s.log.Debug(
					"dropping invalid change proof request",
					zap.Stringer("nodeID", nodeID),
					zap.Uint32("requestID", requestID),
					zap.Stringer("req", req),
					zap.Error(err),
				)
				return nil // dropping request
			}
			return err
		}

		proofBytes, err := merkledb.Codec.EncodeChangeProof(merkledb.Version, changeProof)
		if err != nil {
			return err
		}
		if len(proofBytes) < bytesLimit {
			return s.appSender.SendAppResponse(ctx, nodeID, requestID, proofBytes)
		}
		// the proof size was too large, try to shrink it
		keyLimit = uint32(len(changeProof.KeyChanges)) / 2
	}
	return ErrMinProofSizeIsTooLarge
}

// Generates a range proof and sends it to [nodeID].
// TODO danlaine how should we handle context cancellation?
func (s *NetworkServer) HandleRangeProofRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	requestID uint32,
	req *syncpb.RangeProofRequest,
) error {
	if req.BytesLimit == 0 ||
		req.KeyLimit == 0 ||
		len(req.Root) != hashing.HashLen ||
		(len(req.End) > 0 && bytes.Compare(req.Start, req.End) > 0) {
		s.log.Debug(
			"dropping invalid range proof request",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("req", req),
		)
		return nil // dropping request
	}

	// override limit if it is greater than maxKeyValuesLimit
	keyLimit := req.KeyLimit
	if keyLimit > maxKeyValuesLimit {
		keyLimit = maxKeyValuesLimit
	}
	bytesLimit := int(req.BytesLimit)
	if bytesLimit > maxByteSizeLimit {
		bytesLimit = maxByteSizeLimit
	}
	for keyLimit > 0 {
		root, err := ids.ToID(req.Root)
		if err != nil {
			return err
		}
		rangeProof, err := s.db.GetRangeProofAtRoot(ctx, root, req.Start, req.End, int(keyLimit))
		if err != nil {
			// handle expected errors so clients cannot cause servers to spam warning logs.
			if errors.Is(err, merkledb.ErrRootIDNotPresent) {
				s.log.Debug(
					"dropping invalid range proof request",
					zap.Stringer("nodeID", nodeID),
					zap.Uint32("requestID", requestID),
					zap.Stringer("req", req),
					zap.Error(err),
				)
				return nil // dropping request
			}
			return err
		}

		proofBytes, err := merkledb.Codec.EncodeRangeProof(merkledb.Version, rangeProof)
		if err != nil {
			return err
		}
		if len(proofBytes) < bytesLimit {
			return s.appSender.SendAppResponse(ctx, nodeID, requestID, proofBytes)
		}
		// the proof size was too large, try to shrink it
		keyLimit = uint32(len(rangeProof.KeyValues)) / 2
	}
	return ErrMinProofSizeIsTooLarge
}
