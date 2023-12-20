// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var errUnexpectedStakerTx = errors.New("unexpected stakerTx type ")

func getStakerColdAttributes(chain Chain, stakerID ids.ID) (*StakerColdAttributes, error) {
	stakerTx, _, err := chain.GetTx(stakerID)
	if err != nil {
		return nil, fmt.Errorf("failed to get next staker %s: %w", stakerID, err)
	}
	switch uStakerTx := stakerTx.Unsigned.(type) {
	case txs.ValidatorTx:
		var pop *signer.ProofOfPossession
		if staker, ok := uStakerTx.(*txs.AddPermissionlessValidatorTx); ok {
			if s, ok := staker.Signer.(*signer.ProofOfPossession); ok {
				pop = s
			}
		}

		return &StakerColdAttributes{
			Stake:                  uStakerTx.Stake(),
			Outputs:                uStakerTx.Outputs(),
			Shares:                 uStakerTx.Shares(),
			ValidationRewardsOwner: uStakerTx.ValidationRewardsOwner(),
			DelegationRewardsOwner: uStakerTx.DelegationRewardsOwner(),
			ProofOfPossession:      pop,
		}, nil
	case txs.DelegatorTx:
		return &StakerColdAttributes{
			Stake:        uStakerTx.Stake(),
			Outputs:      uStakerTx.Outputs(),
			RewardsOwner: uStakerTx.RewardsOwner(),
		}, nil
	default:
		return nil, fmt.Errorf("%w, txType %T", errUnexpectedStakerTx, uStakerTx)
	}
}

func getChainSubnet(chain Chain, chainID ids.ID) (ids.ID, error) {
	chainTx, _, err := chain.GetTx(chainID)
	if err != nil {
		return ids.Empty, fmt.Errorf(
			"problem retrieving blockchain %q: %w",
			chainID,
			err,
		)
	}
	blockChain, ok := chainTx.Unsigned.(*txs.CreateChainTx)
	if !ok {
		return ids.Empty, fmt.Errorf("%q is not a blockchain", chainID)
	}
	return blockChain.SubnetID, nil
}
