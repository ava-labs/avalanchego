// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"time"

	"simplex"
)

func createSimplexInstance(config *Config) (*simplex.Epoch, *BlockBuilder, *WALInterceptor, error) {
	signer, verifier := NewBLSAuth(config)

	comm := NewComm(config)
	wal, err := newWal(config.Ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	storage, err := newStorage(config, verifier)
	if err != nil {
		return nil, nil, nil, err
	}

	bb := &BlockBuilder{
		Logger: config.Ctx.Log,
		VM:     config.VM,
	}

	conf := simplex.EpochConfig{
		BlockBuilder:        bb,
		Signer:              &signer,
		ID:                  config.Ctx.NodeID[:],
		Verifier:            verifier,
		Logger:              config.Ctx.Log,
		QCDeserializer:      QCDeserializer(verifier),
		SignatureAggregator: SignatureAggregator(verifier),
		Comm:                comm,
		WAL:                 wal,
		Storage:             storage,
		StartTime:           time.Now(),
		BlockDeserializer: &blockDeserializer{
			vm: config.VM,
		},
		MaxProposalWait:    config.Ctx.Params.MaxProposalWait,
		MaxRebroadcastWait: config.Ctx.Params.MaxRebroadcastWait,
	}

	simplex, err := simplex.NewEpoch(conf)
	if err != nil {
		return nil, nil, nil, err
	}

	return simplex, bb, wal, nil
}
