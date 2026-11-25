// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

var (
	_ validators.State = (*IsolatedL1)(nil)

	errUnknownChain  = errors.New("unknown chain")
	errUnknownSubnet = errors.New("unknown subnet")
	ErrWarpDisabled  = errors.New("warp verification is disabled in isolated L1 mode")
)

// IsolatedL1 runs one chain without the P-chain.
//
// The chain must already be created on the P-chain, because the chain ID is
// the ID of its CreateChainTx and the P-chain rejects that tx once the subnet
// is converted. To rejoin the P-chain, issue a ConvertSubnetToL1Tx with
// validators that store its history, wait for it to be accepted, and restart every validator
// without the isolated L1 flags.
type IsolatedL1 struct {
	Chain      ChainParameters
	Validators map[ids.NodeID]*validators.GetValidatorOutput
	Height     uint64
}

// RegisterValidators adds the validators to [vdrs] for the L1's subnet only.
func (l *IsolatedL1) RegisterValidators(vdrs validators.Manager) error {
	for nodeID, vdr := range l.Validators {
		if err := vdrs.AddStaker(l.Chain.SubnetID, nodeID, vdr.PublicKey, l.Chain.ID, vdr.Weight); err != nil {
			return err
		}
	}
	return nil
}

func (l *IsolatedL1) GetMinimumHeight(context.Context) (uint64, error) {
	return l.Height, nil
}

func (l *IsolatedL1) GetCurrentHeight(context.Context) (uint64, error) {
	return l.Height, nil
}

// GetSubnetID only knows the L1 chain. Warp verification calls this for the
// source chain of every message, so the L1 rejects messages from all other
// chains.
func (l *IsolatedL1) GetSubnetID(_ context.Context, chainID ids.ID) (ids.ID, error) {
	if chainID != l.Chain.ID {
		return ids.Empty, fmt.Errorf("%w: %s", errUnknownChain, chainID)
	}
	return l.Chain.SubnetID, nil
}

func (*IsolatedL1) GetWarpValidatorSets(context.Context, uint64) (map[ids.ID]validators.WarpSet, error) {
	return nil, ErrWarpDisabled
}

func (l *IsolatedL1) GetValidatorSet(_ context.Context, _ uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	if subnetID != l.Chain.SubnetID {
		return nil, fmt.Errorf("%w: %s", errUnknownSubnet, subnetID)
	}
	return maps.Clone(l.Validators), nil
}

func (l *IsolatedL1) GetCurrentValidatorSet(ctx context.Context, subnetID ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
	vdrs, err := l.GetValidatorSet(ctx, l.Height, subnetID)
	if err != nil {
		return nil, 0, err
	}
	out := make(map[ids.ID]*validators.GetCurrentValidatorOutput, len(vdrs))
	for nodeID, vdr := range vdrs {
		// Stand-in until the P-chain assigns real validation IDs on rejoin.
		validationID := ids.ID(hashing.ComputeHash256Array(nodeID[:]))
		out[validationID] = &validators.GetCurrentValidatorOutput{
			ValidationID: validationID,
			NodeID:       nodeID,
			PublicKey:    vdr.PublicKey,
			Weight:       vdr.Weight,
			IsActive:     true,
		}
	}
	return out, l.Height, nil
}
