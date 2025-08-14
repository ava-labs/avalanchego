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
var walLocation = "temp.txt"

type Engine struct {
	common.Handler
	health.Checker

	epoch *simplex.Epoch
	blockDeserializer *blockDeserializer
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
	wal, err := wal.New(walLocation)
	if err != nil {
		return nil, err
	}

	lastBlock, _, found := storage.retrieve(storage.Height() - 1)
	if !found {
		return nil, fmt.Errorf("couldn't find last block at height %d", storage.Height()-1)
	}

	blockTracker := newBlockTracker(lastBlock)

	blockBuilder := &BlockBuilder{
		vm: config.VM,
		blockTracker: blockTracker,
		log: config.Log,
	}

	epochConfig := simplex.EpochConfig{
		MaxProposalWait: maxProposalWaitTime,
		MaxRebroadcastWait:    maxRebroadcastWait,
		QCDeserializer: qcDeserializer,
		Logger: config.Log,
		ID: config.Ctx.NodeID[:], 
		Signer: &signer,
		Verifier: &verifier,
		BlockDeserializer: blockDeserializer,
		SignatureAggregator: signatureAggregator,
		Comm: comm,
		Storage: storage,
		WAL: wal,
		BlockBuilder: blockBuilder,
		Epoch: 0, // 0 for now, but we would get the epoch from the metadata associated with the latest block
		StartTime: time.Now(),
		ReplicationEnabled: true,
	}

	epoch, err := simplex.NewEpoch(epochConfig)
	if err != nil {
		return nil, err
	}

	return &Engine{
		epoch: epoch,
		blockDeserializer: blockDeserializer,
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
	case msg.GetBlockProposal() != nil :
			return blockProposalFromP2P(context.TODO(), msg.GetBlockProposal(), e.blockDeserializer)
	case msg.GetEmptyNotarization() != nil:
			return emptyNotarizationFromP2P(msg.GetEmptyNotarization())
	case msg.GetVote() != nil:
			vote, err := p2pVoteToSimplexVote(msg.GetVote())
			if err != nil {
				return nil, fmt.Errorf("failed to convert p2p vote to simplex vote: %w", err)
			}
			return &simplex.Message{
				VoteMessage: &vote,
			}, nil
	case msg.GetEmptyVote() != nil:
			emptyVote := msg.GetEmptyVote()
			return 
	default:
		return nil, fmt.Errorf("unknown message type")
	}
}