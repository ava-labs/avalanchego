// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pchain

import (
	"context"
	"fmt"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p/wallet"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

const (
	// validationDuration outlasts any test run and stays below the maximum
	// stake duration of every test network.
	validationDuration = 72 * time.Hour
	// delegationFee is 10%, above the minimum fee of every test network.
	delegationFee = reward.PercentDenominator / 10
)

// IssueAddValidatorTx issues a primary network validator that stakes the
// minimum amount for validationDuration with a fresh BLS key and a 10%
// delegation fee.
func IssueAddValidatorTx(
	ctx context.Context,
	pWallet wallet.Wallet,
	issuingNode Node,
	nodeID ids.NodeID,
	validationRewardAddr ids.ShortID,
	delegationRewardAddr ids.ShortID,
) (*platform.Tx, error) {
	minStake, _, err := issuingNode.Client.GetMinStake(ctx, constants.PrimaryNetworkID)
	if err != nil {
		return nil, fmt.Errorf("getting minimum stake from %s: %w", issuingNode, err)
	}
	chainTime, err := issuingNode.Client.GetTimestamp(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting chain time from %s: %w", issuingNode, err)
	}
	sk, err := localsigner.New()
	if err != nil {
		return nil, fmt.Errorf("creating validator signer: %w", err)
	}
	pop, err := signer.NewProofOfPossession(sk)
	if err != nil {
		return nil, fmt.Errorf("creating validator proof of possession: %w", err)
	}

	tx, err := pWallet.IssueAddPermissionlessValidatorTx(
		&platform.SubnetValidator{
			Validator: platform.Validator{
				NodeID: nodeID,
				End:    uint64(chainTime.Add(validationDuration).Unix()),
				Wght:   minStake,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		pop,
		pWallet.Builder().Context().AVAXAssetID,
		&secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{validationRewardAddr}},
		&secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{delegationRewardAddr}},
		delegationFee,
		common.WithContext(ctx),
	)
	if err != nil {
		return tx, fmt.Errorf("issuing validator %s through %s: %w", nodeID, issuingNode, err)
	}
	return tx, nil
}

// VerifyValidator checks the committed transaction bytes and the validator
// they describe on every node.
func VerifyValidator(tc tests.TestContext, tx *platform.Tx, nodes []Node) {
	require.NotEmpty(tc, nodes)
	require.IsType(tc, &platform.AddPermissionlessValidatorTx{}, tx.Unsigned)
	validatorTx := tx.Unsigned.(*platform.AddPermissionlessValidatorTx)
	require.IsType(tc, &signer.ProofOfPossession{}, validatorTx.Signer)
	var (
		nodeID          = validatorTx.Validator.NodeID
		pop             = validatorTx.Signer.(*signer.ProofOfPossession)
		validationOwner = clientOwner(tc, validatorTx.ValidatorRewardsOwner)
		delegationOwner = clientOwner(tc, validatorTx.DelegatorRewardsOwner)
	)
	tc.By("verifying validator "+tx.ID().String()+" on every node", func() {
		ctx := tc.DefaultContext()
		for _, node := range nodes {
			msg := fmt.Sprintf("transaction %s on %s", tx.ID(), node)
			txBytes, err := node.Client.GetTx(ctx, tx.ID())
			require.NoError(tc, err, msg)
			require.Equal(tc, tx.Bytes(), txBytes, msg)

			validators, err := node.Client.GetCurrentValidators(ctx, constants.PrimaryNetworkID, []ids.NodeID{nodeID})
			require.NoError(tc, err, msg)
			require.Len(tc, validators, 1, msg)
			vdr := validators[0]
			require.Equal(tc, tx.ID(), vdr.TxID, msg)
			require.Equal(tc, validatorTx.Validator.Wght, vdr.Weight, msg)
			require.Equal(tc, validatorTx.Validator.End, vdr.EndTime, msg)
			require.Equal(tc, float32(validatorTx.DelegationShares)/(reward.PercentDenominator/100), vdr.DelegationFee, msg)
			require.NotNil(tc, vdr.Signer, msg)
			require.Equal(tc, pop.PublicKey, vdr.Signer.PublicKey, msg)
			require.Equal(tc, pop.ProofOfPossession, vdr.Signer.ProofOfPossession, msg)
			require.Equal(tc, validationOwner, vdr.ValidationRewardOwner, msg)
			require.Equal(tc, delegationOwner, vdr.DelegationRewardOwner, msg)
		}
	})
}

// AddValidator issues a validator, waits for every node to commit it, and
// verifies the result on every node.
func AddValidator(
	tc tests.TestContext,
	pWallet wallet.Wallet,
	issuingNode Node,
	nodes []Node,
	nodeID ids.NodeID,
	validationRewardAddr ids.ShortID,
	delegationRewardAddr ids.ShortID,
) *platform.Tx {
	tx, err := IssueAddValidatorTx(tc.DefaultContext(), pWallet, issuingNode, nodeID, validationRewardAddr, delegationRewardAddr)
	require.NoError(tc, err)
	WaitForTxCommitted(tc, nodes, tx.ID())
	VerifyValidator(tc, tx, nodes)
	return tx
}

// IssueAddDelegatorTx issues a primary network delegation of the minimum
// stake to nodeID. A zero end delegates until the validator's end time.
func IssueAddDelegatorTx(
	ctx context.Context,
	pWallet wallet.Wallet,
	issuingNode Node,
	nodeID ids.NodeID,
	end time.Time,
	rewardAddr ids.ShortID,
	options ...common.Option,
) (*platform.Tx, error) {
	_, minStake, err := issuingNode.Client.GetMinStake(ctx, constants.PrimaryNetworkID)
	if err != nil {
		return nil, fmt.Errorf("getting minimum stake from %s: %w", issuingNode, err)
	}
	endTime := uint64(end.Unix())
	if end.IsZero() {
		validators, err := issuingNode.Client.GetCurrentValidators(ctx, constants.PrimaryNetworkID, []ids.NodeID{nodeID})
		if err != nil {
			return nil, fmt.Errorf("getting validator %s from %s: %w", nodeID, issuingNode, err)
		}
		if len(validators) != 1 {
			return nil, fmt.Errorf("expected one current validator %s from %s but got %d", nodeID, issuingNode, len(validators))
		}
		endTime = validators[0].EndTime
	}

	tx, err := pWallet.IssueAddPermissionlessDelegatorTx(
		&platform.SubnetValidator{
			Validator: platform.Validator{
				NodeID: nodeID,
				End:    endTime,
				Wght:   minStake,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		pWallet.Builder().Context().AVAXAssetID,
		&secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{rewardAddr}},
		append([]common.Option{common.WithContext(ctx)}, options...)...,
	)
	if err != nil {
		return tx, fmt.Errorf("issuing delegation to %s through %s: %w", nodeID, issuingNode, err)
	}
	return tx, nil
}

// GetDelegator returns the delegator created by txID from one node's public validator view.
func GetDelegator(ctx context.Context, node Node, nodeID ids.NodeID, txID ids.ID) (platformvm.ClientDelegator, error) {
	validators, err := node.Client.GetCurrentValidators(ctx, constants.PrimaryNetworkID, []ids.NodeID{nodeID})
	if err != nil {
		return platformvm.ClientDelegator{}, fmt.Errorf("getting validator %s from %s: %w", nodeID, node, err)
	}
	if len(validators) != 1 {
		return platformvm.ClientDelegator{}, fmt.Errorf("expected one validator %s from %s but got %d", nodeID, node, len(validators))
	}
	for _, delegator := range validators[0].Delegators {
		if delegator.TxID == txID {
			return delegator, nil
		}
	}
	return platformvm.ClientDelegator{}, fmt.Errorf("delegator transaction %s not found on validator %s from %s", txID, nodeID, node)
}

// VerifyDelegator checks the committed transaction bytes and the delegation
// they describe on every node.
func VerifyDelegator(tc tests.TestContext, tx *platform.Tx, nodes []Node) {
	require.NotEmpty(tc, nodes)
	require.IsType(tc, &platform.AddPermissionlessDelegatorTx{}, tx.Unsigned)
	var (
		delegatorTx = tx.Unsigned.(*platform.AddPermissionlessDelegatorTx)
		nodeID      = delegatorTx.Validator.NodeID
		rewardOwner = clientOwner(tc, delegatorTx.DelegationRewardsOwner)
	)
	tc.By("verifying delegation "+tx.ID().String()+" on every node", func() {
		ctx := tc.DefaultContext()
		for _, node := range nodes {
			msg := fmt.Sprintf("transaction %s on %s", tx.ID(), node)
			txBytes, err := node.Client.GetTx(ctx, tx.ID())
			require.NoError(tc, err, msg)
			require.Equal(tc, tx.Bytes(), txBytes, msg)

			delegator, err := GetDelegator(ctx, node, nodeID, tx.ID())
			require.NoError(tc, err, msg)
			require.Equal(tc, delegatorTx.Validator.Wght, delegator.Weight, msg)
			require.Equal(tc, delegatorTx.Validator.End, delegator.EndTime, msg)
			require.Equal(tc, rewardOwner, delegator.RewardOwner, msg)
		}
	})
}

// AddDelegator issues a delegation, waits for every node to commit it, and
// verifies the result on every node. A zero end delegates until the
// validator's end time.
func AddDelegator(
	tc tests.TestContext,
	pWallet wallet.Wallet,
	issuingNode Node,
	nodes []Node,
	nodeID ids.NodeID,
	end time.Time,
	rewardAddr ids.ShortID,
) *platform.Tx {
	tx, err := IssueAddDelegatorTx(tc.DefaultContext(), pWallet, issuingNode, nodeID, end, rewardAddr)
	require.NoError(tc, err)
	WaitForTxCommitted(tc, nodes, tx.ID())
	VerifyDelegator(tc, tx, nodes)
	return tx
}

// clientOwner returns the API representation of a transaction's reward owner.
func clientOwner(tc tests.TestContext, owner fx.Owner) *platformvm.ClientOwner {
	require.IsType(tc, &secp256k1fx.OutputOwners{}, owner)
	outputOwners := owner.(*secp256k1fx.OutputOwners)
	return &platformvm.ClientOwner{
		Locktime:  outputOwners.Locktime,
		Threshold: outputOwners.Threshold,
		Addresses: outputOwners.Addrs,
	}
}
