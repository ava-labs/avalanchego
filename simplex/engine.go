// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/ids"

	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/simplex"
	"github.com/ava-labs/simplex/wal"
)

var _ common.Engine = (*Engine)(nil)
var maxProposalWaitTime = time.Second * 2
var maxRebroadcastWait = time.Second * 2

type Engine struct {
	common.Handler
	health.Checker

	epoch              *simplex.Epoch
	blockDeserializer  *blockDeserializer
	quorumDeserializer *QCDeserializer
}

func NewEngine(ctx context.Context, config *Config) (*Engine, error) {
	signer, verifier := NewBLSAuth(config)
	qcDeserializer := &QCDeserializer{
		verifier: &verifier,
	}
	blockDeserializer := &blockDeserializer{
		parser: config.VM,
	}
	signatureAggregator := &SignatureAggregator{
		verifier: &verifier,
	}
	comm, err := NewComm(config)
	if err != nil {
		return nil, err
	}

	storage, err := newStorage(ctx, config, qcDeserializer, &blockTracker{})
	if err != nil {
		return nil, err
	}
	wal, err := wal.New(config.WalLocation)
	if err != nil {
		return nil, err
	}

	lastBlock, _, found := storage.retrieve(storage.Height() - 1)
	if !found {
		return nil, fmt.Errorf("couldn't find last block at height %d", storage.Height()-1)
	}

	blockTracker := newBlockTracker(lastBlock)

	blockBuilder := &BlockBuilder{
		vm:           config.VM,
		blockTracker: blockTracker,
		log:          config.Log,
	}

	epochConfig := simplex.EpochConfig{
		MaxProposalWait:     maxProposalWaitTime,
		MaxRebroadcastWait:  maxRebroadcastWait,
		QCDeserializer:      qcDeserializer,
		Logger:              config.Log,
		ID:                  config.Ctx.NodeID[:],
		Signer:              &signer,
		Verifier:            &verifier,
		BlockDeserializer:   blockDeserializer,
		SignatureAggregator: signatureAggregator,
		Comm:                comm,
		Storage:             storage,
		WAL:                 wal,
		BlockBuilder:        blockBuilder,
		Epoch:               0, // 0 for now, but we would get the epoch from the metadata associated with the latest block
		StartTime:           time.Now(),
		ReplicationEnabled:  true,
	}

	epoch, err := simplex.NewEpoch(epochConfig)
	if err != nil {
		return nil, err
	}

	return &Engine{
		epoch:              epoch,
		blockDeserializer:  blockDeserializer,
		quorumDeserializer: qcDeserializer,
	}, nil
}

func (e *Engine) Start(_ context.Context, _ uint32) error {
	return e.epoch.Start()
}

func (e *Engine) SimplexMessage(ctx context.Context, nodeID ids.NodeID, msg *p2p.Simplex) error {
	simplexMsg, err := e.p2pToSimplexMessage(msg)
	if err != nil {
		return fmt.Errorf("failed to convert p2p message to simplex message: %w", err)
	}

	return e.epoch.HandleMessage(simplexMsg, nodeID[:])
}

func (e *Engine) p2pToSimplexMessage(msg *p2p.Simplex) (*simplex.Message, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil message")
	}

	switch {
	case msg.GetBlockProposal() != nil:
		return blockProposalFromP2P(context.TODO(), msg.GetBlockProposal(), e.blockDeserializer)
	case msg.GetEmptyNotarization() != nil:
		return emptyNotarizationMessageFromP2P(msg.GetEmptyNotarization(), e.quorumDeserializer)
	case msg.GetVote() != nil:
		return voteFromP2P(msg.GetVote())
	case msg.GetEmptyVote() != nil:
		return emptyVoteFromP2P(msg.GetEmptyVote())
	case msg.GetNotarization() != nil:
		return notarizationMessageFromP2P(msg.GetNotarization(), e.quorumDeserializer)
	case msg.GetFinalizeVote() != nil:
		return finalizeVoteFromP2P(msg.GetFinalizeVote(), e.quorumDeserializer)
	case msg.GetFinalization() != nil:
		return finalizationMessageFromP2P(msg.GetFinalization(), e.quorumDeserializer)
	case msg.GetReplicationRequest() != nil:
		return replicationRequestFromP2P(msg.GetReplicationRequest()), nil
	case msg.GetReplicationResponse() != nil:
		return replicationResponseFromP2P(context.TODO(), msg.GetReplicationResponse(), e.blockDeserializer, e.quorumDeserializer)
	default:
		return nil, fmt.Errorf("unknown message type")
	}
}
