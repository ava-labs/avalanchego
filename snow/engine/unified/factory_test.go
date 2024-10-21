// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package unified_test

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/syncer"
	"github.com/ava-labs/avalanchego/snow/engine/unified"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/trace"

	avbootstrap "github.com/ava-labs/avalanchego/snow/engine/avalanche/bootstrap"
	smbootstrap "github.com/ava-labs/avalanchego/snow/engine/snowman/bootstrap"
)

func TestFactory(t *testing.T) {
	ctx := snowtest.Context(t, snowtest.PChainID)

	vals := validators.NewManager()
	metrics := prometheus.NewRegistry()

	snowConfig := snowman.Config{
		Validators: vals,
		Ctx: &snow.ConsensusContext{
			Registerer: metrics,
			Context:    ctx,
		},
	}

	bootConfig := smbootstrap.Config{
		Ctx: &snow.ConsensusContext{
			Registerer: metrics,
			Context:    ctx,
		},
	}

	var getServer mockStateSyncer
	var avaAncestorGetter mockStateSyncer

	ef := &unified.EngineFactory{
		TracingEnabled:    true,
		Tracer:            trace.Noop,
		GetServer:         &getServer,
		AvaAncestorGetter: &avaAncestorGetter,
		AvaMetrics:        metrics,
		AvaBootConfig: avbootstrap.Config{
			Ctx: &snow.ConsensusContext{
				Context:    ctx,
				Registerer: metrics,
			},
		},
		Logger:          ctx.Log,
		BootConfig:      bootConfig,
		SnowmanConfig:   snowConfig,
		StateSyncConfig: syncer.Config{},
	}

	snowman, err := ef.NewSnowman()
	require.NoError(t, err)
	require.NotNil(t, snowman)

	ss, err := ef.NewStateSyncer(func(_ context.Context, _ uint32) error {
		return nil
	})
	require.NoError(t, err)
	require.NotNil(t, ss)

	avaBoot, err := ef.NewAvalancheSyncer(func(_ context.Context, _ uint32) error {
		return nil
	})
	require.NoError(t, err)
	require.NotNil(t, avaBoot)

	bs, err := ef.NewSnowBootstrapper(func(_ context.Context, _ uint32) error {
		return nil
	})
	require.NoError(t, err)
	require.NotNil(t, bs)

	gs := ef.AllGetServer()
	require.Equal(t, gs, &getServer)

	ag := ef.NewAvalancheAncestorsGetter()
	require.Equal(t, ag, &avaAncestorGetter)
}
