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
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/x/merkledb"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
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
)

var (
	ErrMinProofSizeIsTooLarge = errors.New("cannot generate any proof within the requested limit")

	errInvalidBytesLimit    = errors.New("bytes limit must be greater than 0")
	errInvalidKeyLimit      = errors.New("key limit must be greater than 0")
	errInvalidStartRootHash = fmt.Errorf("start root hash must have length %d", hashing.HashLen)
	errInvalidEndRootHash   = fmt.Errorf("end root hash must have length %d", hashing.HashLen)
	errInvalidStartKey      = errors.New("start key is Nothing but has value")
	errInvalidEndKey        = errors.New("end key is Nothing but has value")
	errInvalidBounds        = errors.New("start key is greater than end key")
	errInvalidRootHash      = fmt.Errorf("root hash must have length %d", hashing.HashLen)
)

type NetworkServer struct {
	appSender common.AppSender // Used to respond to peer requests via AppResponse.
	db        DB
	log       logging.Logger
}

func NewNetworkServer(appSender common.AppSender, db DB, log logging.Logger) *NetworkServer {
	return &NetworkServer{
		appSender: appSender,
		db:        db,
		log:       log,
	}
}

// AppRequest is called by avalanchego -> VM when there is an incoming AppRequest from a peer.
// Returns a non-nil error iff we fail to send an app message. This is a fatal error.
// Sends a response back to the sender if length of response returned by the handler > 0.
func (s *NetworkServer) AppRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	requestID uint32,
	deadline time.Time,
	request []byte,
) error {
	var req pb.Request
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
	case *pb.Request_ChangeProofRequest:
		err = s.HandleChangeProofRequest(ctx, nodeID, requestID, req.ChangeProofRequest)
	case *pb.Request_RangeProofRequest:
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

	if err != nil {
		if errors.Is(err, errAppSendFailed) {
			return err
		}

		if !isTimeout(err) {
			// log unexpected errors instead of returning them, since they are fatal.
			s.log.Warn(
				"unexpected error handling AppRequest",
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Error(err),
			)
		}
	}
	return nil
}

func maybeBytesToMaybe(mb *pb.MaybeBytes) maybe.Maybe[[]byte] {
	if mb != nil && !mb.IsNothing {
		return maybe.Some(mb.Value)
	}
	return maybe.Nothing[[]byte]()
}

// Generates a change proof and sends it to [nodeID].
// If [errAppSendFailed] is returned, this should be considered fatal.
func (s *NetworkServer) HandleChangeProofRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	requestID uint32,
	req *pb.SyncGetChangeProofRequest,
) error {
	if err := validateChangeProofRequest(req); err != nil {
		s.log.Debug(
			"dropping invalid change proof request",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("req", req),
			zap.Error(err),
		)
		return nil // dropping request
	}

	// override limits if they exceed caps
	var (
		keyLimit   = math.Min(req.KeyLimit, maxKeyValuesLimit)
		bytesLimit = math.Min(int(req.BytesLimit), maxByteSizeLimit)
		start      = maybeBytesToMaybe(req.StartKey)
		end        = maybeBytesToMaybe(req.EndKey)
	)

	startRoot, err := ids.ToID(req.StartRootHash)
	if err != nil {
		return err
	}

	endRoot, err := ids.ToID(req.EndRootHash)
	if err != nil {
		return err
	}

	for keyLimit > 0 {
		changeProof, err := s.db.GetChangeProof(ctx, startRoot, endRoot, start, end, int(keyLimit))
		if err != nil {
			if !errors.Is(err, merkledb.ErrInsufficientHistory) {
				return err
			}

			// [s.db] doesn't have sufficient history to generate change proof.
			// Generate a range proof for the end root ID instead.
			proofBytes, err := getRangeProof(
				ctx,
				s.db,
				&pb.SyncGetRangeProofRequest{
					RootHash:   req.EndRootHash,
					StartKey:   req.StartKey,
					EndKey:     req.EndKey,
					KeyLimit:   req.KeyLimit,
					BytesLimit: req.BytesLimit,
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
				return err
			}

			if err := s.appSender.SendAppResponse(ctx, nodeID, requestID, proofBytes); err != nil {
				s.log.Fatal(
					"failed to send app response",
					zap.Stringer("nodeID", nodeID),
					zap.Uint32("requestID", requestID),
					zap.Int("responseLen", len(proofBytes)),
					zap.Error(err),
				)
				return fmt.Errorf("%w: %w", errAppSendFailed, err)
			}
			return nil
		}

		// We generated a change proof. See if it's small enough.
		proofBytes, err := proto.Marshal(&pb.SyncGetChangeProofResponse{
			Response: &pb.SyncGetChangeProofResponse_ChangeProof{
				ChangeProof: changeProof.ToProto(),
			},
		})
		if err != nil {
			return err
		}

		if len(proofBytes) < bytesLimit {
			if err := s.appSender.SendAppResponse(ctx, nodeID, requestID, proofBytes); err != nil {
				s.log.Fatal(
					"failed to send app response",
					zap.Stringer("nodeID", nodeID),
					zap.Uint32("requestID", requestID),
					zap.Int("responseLen", len(proofBytes)),
					zap.Error(err),
				)
				return fmt.Errorf("%w: %w", errAppSendFailed, err)
			}
			return nil
		}

		// The proof was too large. Try to shrink it.
		keyLimit = uint32(len(changeProof.KeyChanges)) / 2
	}
	return ErrMinProofSizeIsTooLarge
}

// Generates a range proof and sends it to [nodeID].
// If [errAppSendFailed] is returned, this should be considered fatal.
func (s *NetworkServer) HandleRangeProofRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	requestID uint32,
	req *pb.SyncGetRangeProofRequest,
) error {
	if err := validateRangeProofRequest(req); err != nil {
		s.log.Debug(
			"dropping invalid range proof request",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("req", req),
			zap.Error(err),
		)
		return nil // drop request
	}

	// override limits if they exceed caps
	req.KeyLimit = math.Min(req.KeyLimit, maxKeyValuesLimit)
	req.BytesLimit = math.Min(req.BytesLimit, maxByteSizeLimit)

	proofBytes, err := getRangeProof(
		ctx,
		s.db,
		req,
		func(rangeProof *merkledb.RangeProof) ([]byte, error) {
			return proto.Marshal(rangeProof.ToProto())
		},
	)
	if err != nil {
		return err
	}
	if err := s.appSender.SendAppResponse(ctx, nodeID, requestID, proofBytes); err != nil {
		s.log.Fatal(
			"failed to send app response",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Int("responseLen", len(proofBytes)),
			zap.Error(err),
		)
		return fmt.Errorf("%w: %w", errAppSendFailed, err)
	}
	return nil
}

// Get the range proof specified by [req].
// If the generated proof is too large, the key limit is reduced
// and the proof is regenerated. This process is repeated until
// the proof is smaller than [req.BytesLimit].
// When a sufficiently small proof is generated, returns it.
// If no sufficiently small proof can be generated, returns [ErrMinProofSizeIsTooLarge].
// TODO improve range proof generation so we don't need to iteratively
// reduce the key limit.
func getRangeProof(
	ctx context.Context,
	db DB,
	req *pb.SyncGetRangeProofRequest,
	marshalFunc func(*merkledb.RangeProof) ([]byte, error),
) ([]byte, error) {
	root, err := ids.ToID(req.RootHash)
	if err != nil {
		return nil, err
	}

	keyLimit := int(req.KeyLimit)

	for keyLimit > 0 {
		rangeProof, err := db.GetRangeProofAtRoot(
			ctx,
			root,
			maybeBytesToMaybe(req.StartKey),
			maybeBytesToMaybe(req.EndKey),
			keyLimit,
		)
		if err != nil {
			if errors.Is(err, merkledb.ErrInsufficientHistory) {
				return nil, nil // drop request
			}
			return nil, err
		}

		proofBytes, err := marshalFunc(rangeProof)
		if err != nil {
			return nil, err
		}

		if len(proofBytes) < int(req.BytesLimit) {
			return proofBytes, nil
		}

		// The proof was too large. Try to shrink it.
		keyLimit = len(rangeProof.KeyValues) / 2
	}
	return nil, ErrMinProofSizeIsTooLarge
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

// Returns nil iff [req] is well-formed.
func validateChangeProofRequest(req *pb.SyncGetChangeProofRequest) error {
	switch {
	case req.BytesLimit == 0:
		return errInvalidBytesLimit
	case req.KeyLimit == 0:
		return errInvalidKeyLimit
	case len(req.StartRootHash) != hashing.HashLen:
		return errInvalidStartRootHash
	case len(req.EndRootHash) != hashing.HashLen:
		return errInvalidEndRootHash
	case req.StartKey != nil && req.StartKey.IsNothing && len(req.StartKey.Value) > 0:
		return errInvalidStartKey
	case req.EndKey != nil && req.EndKey.IsNothing && len(req.EndKey.Value) > 0:
		return errInvalidEndKey
	case req.StartKey != nil && req.EndKey != nil && !req.StartKey.IsNothing &&
		!req.EndKey.IsNothing && bytes.Compare(req.StartKey.Value, req.EndKey.Value) > 0:
		return errInvalidBounds
	default:
		return nil
	}
}

// Returns nil iff [req] is well-formed.
func validateRangeProofRequest(req *pb.SyncGetRangeProofRequest) error {
	switch {
	case req.BytesLimit == 0:
		return errInvalidBytesLimit
	case req.KeyLimit == 0:
		return errInvalidKeyLimit
	case len(req.RootHash) != ids.IDLen:
		return errInvalidRootHash
	case req.StartKey != nil && req.StartKey.IsNothing && len(req.StartKey.Value) > 0:
		return errInvalidStartKey
	case req.EndKey != nil && req.EndKey.IsNothing && len(req.EndKey.Value) > 0:
		return errInvalidEndKey
	case req.StartKey != nil && req.EndKey != nil && !req.StartKey.IsNothing &&
		!req.EndKey.IsNothing && bytes.Compare(req.StartKey.Value, req.EndKey.Value) > 0:
		return errInvalidBounds
	default:
		return nil
	}
}
