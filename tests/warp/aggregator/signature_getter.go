// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package aggregator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"google.golang.org/protobuf/proto"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

const (
	initialRetryFetchSignatureDelay = 100 * time.Millisecond
	maxRetryFetchSignatureDelay     = 5 * time.Second
	retryBackoffFactor              = 2
)

var _ SignatureGetter = (*NetworkSignatureGetter)(nil)

// SignatureGetter defines the minimum network interface to perform signature aggregation
type SignatureGetter interface {
	// GetSignature attempts to fetch a BLS Signature from [nodeID] for [unsignedWarpMessage]
	GetSignature(ctx context.Context, nodeID ids.NodeID, unsignedWarpMessage *avalancheWarp.UnsignedMessage) (*bls.Signature, error)
}

type NetworkClient interface {
	SendSyncedAppRequest(ctx context.Context, nodeID ids.NodeID, message []byte) ([]byte, error)
}

// NetworkSignatureGetter fetches warp signatures on behalf of the
// aggregator using VM App-Specific Messaging
type NetworkSignatureGetter struct {
	client       NetworkClient
	networkCodec codec.Manager
}

func NewSignatureGetter(client NetworkClient, networkCodec codec.Manager) *NetworkSignatureGetter {
	return &NetworkSignatureGetter{
		client:       client,
		networkCodec: networkCodec,
	}
}

// GetSignature attempts to fetch a BLS Signature of [unsignedWarpMessage] from [nodeID] until it succeeds or receives an invalid response
//
// Note: this function will continue attempting to fetch the signature from [nodeID] until it receives an invalid value or [ctx] is cancelled.
// The caller is responsible to cancel [ctx] if it no longer needs to fetch this signature.
func (s *NetworkSignatureGetter) GetSignature(ctx context.Context, nodeID ids.NodeID, unsignedWarpMessage *avalancheWarp.UnsignedMessage) (*bls.Signature, error) {
	protoMsg := &sdk.SignatureRequest{Message: unsignedWarpMessage.Bytes()}
	protoBytes, err := proto.Marshal(protoMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal signature request: %w", err)
	}

	delay := initialRetryFetchSignatureDelay
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		signatureRes, err := s.client.SendSyncedAppRequest(ctx, nodeID, protoBytes)
		// If the client fails to retrieve a response perform an exponential backoff.
		// Note: it is up to the caller to ensure that [ctx] is eventually cancelled
		if err != nil {
			// Wait until the retry delay has elapsed before retrying.
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(delay)

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-timer.C:
			}

			// Exponential backoff.
			delay *= retryBackoffFactor
			if delay > maxRetryFetchSignatureDelay {
				delay = maxRetryFetchSignatureDelay
			}
			continue
		}
		var response sdk.SignatureResponse
		if err := proto.Unmarshal(signatureRes, &response); err != nil {
			return nil, fmt.Errorf("failed to unmarshal signature res: %w", err)
		}

		if len(response.Signature) == 0 {
			return nil, errors.New("received empty signature response")
		}
		blsSignature, err := bls.SignatureFromBytes(response.Signature[:])
		if err != nil {
			return nil, fmt.Errorf("failed to parse signature from res: %w", err)
		}
		return blsSignature, nil
	}
}
