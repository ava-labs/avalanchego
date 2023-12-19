// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowtest

import (
	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/prometheus/client_golang/prometheus"
)

var _ snow.Acceptor = noOpAcceptor{}

type noOpAcceptor struct{}

func (noOpAcceptor) Accept(*snow.ConsensusContext, ids.ID, []byte) error {
	return nil
}

func EmptyContext() *snow.Context {
	sk, err := bls.NewSecretKey()
	if err != nil {
		panic(err)
	}
	pk := bls.PublicFromSecretKey(sk)
	return &snow.Context{
		NetworkID:    0,
		SubnetID:     ids.Empty,
		ChainID:      ids.Empty,
		NodeID:       ids.EmptyNodeID,
		PublicKey:    pk,
		Log:          logging.NoLog{},
		BCLookup:     ids.NewAliaser(),
		Metrics:      metrics.NewOptionalGatherer(),
		ChainDataDir: "",
	}
}

func EmptyConsensusContext() *snow.ConsensusContext {
	return &snow.ConsensusContext{
		Context:             EmptyContext(),
		Registerer:          prometheus.NewRegistry(),
		AvalancheRegisterer: prometheus.NewRegistry(),
		BlockAcceptor:       noOpAcceptor{},
		TxAcceptor:          noOpAcceptor{},
		VertexAcceptor:      noOpAcceptor{},
	}
}
