// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

type DecisionsTxBuilder interface {
	NewCreateChainTx(
		subnetID ids.ID, // ID of the subnet that validates the new chain
		genesisData []byte, // Byte repr. of genesis state of the new chain
		vmID ids.ID, // VM this chain runs
		fxIDs []ids.ID, // fxs this chain supports
		chainName string, // Name of the chain
		keys []*crypto.PrivateKeySECP256K1R, // Keys to sign the tx
		changeAddr ids.ShortID, // Address to send change to, if there is any
	) (*signed.Tx, error)

	NewCreateSubnetTx(
		threshold uint32, // [threshold] of [ownerAddrs] needed to manage this subnet
		ownerAddrs []ids.ShortID, // control addresses for the new subnet
		keys []*crypto.PrivateKeySECP256K1R, // pay the fee
		changeAddr ids.ShortID, // Address to send change to, if there is any
	) (*signed.Tx, error)
}

func (b *builder) NewCreateChainTx(
	subnetID ids.ID, // ID of the subnet that validates the new chain
	genesisData []byte, // Byte repr. of genesis state of the new chain
	vmID ids.ID, // VM this chain runs
	fxIDs []ids.ID, // fxs this chain supports
	chainName string, // Name of the chain
	keys []*crypto.PrivateKeySECP256K1R, // Keys to sign the tx
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*signed.Tx, error) {
	timestamp := b.state.GetTimestamp()
	createBlockchainTxFee := GetCreateBlockchainTxFee(b.cfg, timestamp)
	ins, outs, _, signers, err := b.Stake(keys, 0, createBlockchainTxFee, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	subnetAuth, subnetSigners, err := b.Authorize(b.state, subnetID, keys)
	if err != nil {
		return nil, fmt.Errorf("couldn't authorize tx's subnet restrictions: %w", err)
	}
	signers = append(signers, subnetSigners)

	// Sort the provided fxIDs
	ids.SortIDs(fxIDs)

	// Create the tx
	utx := &unsigned.CreateChainTx{
		BaseTx: unsigned.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		SubnetID:    subnetID,
		ChainName:   chainName,
		VMID:        vmID,
		FxIDs:       fxIDs,
		GenesisData: genesisData,
		SubnetAuth:  subnetAuth,
	}
	tx, err := signed.NewSigned(utx, unsigned.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewCreateSubnetTx(
	threshold uint32, // [threshold] of [ownerAddrs] needed to manage this subnet
	ownerAddrs []ids.ShortID, // control addresses for the new subnet
	keys []*crypto.PrivateKeySECP256K1R, // pay the fee
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*signed.Tx, error) {
	timestamp := b.state.GetTimestamp()
	createSubnetTxFee := GetCreateSubnetTxFee(b.cfg, timestamp)
	ins, outs, _, signers, err := b.Stake(keys, 0, createSubnetTxFee, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Sort control addresses
	ids.SortShortIDs(ownerAddrs)

	// Create the tx
	utx := &unsigned.CreateSubnetTx{
		BaseTx: unsigned.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		Owner: &secp256k1fx.OutputOwners{
			Threshold: threshold,
			Addrs:     ownerAddrs,
		},
	}
	tx, err := signed.NewSigned(utx, unsigned.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}
