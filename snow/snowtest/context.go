// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowtest

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/MetalBlockchain/metalgo/api/metrics"
	"github.com/MetalBlockchain/metalgo/chains/atomic"
	"github.com/MetalBlockchain/metalgo/database/memdb"
	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/snow"
	"github.com/MetalBlockchain/metalgo/snow/validators"
	"github.com/MetalBlockchain/metalgo/snow/validators/validatorstest"
	"github.com/MetalBlockchain/metalgo/upgrade/upgradetest"
	"github.com/MetalBlockchain/metalgo/utils/constants"
	"github.com/MetalBlockchain/metalgo/utils/crypto/bls/signer/localsigner"
	"github.com/MetalBlockchain/metalgo/utils/logging"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/warp"
)

var (
	PChainID    = constants.PlatformChainID
	XChainID    = ids.GenerateTestID()
	CChainID    = ids.GenerateTestID()
	AVAXAssetID = ids.GenerateTestID()

	errMissing = errors.New("missing")

	_ snow.Acceptor = noOpAcceptor{}
)

type noOpAcceptor struct{}

func (noOpAcceptor) Accept(*snow.ConsensusContext, ids.ID, []byte) error {
	return nil
}

func ConsensusContext(ctx *snow.Context) *snow.ConsensusContext {
	return &snow.ConsensusContext{
		Context:        ctx,
		PrimaryAlias:   ctx.ChainID.String(),
		Registerer:     prometheus.NewRegistry(),
		BlockAcceptor:  noOpAcceptor{},
		TxAcceptor:     noOpAcceptor{},
		VertexAcceptor: noOpAcceptor{},
	}
}

func Context(tb testing.TB, chainID ids.ID) *snow.Context {
	require := require.New(tb)

	secretKey, err := localsigner.New()
	require.NoError(err)
	publicKey := secretKey.PublicKey()

	memory := atomic.NewMemory(memdb.New())
	sharedMemory := memory.NewSharedMemory(chainID)

	aliaser := ids.NewAliaser()
	require.NoError(aliaser.Alias(PChainID, "P"))
	require.NoError(aliaser.Alias(PChainID, PChainID.String()))
	require.NoError(aliaser.Alias(XChainID, "X"))
	require.NoError(aliaser.Alias(XChainID, XChainID.String()))
	require.NoError(aliaser.Alias(CChainID, "C"))
	require.NoError(aliaser.Alias(CChainID, CChainID.String()))

	validatorState := &validatorstest.State{
		GetMinimumHeightF: func(context.Context) (uint64, error) {
			return 0, nil
		},
		GetCurrentHeightF: func(context.Context) (uint64, error) {
			return 0, nil
		},
		GetSubnetIDF: func(_ context.Context, chainID ids.ID) (ids.ID, error) {
			switch chainID {
			case PChainID, XChainID, CChainID:
				return constants.PrimaryNetworkID, nil
			default:
				return ids.Empty, errMissing
			}
		},
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			return map[ids.NodeID]*validators.GetValidatorOutput{}, nil
		},
	}

	return &snow.Context{
		NetworkID:       constants.UnitTestID,
		SubnetID:        constants.PrimaryNetworkID,
		ChainID:         chainID,
		NodeID:          ids.GenerateTestNodeID(),
		PublicKey:       publicKey,
		NetworkUpgrades: upgradetest.GetConfig(upgradetest.Latest),

		XChainID:    XChainID,
		CChainID:    CChainID,
		AVAXAssetID: AVAXAssetID,

		Log:          logging.NoLog{},
		SharedMemory: sharedMemory,
		BCLookup:     aliaser,
		Metrics:      metrics.NewPrefixGatherer(),

		WarpSigner: warp.NewSigner(secretKey, constants.UnitTestID, chainID),

		ValidatorState: validatorState,
		ChainDataDir:   tb.TempDir(),
	}
}
