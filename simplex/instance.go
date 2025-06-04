// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"crypto/sha256"
	"fmt"
	"time"

	"simplex"
	"simplex/wal"
)

func createSimplexInstance(config *Config) (*simplex.Epoch, *BlockBuilder, error) {
	signer, verifier := NewBLSAuth(config)

	comm := NewComm(config)
	wal, err := wal.New(createWALFileName(config))
	if err != nil {
		return nil, nil, err
	}

	storage, err := newStorage(config, verifier)
	if err != nil {
		return nil, nil, err
	}

	bb := NewBlockBuilder(config.Ctx.Log)

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
		MaxProposalWait:    config.Ctx.Params.MaxProposalWait,
		MaxRebroadcastWait: config.Ctx.Params.MaxRebroadcastWait,
	}

	simplex, err := simplex.NewEpoch(conf)
	if err != nil {
		return nil, nil, err
	}

	return simplex, bb, nil
}

func createWALFileName(config *Config) string {
	h := sha256.New()
	h.Write(config.Ctx.NodeID[:])
	h.Write(config.Ctx.ChainID[:])
	walDigest := h.Sum(nil)
	return fmt.Sprintf("%x.wal", walDigest[:10])
}
