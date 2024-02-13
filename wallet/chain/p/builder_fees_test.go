// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p/mocks"

	stdcontext "context"
)

var testKeys = secp256k1.TestKeys()

// These tests create and sign a tx, then verify that utxos included
// in the tx are exactly necessary to pay fees for it

func TestBaseTx(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	be := mocks.NewMockBuilderBackend(ctrl)

	var (
		utxosKey              = testKeys[1]
		utxoAddr              = utxosKey.PublicKey().Address()
		utxos, avaxAssetID, _ = testUTXOsList(utxosKey)

		outputsToMove = []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 7 * units.Avax,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{utxosKey.PublicKey().Address()},
				},
			},
		}}

		kc  = secp256k1fx.NewKeychain(utxosKey)
		sbe = mocks.NewMockSignerBackend(ctrl)
		s   = NewSigner(kc, sbe)
	)

	// set expectations
	be.EXPECT().AVAXAssetID().Return(avaxAssetID).AnyTimes()
	be.EXPECT().NetworkID().Return(constants.MainnetID).AnyTimes()
	be.EXPECT().UTXOs(gomock.Any(), constants.PlatformChainID).Return(utxos, nil).AnyTimes()
	for _, utxo := range utxos {
		sbe.EXPECT().GetUTXO(gomock.Any(), gomock.Any(), utxo.InputID()).Return(utxo, nil).AnyTimes()
	}

	b := NewBuilder(set.Of(utxoAddr), be)

	expectedFee := units.MilliAvax
	be.EXPECT().CreateSubnetTxFee().Return(expectedFee)

	utx, err := b.NewBaseTx(outputsToMove)
	require.NoError(err)

	_, err = s.SignUnsigned(stdcontext.Background(), utx)
	require.NoError(err)

	ins := utx.Ins
	outs := utx.Outs
	require.Len(ins, 2)
	require.Len(outs, 2)
	require.Equal(expectedFee+outputsToMove[0].Out.Amount(), ins[0].In.Amount()+ins[1].In.Amount()-outs[0].Out.Amount())
	require.Equal(outputsToMove[0], outs[1])
}

func TestAddSubnetValidatorTx(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	be := mocks.NewMockBuilderBackend(ctrl)

	var (
		subnetAuthKey         = testKeys[0]
		utxosKey              = testKeys[1]
		subnetAuthAddr        = subnetAuthKey.PublicKey().Address()
		utxoAddr              = utxosKey.PublicKey().Address()
		utxos, avaxAssetID, _ = testUTXOsList(utxosKey)
		subnetID              = ids.GenerateTestID()
		subnetOwner           = fx.Owner(
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{subnetAuthKey.PublicKey().Address()},
			},
		)
		subnetValidator = &txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: ids.GenerateTestNodeID(),
				End:    uint64(time.Now().Add(time.Hour).Unix()),
			},
			Subnet: subnetID,
		}

		kc  = secp256k1fx.NewKeychain(utxosKey)
		sbe = mocks.NewMockSignerBackend(ctrl)
		s   = NewSigner(kc, sbe)
	)

	// set expectations
	be.EXPECT().AVAXAssetID().Return(avaxAssetID).AnyTimes()
	be.EXPECT().NetworkID().Return(constants.MainnetID).AnyTimes()
	be.EXPECT().UTXOs(gomock.Any(), constants.PlatformChainID).Return(utxos, nil).AnyTimes()
	be.EXPECT().GetSubnetOwner(gomock.Any(), subnetID).Return(subnetOwner, nil).AnyTimes()
	for _, utxo := range utxos {
		sbe.EXPECT().GetUTXO(gomock.Any(), gomock.Any(), utxo.InputID()).Return(utxo, nil).AnyTimes()
	}
	sbe.EXPECT().GetSubnetOwner(gomock.Any(), subnetID).Return(subnetOwner, nil).AnyTimes()

	b := NewBuilder(set.Of(utxoAddr, subnetAuthAddr), be)

	expectedFee := 1234 * units.MicroAvax
	be.EXPECT().AddSubnetValidatorFee().Return(expectedFee)

	utx, err := b.NewAddSubnetValidatorTx(subnetValidator)
	require.NoError(err)

	_, err = s.SignUnsigned(stdcontext.Background(), utx)
	require.NoError(err)

	ins := utx.Ins
	outs := utx.Outs
	require.Len(ins, 1)
	require.Len(outs, 1)
	require.Equal(expectedFee, ins[0].In.Amount()-outs[0].Out.Amount())
}

func TestRemoveSubnetValidatorTx(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	be := mocks.NewMockBuilderBackend(ctrl)

	var (
		subnetAuthKey         = testKeys[0]
		utxosKey              = testKeys[1]
		subnetAuthAddr        = subnetAuthKey.PublicKey().Address()
		utxoAddr              = utxosKey.PublicKey().Address()
		utxos, avaxAssetID, _ = testUTXOsList(utxosKey)
		subnetID              = ids.GenerateTestID()

		subnetOwner = fx.Owner(
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{subnetAuthKey.PublicKey().Address()},
			},
		)

		kc  = secp256k1fx.NewKeychain(utxosKey)
		sbe = mocks.NewMockSignerBackend(ctrl)
		s   = NewSigner(kc, sbe)
	)

	// set expectations
	be.EXPECT().AVAXAssetID().Return(avaxAssetID).AnyTimes()
	be.EXPECT().NetworkID().Return(constants.MainnetID).AnyTimes()
	be.EXPECT().UTXOs(gomock.Any(), constants.PlatformChainID).Return(utxos, nil).AnyTimes()
	be.EXPECT().GetSubnetOwner(gomock.Any(), subnetID).Return(subnetOwner, nil).AnyTimes()
	for _, utxo := range utxos {
		sbe.EXPECT().GetUTXO(gomock.Any(), gomock.Any(), utxo.InputID()).Return(utxo, nil).AnyTimes()
	}
	sbe.EXPECT().GetSubnetOwner(gomock.Any(), subnetID).Return(subnetOwner, nil).AnyTimes()

	b := NewBuilder(set.Of(utxoAddr, subnetAuthAddr), be)

	expectedFee := 23 * units.MilliAvax
	be.EXPECT().BaseTxFee().Return(expectedFee)

	utx, err := b.NewRemoveSubnetValidatorTx(
		ids.GenerateTestNodeID(),
		subnetID,
	)
	require.NoError(err)

	_, err = s.SignUnsigned(stdcontext.Background(), utx)
	require.NoError(err)

	ins := utx.Ins
	outs := utx.Outs
	require.Len(ins, 2)
	require.Len(outs, 1)
	require.Equal(expectedFee, ins[0].In.Amount()+ins[1].In.Amount()-outs[0].Out.Amount())
}

func TestCreateChainTx(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	be := mocks.NewMockBuilderBackend(ctrl)

	var (
		subnetAuthKey  = testKeys[0]
		utxosKey       = testKeys[1]
		subnetAuthAddr = subnetAuthKey.PublicKey().Address()
		utxoAddr       = utxosKey.PublicKey().Address()
		subnetID       = ids.GenerateTestID()
		genesisBytes   = []byte{'a', 'b', 'c'}
		vmID           = ids.GenerateTestID()
		fxIDs          = []ids.ID{ids.GenerateTestID()}
		chainName      = "dummyChain"
		subnetOwner    = fx.Owner(
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{subnetAuthKey.PublicKey().Address()},
			},
		)
		utxos, avaxAssetID, _ = testUTXOsList(utxosKey)

		kc  = secp256k1fx.NewKeychain(utxosKey)
		sbe = mocks.NewMockSignerBackend(ctrl)
		s   = NewSigner(kc, sbe)
	)

	// set expectations
	be.EXPECT().GetSubnetOwner(gomock.Any(), subnetID).Return(subnetOwner, nil).AnyTimes()
	be.EXPECT().AVAXAssetID().Return(avaxAssetID).AnyTimes()
	be.EXPECT().NetworkID().Return(constants.MainnetID).AnyTimes()
	be.EXPECT().UTXOs(gomock.Any(), constants.PlatformChainID).Return(utxos, nil).AnyTimes()
	for _, utxo := range utxos {
		sbe.EXPECT().GetUTXO(gomock.Any(), gomock.Any(), utxo.InputID()).Return(utxo, nil).AnyTimes()
	}
	sbe.EXPECT().GetSubnetOwner(gomock.Any(), subnetID).Return(subnetOwner, nil).AnyTimes()

	b := NewBuilder(set.Of(utxoAddr, subnetAuthAddr), be)

	expectedFee := 1 * units.Avax
	be.EXPECT().CreateBlockchainTxFee().Return(expectedFee)

	utx, err := b.NewCreateChainTx(
		subnetID,
		genesisBytes,
		vmID,
		fxIDs,
		chainName,
	)
	require.NoError(err)

	_, err = s.SignUnsigned(stdcontext.Background(), utx)
	require.NoError(err)

	ins := utx.Ins
	outs := utx.Outs
	require.Len(ins, 2)
	require.Len(outs, 1)
	require.Equal(expectedFee, ins[0].In.Amount()+ins[1].In.Amount()-outs[0].Out.Amount())
}

func TestCreateSubnetTx(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	be := mocks.NewMockBuilderBackend(ctrl)

	var (
		subnetAuthKey         = testKeys[0]
		utxosKey              = testKeys[1]
		utxoAddr              = utxosKey.PublicKey().Address()
		utxos, avaxAssetID, _ = testUTXOsList(utxosKey)
		subnetOwner           = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				subnetAuthKey.Address(),
			},
		}

		kc  = secp256k1fx.NewKeychain(utxosKey)
		sbe = mocks.NewMockSignerBackend(ctrl)
		s   = NewSigner(kc, sbe)
	)

	// setup expectations
	be.EXPECT().AVAXAssetID().Return(avaxAssetID).AnyTimes()
	be.EXPECT().NetworkID().Return(constants.MainnetID).AnyTimes()
	be.EXPECT().UTXOs(gomock.Any(), constants.PlatformChainID).Return(utxos, nil).AnyTimes()
	for _, utxo := range utxos {
		sbe.EXPECT().GetUTXO(gomock.Any(), gomock.Any(), utxo.InputID()).Return(utxo, nil).AnyTimes()
	}

	b := NewBuilder(set.Of(utxoAddr), be)

	expectedFee := 1234 * units.MilliAvax
	be.EXPECT().CreateSubnetTxFee().Return(expectedFee)

	utx, err := b.NewCreateSubnetTx(subnetOwner)
	require.NoError(err)

	_, err = s.SignUnsigned(stdcontext.Background(), utx)
	require.NoError(err)

	ins := utx.Ins
	outs := utx.Outs
	require.Len(ins, 2)
	require.Len(outs, 1)
	require.Equal(expectedFee, ins[0].In.Amount()+ins[1].In.Amount()-outs[0].Out.Amount())
}

func TestTransferSubnetOwnershipTx(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	be := mocks.NewMockBuilderBackend(ctrl)

	var (
		subnetAuthKey         = testKeys[0]
		utxosKey              = testKeys[1]
		utxoAddr              = utxosKey.PublicKey().Address()
		utxos, avaxAssetID, _ = testUTXOsList(utxosKey)
		subnetID              = ids.GenerateTestID()
		subnetOwner           = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				subnetAuthKey.Address(),
			},
		}

		kc  = secp256k1fx.NewKeychain(utxosKey)
		sbe = mocks.NewMockSignerBackend(ctrl)
		s   = NewSigner(kc, sbe)
	)

	// setup expectations
	be.EXPECT().AVAXAssetID().Return(avaxAssetID).AnyTimes()
	be.EXPECT().NetworkID().Return(constants.MainnetID).AnyTimes()
	be.EXPECT().GetSubnetOwner(gomock.Any(), subnetID).Return(subnetOwner, nil).AnyTimes()
	be.EXPECT().UTXOs(gomock.Any(), constants.PlatformChainID).Return(utxos, nil).AnyTimes()
	for _, utxo := range utxos {
		sbe.EXPECT().GetUTXO(gomock.Any(), gomock.Any(), utxo.InputID()).Return(utxo, nil).AnyTimes()
	}
	sbe.EXPECT().GetSubnetOwner(gomock.Any(), gomock.Any()).Return(subnetOwner, nil).AnyTimes()

	b := NewBuilder(set.Of(utxoAddr, subnetAuthKey.Address()), be)

	expectedFee := units.MilliAvax
	be.EXPECT().BaseTxFee().Return(expectedFee)

	utx, err := b.NewTransferSubnetOwnershipTx(
		subnetID,
		subnetOwner,
	)
	require.NoError(err)

	_, err = s.SignUnsigned(stdcontext.Background(), utx)
	require.NoError(err)

	ins := utx.Ins
	outs := utx.Outs
	require.Len(ins, 1)
	require.Len(outs, 1)
	require.Equal(expectedFee, ins[0].In.Amount()-outs[0].Out.Amount())
}

func TestImportTx(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	be := mocks.NewMockBuilderBackend(ctrl)

	var (
		utxosKey              = testKeys[1]
		utxoAddr              = utxosKey.PublicKey().Address()
		sourceChainID         = ids.GenerateTestID()
		utxos, avaxAssetID, _ = testUTXOsList(utxosKey)

		importKey = testKeys[0]
		importTo  = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				importKey.Address(),
			},
		}

		kc  = secp256k1fx.NewKeychain(utxosKey)
		sbe = mocks.NewMockSignerBackend(ctrl)
		s   = NewSigner(kc, sbe)
	)

	importedUtxo := utxos[0]
	utxos = utxos[1:]

	// setup expectations
	be.EXPECT().AVAXAssetID().Return(avaxAssetID).AnyTimes()
	be.EXPECT().NetworkID().Return(constants.MainnetID).AnyTimes()
	be.EXPECT().UTXOs(gomock.Any(), sourceChainID).Return([]*avax.UTXO{importedUtxo}, nil).AnyTimes()
	be.EXPECT().UTXOs(gomock.Any(), constants.PlatformChainID).Return(utxos, nil).AnyTimes()
	sbe.EXPECT().GetUTXO(gomock.Any(), gomock.Any(), importedUtxo.InputID()).Return(importedUtxo, nil).AnyTimes()
	for _, utxo := range utxos {
		sbe.EXPECT().GetUTXO(gomock.Any(), gomock.Any(), utxo.InputID()).Return(utxo, nil).AnyTimes()
	}

	b := NewBuilder(set.Of(utxoAddr), be)

	expectedFee := units.Avax
	be.EXPECT().BaseTxFee().Return(expectedFee)

	utx, err := b.NewImportTx(
		sourceChainID,
		importTo,
	)
	require.NoError(err)

	_, err = s.SignUnsigned(stdcontext.Background(), utx)
	require.NoError(err)

	ins := utx.Ins
	outs := utx.Outs
	importedIns := utx.ImportedInputs
	require.Len(ins, 1)
	require.Len(importedIns, 1)
	require.Len(outs, 1)
	require.Equal(expectedFee, importedIns[0].In.Amount()+ins[0].In.Amount()-outs[0].Out.Amount())
}

func TestExportTx(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	be := mocks.NewMockBuilderBackend(ctrl)

	var (
		utxosKey              = testKeys[1]
		utxoAddr              = utxosKey.PublicKey().Address()
		subnetID              = ids.GenerateTestID()
		utxos, avaxAssetID, _ = testUTXOsList(utxosKey)

		exportedOutputs = []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 7 * units.Avax,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{utxosKey.PublicKey().Address()},
				},
			},
		}}

		kc  = secp256k1fx.NewKeychain(utxosKey)
		sbe = mocks.NewMockSignerBackend(ctrl)
		s   = NewSigner(kc, sbe)
	)

	// setup expectation
	be.EXPECT().AVAXAssetID().Return(avaxAssetID).AnyTimes()
	be.EXPECT().NetworkID().Return(constants.MainnetID).AnyTimes()
	be.EXPECT().UTXOs(gomock.Any(), constants.PlatformChainID).Return(utxos, nil).AnyTimes()
	for _, utxo := range utxos {
		sbe.EXPECT().GetUTXO(gomock.Any(), gomock.Any(), utxo.InputID()).Return(utxo, nil).AnyTimes()
	}

	b := NewBuilder(set.Of(utxoAddr), be)

	expectedFee := 1234 * units.MilliAvax
	be.EXPECT().BaseTxFee().Return(expectedFee)

	utx, err := b.NewExportTx(
		subnetID,
		exportedOutputs,
	)
	require.NoError(err)

	_, err = s.SignUnsigned(stdcontext.Background(), utx)
	require.NoError(err)

	ins := utx.Ins
	outs := utx.Outs
	require.Len(ins, 2)
	require.Len(outs, 1)
	require.Equal(expectedFee+exportedOutputs[0].Out.Amount(), ins[0].In.Amount()+ins[1].In.Amount()-outs[0].Out.Amount())
	require.Equal(utx.ExportedOutputs, exportedOutputs)
}

func TestTransformSubnetTx(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	be := mocks.NewMockBuilderBackend(ctrl)

	var (
		subnetAuthKey                     = testKeys[0]
		utxosKey                          = testKeys[1]
		subnetAuthAddr                    = subnetAuthKey.PublicKey().Address()
		utxoAddr                          = utxosKey.PublicKey().Address()
		subnetID                          = ids.GenerateTestID()
		utxos, avaxAssetID, subnetAssetID = testUTXOsList(utxosKey)
		subnetOwner                       = fx.Owner(
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{subnetAuthKey.PublicKey().Address()},
			},
		)

		kc  = secp256k1fx.NewKeychain(utxosKey)
		sbe = mocks.NewMockSignerBackend(ctrl)
		s   = NewSigner(kc, sbe)

		initialSupply = 40 * units.MegaAvax
		maxSupply     = 100 * units.MegaAvax
	)

	be.EXPECT().GetSubnetOwner(gomock.Any(), subnetID).Return(subnetOwner, nil).AnyTimes()
	be.EXPECT().AVAXAssetID().Return(avaxAssetID).AnyTimes()
	be.EXPECT().NetworkID().Return(constants.MainnetID).AnyTimes()
	be.EXPECT().UTXOs(gomock.Any(), constants.PlatformChainID).Return(utxos, nil).AnyTimes()
	for _, utxo := range utxos {
		sbe.EXPECT().GetUTXO(gomock.Any(), gomock.Any(), utxo.InputID()).Return(utxo, nil).AnyTimes()
	}
	sbe.EXPECT().GetSubnetOwner(gomock.Any(), subnetID).Return(subnetOwner, nil).AnyTimes()

	b := NewBuilder(set.Of(utxoAddr, subnetAuthAddr), be)

	expectedFee := 1234 * units.MilliAvax
	be.EXPECT().TransformSubnetTxFee().Return(expectedFee)

	utx, err := b.NewTransformSubnetTx(
		subnetID,
		subnetAssetID,
		initialSupply,                 // initial supply
		maxSupply,                     // max supply
		reward.PercentDenominator,     // min consumption rate
		reward.PercentDenominator,     // max consumption rate
		1,                             // min validator stake
		100*units.MegaAvax,            // max validator stake
		time.Second,                   // min stake duration
		365*24*time.Hour,              // max stake duration
		0,                             // min delegation fee
		1,                             // min delegator stake
		5,                             // max validator weight factor
		.80*reward.PercentDenominator, // uptime requirement
	)
	require.NoError(err)

	_, err = s.SignUnsigned(stdcontext.Background(), utx)
	require.NoError(err)

	ins := utx.Ins
	outs := utx.Outs
	require.Len(ins, 3)
	require.Len(outs, 2)
	require.Equal(maxSupply-initialSupply, ins[0].In.Amount()-outs[0].Out.Amount())
	require.Equal(expectedFee, ins[1].In.Amount()+ins[2].In.Amount()-outs[1].Out.Amount())
}

func TestAddPermissionlessValidatorTx(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	be := mocks.NewMockBuilderBackend(ctrl)

	var (
		rewardKey              = testKeys[0]
		utxosKey               = testKeys[1]
		rewardAddr             = rewardKey.PublicKey().Address()
		utxoAddr               = utxosKey.PublicKey().Address()
		utxos, avaxAssetID, _  = testUTXOsList(utxosKey)
		validationRewardsOwner = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				rewardKey.Address(),
			},
		}
		delegationRewardsOwner = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				rewardKey.Address(),
			},
		}

		kc  = secp256k1fx.NewKeychain(utxosKey)
		sbe = mocks.NewMockSignerBackend(ctrl)
		s   = NewSigner(kc, sbe)
	)

	sk, err := bls.NewSecretKey()
	require.NoError(err)

	// setup expectations
	be.EXPECT().AVAXAssetID().Return(avaxAssetID).AnyTimes()
	be.EXPECT().NetworkID().Return(constants.MainnetID).AnyTimes()
	be.EXPECT().UTXOs(gomock.Any(), constants.PlatformChainID).Return(utxos, nil).AnyTimes()
	for _, utxo := range utxos {
		sbe.EXPECT().GetUTXO(gomock.Any(), gomock.Any(), utxo.InputID()).Return(utxo, nil).AnyTimes()
	}

	b := NewBuilder(set.Of(utxoAddr, rewardAddr), be)

	expectedFee := 1234 * units.MilliAvax
	be.EXPECT().AddPrimaryNetworkValidatorFee().Return(expectedFee)

	utx, err := b.NewAddPermissionlessValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: ids.GenerateTestNodeID(),
				End:    uint64(time.Now().Add(time.Hour).Unix()),
				Wght:   2 * units.Avax,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		signer.NewProofOfPossession(sk),
		avaxAssetID,
		validationRewardsOwner,
		delegationRewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(err)

	_, err = s.SignUnsigned(stdcontext.Background(), utx)
	require.NoError(err)

	ins := utx.Ins
	staked := utx.StakeOuts
	outs := utx.Outs
	require.Len(ins, 4)
	require.Len(staked, 2)
	require.Len(outs, 2)
	require.Equal(utx.Validator.Weight(), staked[0].Out.Amount()+staked[1].Out.Amount())
	require.Equal(expectedFee, ins[1].In.Amount()+ins[3].In.Amount()-outs[0].Out.Amount())
}

func TestAddPermissionlessDelegatorTx(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	be := mocks.NewMockBuilderBackend(ctrl)

	var (
		rewardKey             = testKeys[0]
		utxosKey              = testKeys[1]
		rewardAddr            = rewardKey.PublicKey().Address()
		utxoAddr              = utxosKey.PublicKey().Address()
		utxos, avaxAssetID, _ = testUTXOsList(utxosKey)
		rewardsOwner          = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				rewardKey.Address(),
			},
		}

		kc  = secp256k1fx.NewKeychain(utxosKey)
		sbe = mocks.NewMockSignerBackend(ctrl)
		s   = NewSigner(kc, sbe)
	)

	be.EXPECT().AVAXAssetID().Return(avaxAssetID).AnyTimes()
	be.EXPECT().NetworkID().Return(constants.MainnetID).AnyTimes()
	be.EXPECT().UTXOs(gomock.Any(), constants.PlatformChainID).Return(utxos, nil).AnyTimes()
	for _, utxo := range utxos {
		sbe.EXPECT().GetUTXO(gomock.Any(), gomock.Any(), utxo.InputID()).Return(utxo, nil).AnyTimes()
	}

	b := NewBuilder(set.Of(utxoAddr, rewardAddr), be)

	expectedFee := 1234 * units.MilliAvax
	be.EXPECT().AddPrimaryNetworkDelegatorFee().Return(expectedFee)

	utx, err := b.NewAddPermissionlessDelegatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: ids.GenerateTestNodeID(),
				End:    uint64(time.Now().Add(time.Hour).Unix()),
				Wght:   2 * units.Avax,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		avaxAssetID,
		rewardsOwner,
	)
	require.NoError(err)

	_, err = s.SignUnsigned(stdcontext.Background(), utx)
	require.NoError(err)

	ins := utx.Ins
	staked := utx.StakeOuts
	outs := utx.Outs
	require.Len(ins, 4)
	require.Len(staked, 2)
	require.Len(outs, 2)
	require.Equal(utx.Validator.Weight(), staked[0].Out.Amount()+staked[1].Out.Amount())
	require.Equal(expectedFee, ins[1].In.Amount()+ins[3].In.Amount()-outs[0].Out.Amount())
}

func testUTXOsList(utxosKey *secp256k1.PrivateKey) (
	[]*avax.UTXO,
	ids.ID, // avaxAssetID,
	ids.ID, // subnetAssetID
) {
	// Note: we avoid ids.GenerateTestNodeID here to make sure that UTXO IDs won't change
	// run by run. This simplifies checking what utxos are included in the built txs.
	utxosOffset := uint64(2024)

	var (
		avaxAssetID   = ids.Empty.Prefix(utxosOffset)
		subnetAssetID = ids.Empty.Prefix(utxosOffset + 1)
	)

	return []*avax.UTXO{
			{ // a small UTXO first, which  should not be enough to pay fees
				UTXOID: avax.UTXOID{
					TxID:        ids.Empty.Prefix(utxosOffset),
					OutputIndex: uint32(utxosOffset),
				},
				Asset: avax.Asset{ID: avaxAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 2 * units.MilliAvax,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Addrs:     []ids.ShortID{utxosKey.PublicKey().Address()},
						Threshold: 1,
					},
				},
			},
			{ // a locked, small UTXO
				UTXOID: avax.UTXOID{
					TxID:        ids.Empty.Prefix(utxosOffset + 1),
					OutputIndex: uint32(utxosOffset + 1),
				},
				Asset: avax.Asset{ID: avaxAssetID},
				Out: &stakeable.LockOut{
					Locktime: uint64(time.Now().Add(time.Hour).Unix()),
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt: 3 * units.MilliAvax,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{utxosKey.PublicKey().Address()},
						},
					},
				},
			},
			{ // a subnetAssetID denominated UTXO
				UTXOID: avax.UTXOID{
					TxID:        ids.Empty.Prefix(utxosOffset + 2),
					OutputIndex: uint32(utxosOffset + 2),
				},
				Asset: avax.Asset{ID: subnetAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 99 * units.MegaAvax,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Addrs:     []ids.ShortID{utxosKey.PublicKey().Address()},
						Threshold: 1,
					},
				},
			},
			{ // a locked, large UTXO
				UTXOID: avax.UTXOID{
					TxID:        ids.Empty.Prefix(utxosOffset + 3),
					OutputIndex: uint32(utxosOffset + 3),
				},
				Asset: avax.Asset{ID: avaxAssetID},
				Out: &stakeable.LockOut{
					Locktime: uint64(time.Now().Add(time.Hour).Unix()),
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt: 88 * units.Avax,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{utxosKey.PublicKey().Address()},
						},
					},
				},
			},
			{ // a large UTXO last, which should be enough to pay any fee by itself
				UTXOID: avax.UTXOID{
					TxID:        ids.Empty.Prefix(utxosOffset + 4),
					OutputIndex: uint32(utxosOffset + 4),
				},
				Asset: avax.Asset{ID: avaxAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 9 * units.Avax,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Addrs:     []ids.ShortID{utxosKey.PublicKey().Address()},
						Threshold: 1,
					},
				},
			},
		},
		avaxAssetID,
		subnetAssetID
}
