// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/api/health"
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
		// Handle error (e.g., log it, return a zero value, etc.)
		return nil, err
	}

	return &Engine{
		epoch: epoch,
	}, nil
}

func (e *Engine) Start(_ context.Context, _ uint32) error {
	// Initialize the engine, set up necessary components, etc.
	return e.epoch.Start()
}