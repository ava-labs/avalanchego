// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"context"

	"github.com/ethereum/go-ethereum/log"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ GossipHandler            = NoopMempoolGossipHandler{}
	_ RequestHandler           = NoopRequestHandler{}
	_ CrossChainRequestHandler = NoopCrossChainRequestHandler{}
)

// GossipHandler handles incoming gossip messages
type GossipHandler interface {
	HandleTxs(nodeID ids.NodeID, msg TxsGossip) error
}

type NoopMempoolGossipHandler struct{}

func (NoopMempoolGossipHandler) HandleTxs(nodeID ids.NodeID, _ TxsGossip) error {
	log.Debug("dropping unexpected Txs message", "peerID", nodeID)
	return nil
}

// RequestHandler interface handles incoming requests from peers
// Must have methods in format of handleType(context.Context, ids.NodeID, uint32, request Type) error
// so that the Request object of relevant Type can invoke its respective handle method
// on this struct.
// Also see GossipHandler for implementation style.
type RequestHandler interface {
	HandleTrieLeafsRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, leafsRequest LeafsRequest) ([]byte, error)
	HandleBlockRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, blockRequest BlockRequest) ([]byte, error)
	HandleCodeRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, codeRequest CodeRequest) ([]byte, error)
	HandleSignatureRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, signatureRequest SignatureRequest) ([]byte, error)
}

// ResponseHandler handles response for a sent request
// Only one of OnResponse or OnFailure is called for a given requestID, not both
type ResponseHandler interface {
	// OnResponse is invoked when the peer responded to a request
	OnResponse(response []byte) error
	// OnFailure is invoked when there was a failure in processing a request
	OnFailure() error
}

type NoopRequestHandler struct{}

func (NoopRequestHandler) HandleTrieLeafsRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, leafsRequest LeafsRequest) ([]byte, error) {
	return nil, nil
}

func (NoopRequestHandler) HandleBlockRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, request BlockRequest) ([]byte, error) {
	return nil, nil
}

func (NoopRequestHandler) HandleCodeRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, codeRequest CodeRequest) ([]byte, error) {
	return nil, nil
}

func (NoopRequestHandler) HandleSignatureRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, signatureRequest SignatureRequest) ([]byte, error) {
	return nil, nil
}

// CrossChainRequestHandler interface handles incoming requests from another chain
type CrossChainRequestHandler interface {
	HandleEthCallRequest(ctx context.Context, requestingchainID ids.ID, requestID uint32, ethCallRequest EthCallRequest) ([]byte, error)
}

type NoopCrossChainRequestHandler struct{}

func (NoopCrossChainRequestHandler) HandleEthCallRequest(ctx context.Context, requestingchainID ids.ID, requestID uint32, ethCallRequest EthCallRequest) ([]byte, error) {
	return nil, nil
}
