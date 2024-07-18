// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/fee"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const (
	intrinsicValidatorBandwidth = ids.NodeIDLen + // nodeID
		wrappers.LongLen + // start
		wrappers.LongLen + // end
		wrappers.LongLen // weight

	intrinsicSubnetValidatorBandwidth = intrinsicValidatorBandwidth + // validator
		ids.IDLen // subnetID

	intrinsicOutputBandwidth = ids.IDLen + // assetID
		wrappers.IntLen // output typeID

	intrinsicStakeableLockedOutputBandwidth = wrappers.LongLen + // locktime
		wrappers.IntLen // output typeID

	intrinsicSECP256k1FxOutputOwnersBandwidth = wrappers.LongLen + // locktime
		wrappers.IntLen + // threshold
		wrappers.IntLen // num addresses

	intrinsicSECP256k1FxOutputBandwidth = wrappers.LongLen + // amount
		intrinsicSECP256k1FxOutputOwnersBandwidth

	intrinsicInputBandwidth = ids.IDLen + // txID
		wrappers.IntLen + // output index
		ids.IDLen + // assetID
		wrappers.IntLen + // input typeID
		wrappers.IntLen // credential typeID

	intrinsicStakeableLockedInputBandwidth = wrappers.LongLen + // locktime
		wrappers.IntLen // input typeID

	intrinsicSECP256k1FxInputBandwidth = wrappers.LongLen + // amount
		wrappers.IntLen + // num indices
		wrappers.IntLen // num signatures

	intrinsicSECP256k1FxSignatureBandwidth = wrappers.IntLen + // signature index
		secp256k1.SignatureLen // signature length
)

var (
	_ txs.Visitor = (*dimensionsCalculator)(nil)

	IntrinsicAddPermissionlessValidatorTxComplexities = fee.Dimensions{
		fee.Bandwidth: IntrinsicBaseTxComplexities[fee.Bandwidth] +
			intrinsicValidatorBandwidth + // validator
			ids.IDLen + // subnetID
			wrappers.IntLen + // signer typeID
			wrappers.IntLen + // num stake outs
			wrappers.IntLen + // validator rewards typeID
			wrappers.IntLen + // delegator rewards typeID
			wrappers.IntLen, // delegation shares
		fee.DBRead:  0,
		fee.DBWrite: 0,
		fee.Compute: 0,
	}
	IntrinsicAddPermissionlessDelegatorTxComplexities = fee.Dimensions{
		fee.Bandwidth: IntrinsicBaseTxComplexities[fee.Bandwidth] +
			intrinsicValidatorBandwidth + // validator
			ids.IDLen + // subnetID
			wrappers.IntLen + // signer typeID
			wrappers.IntLen + // num stake outs
			wrappers.IntLen, // delegator rewards typeID
		fee.DBRead:  0,
		fee.DBWrite: 0,
		fee.Compute: 0,
	}
	IntrinsicAddSubnetValidatorTxComplexities = fee.Dimensions{
		fee.Bandwidth: IntrinsicBaseTxComplexities[fee.Bandwidth] +
			intrinsicSubnetValidatorBandwidth + // subnetValidator
			wrappers.IntLen, // subnetAuth typeID
		fee.DBRead:  0,
		fee.DBWrite: 0,
		fee.Compute: 0,
	}
	IntrinsicBaseTxComplexities = fee.Dimensions{
		fee.Bandwidth: wrappers.ShortLen + // codecID
			wrappers.IntLen + // typeID
			wrappers.IntLen + // networkID
			ids.IDLen + // blockchainID
			wrappers.IntLen + // number of outputs
			wrappers.IntLen + // number of inputs
			wrappers.IntLen + // length of memo
			wrappers.IntLen, // number of credentials
		fee.DBRead:  0,
		fee.DBWrite: 0,
		fee.Compute: 0,
	}
	IntrinsicCreateChainTxComplexities = fee.Dimensions{
		fee.Bandwidth: IntrinsicBaseTxComplexities[fee.Bandwidth] +
			ids.IDLen + // subnetID
			wrappers.ShortLen + // chainName length
			ids.IDLen + // vmID
			wrappers.IntLen + // num fxIDs
			wrappers.IntLen + // genesis length
			wrappers.IntLen, // subnetAuth typeID
		fee.DBRead:  0,
		fee.DBWrite: 0,
		fee.Compute: 0,
	}
	IntrinsicCreateSubnetTxComplexities = fee.Dimensions{
		fee.Bandwidth: IntrinsicBaseTxComplexities[fee.Bandwidth] +
			wrappers.IntLen, // owner typeID
		fee.DBRead:  0,
		fee.DBWrite: 0,
		fee.Compute: 0,
	}
	IntrinsicExportTxComplexities = fee.Dimensions{
		fee.Bandwidth: IntrinsicBaseTxComplexities[fee.Bandwidth] +
			ids.IDLen + // destination chainID
			wrappers.IntLen, // num exported outputs
		fee.DBRead:  0,
		fee.DBWrite: 0,
		fee.Compute: 0,
	}
	IntrinsicImportTxComplexities = fee.Dimensions{
		fee.Bandwidth: IntrinsicBaseTxComplexities[fee.Bandwidth] +
			ids.IDLen + // source chainID
			wrappers.IntLen, // num importing inputs
		fee.DBRead:  0,
		fee.DBWrite: 0,
		fee.Compute: 0,
	}
	IntrinsicRemoveSubnetValidatorTxComplexities = fee.Dimensions{
		fee.Bandwidth: IntrinsicBaseTxComplexities[fee.Bandwidth] +
			ids.NodeIDLen + // nodeID
			ids.IDLen + // subnetID
			wrappers.IntLen, // subnetAuth typeID
		fee.DBRead:  0,
		fee.DBWrite: 0,
		fee.Compute: 0,
	}
	IntrinsicTransferSubnetOwnershipTxComplexities = fee.Dimensions{
		fee.Bandwidth: IntrinsicBaseTxComplexities[fee.Bandwidth] +
			ids.IDLen + // subnetID
			wrappers.IntLen + // subnetAuth typeID
			wrappers.IntLen, // owner typeID
		fee.DBRead:  0,
		fee.DBWrite: 0,
		fee.Compute: 0,
	}

	errUnsupportedTx     = errors.New("unsupported tx type")
	errUnsupportedOutput = errors.New("unsupported output type")
	errUnsupportedInput  = errors.New("unsupported input type")
)

func TxComplexity(tx txs.UnsignedTx) (fee.Dimensions, error) {
	c := dimensionsCalculator{}
	err := tx.Visit(&c)
	return c.dimensions, err
}

func OutputComplexity(out *avax.TransferableOutput) (fee.Dimensions, error) {
	complexity := fee.Dimensions{
		fee.Bandwidth: intrinsicOutputBandwidth + intrinsicSECP256k1FxOutputBandwidth,
		fee.DBRead:    0,
		fee.DBWrite:   1,
		fee.Compute:   0,
	}

	outIntf := out.Out
	if stakeableOut, ok := outIntf.(*stakeable.LockOut); ok {
		complexity[fee.Bandwidth] += intrinsicStakeableLockedOutputBandwidth
		outIntf = stakeableOut.TransferableOut
	}

	secp256k1Out, ok := outIntf.(*secp256k1fx.TransferOutput)
	if !ok {
		return fee.Dimensions{}, errUnsupportedOutput
	}

	numAddresses := uint64(len(secp256k1Out.Addrs))
	// TODO: Overflow check
	complexity[fee.Bandwidth] += numAddresses * ids.ShortIDLen // addresses
	return complexity, nil
}

// InputComplexity returns the complexity an input adds to a transaction. It
// includes the complexity that the corresponding credential will add.
func InputComplexity(in *avax.TransferableInput) (fee.Dimensions, error) {
	complexity := fee.Dimensions{
		fee.Bandwidth: intrinsicInputBandwidth + intrinsicSECP256k1FxInputBandwidth,
		fee.DBRead:    1,
		fee.DBWrite:   1,
		fee.Compute:   0, // TODO
	}

	inIntf := in.In
	if stakeableIn, ok := inIntf.(*stakeable.LockIn); ok {
		complexity[fee.Bandwidth] += intrinsicStakeableLockedInputBandwidth
		inIntf = stakeableIn.TransferableIn
	}

	secp256k1In, ok := inIntf.(*secp256k1fx.TransferInput)
	if !ok {
		return fee.Dimensions{}, errUnsupportedInput
	}

	numSignatures := uint64(len(secp256k1In.SigIndices))
	complexity[fee.Bandwidth] += numSignatures * intrinsicSECP256k1FxSignatureBandwidth // TODO: Overflow check
	return complexity, nil
}

// OwnerComplexity returns the complexity an owner adds to a transaction.
// It does not include the typeID of the owner.
func OwnerComplexity(_ fx.Owner) (fee.Dimensions, error) {
	return fee.Dimensions{
		fee.Bandwidth: 0, // TODO
		fee.DBRead:    0,
		fee.DBWrite:   1,
		fee.Compute:   0, // TODO
	}, nil
}

// AuthComplexity returns the complexity an authorization adds to a transaction.
// It does not include the typeID of the authorization.
// It does includes the complexity that the corresponding credential will add.
func AuthComplexity(_ verify.Verifiable) (fee.Dimensions, error) {
	return fee.Dimensions{
		fee.Bandwidth: 0, // TODO
		fee.DBRead:    1,
		fee.DBWrite:   0,
		fee.Compute:   0, // TODO
	}, nil
}

// SignerComplexity returns the complexity a signer adds to a transaction.
// It does not include the typeID of the signer.
func SignerComplexity(_ signer.Signer) (fee.Dimensions, error) {
	return fee.Dimensions{
		fee.Bandwidth: 0, // TODO
		fee.DBRead:    0,
		fee.DBWrite:   0,
		fee.Compute:   0, // TODO
	}, nil
}

type dimensionsCalculator struct {
	// outputs:
	dimensions fee.Dimensions
}

func (*dimensionsCalculator) AddDelegatorTx(*txs.AddDelegatorTx) error {
	return errUnsupportedTx
}

func (*dimensionsCalculator) AddValidatorTx(*txs.AddValidatorTx) error {
	return errUnsupportedTx
}

func (*dimensionsCalculator) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return errUnsupportedTx
}

func (*dimensionsCalculator) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return errUnsupportedTx
}

func (*dimensionsCalculator) TransformSubnetTx(*txs.TransformSubnetTx) error {
	return errUnsupportedTx
}

func (c *dimensionsCalculator) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	complexity, err := baseTx(&tx.BaseTx)
	if err != nil {
		return err
	}
	signerComplexity, err := SignerComplexity(tx.Signer)
	if err != nil {
		return err
	}
	complexity, err = complexity.Add(signerComplexity)
	if err != nil {
		return err
	}
	for _, out := range tx.StakeOuts {
		outputComplexity, err := OutputComplexity(out)
		if err != nil {
			return err
		}

		complexity, err = complexity.Add(outputComplexity)
		if err != nil {
			return err
		}
	}
	validatorOwnerComplexity, err := OwnerComplexity(tx.ValidatorRewardsOwner)
	if err != nil {
		return err
	}
	complexity, err = complexity.Add(validatorOwnerComplexity)
	if err != nil {
		return err
	}
	delegatorOwnerComplexity, err := OwnerComplexity(tx.DelegatorRewardsOwner)
	if err != nil {
		return err
	}
	complexity, err = complexity.Add(delegatorOwnerComplexity)
	if err != nil {
		return err
	}
	c.dimensions, err = complexity.Add(IntrinsicAddPermissionlessDelegatorTxComplexities)
	return err
}

func (c *dimensionsCalculator) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	complexity, err := baseTx(&tx.BaseTx)
	if err != nil {
		return err
	}
	ownerComplexity, err := OwnerComplexity(tx.DelegationRewardsOwner)
	if err != nil {
		return err
	}
	complexity, err = complexity.Add(ownerComplexity)
	if err != nil {
		return err
	}
	for _, out := range tx.StakeOuts {
		outputComplexity, err := OutputComplexity(out)
		if err != nil {
			return err
		}

		complexity, err = complexity.Add(outputComplexity)
		if err != nil {
			return err
		}
	}
	c.dimensions, err = complexity.Add(IntrinsicAddPermissionlessDelegatorTxComplexities)
	return err
}

func (c *dimensionsCalculator) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	complexity, err := baseTx(&tx.BaseTx)
	if err != nil {
		return err
	}
	authComplexity, err := AuthComplexity(tx.SubnetAuth)
	if err != nil {
		return err
	}
	complexity, err = complexity.Add(authComplexity)
	if err != nil {
		return err
	}
	c.dimensions, err = complexity.Add(IntrinsicAddSubnetValidatorTxComplexities)
	return err
}

func (c *dimensionsCalculator) BaseTx(tx *txs.BaseTx) error {
	complexity, err := baseTx(tx)
	if err != nil {
		return err
	}
	c.dimensions, err = complexity.Add(IntrinsicBaseTxComplexities)
	return err
}

func (c *dimensionsCalculator) CreateChainTx(tx *txs.CreateChainTx) error {
	complexity := fee.Dimensions{
		// TODO: Overflow check
		fee.Bandwidth: uint64(len(tx.ChainName) + len(tx.FxIDs)*ids.IDLen + len(tx.GenesisData)),
		fee.DBRead:    0,
		fee.DBWrite:   0,
		fee.Compute:   0,
	}
	baseTxComplexity, err := baseTx(&tx.BaseTx)
	if err != nil {
		return err
	}
	complexity, err = complexity.Add(baseTxComplexity)
	if err != nil {
		return err
	}
	authComplexity, err := AuthComplexity(tx.SubnetAuth)
	if err != nil {
		return err
	}
	complexity, err = complexity.Add(authComplexity)
	if err != nil {
		return err
	}
	c.dimensions, err = complexity.Add(IntrinsicCreateChainTxComplexities)
	return err
}

func (c *dimensionsCalculator) CreateSubnetTx(tx *txs.CreateSubnetTx) error {
	complexity, err := baseTx(&tx.BaseTx)
	if err != nil {
		return err
	}
	ownerComplexity, err := OwnerComplexity(tx.Owner)
	if err != nil {
		return err
	}
	complexity, err = complexity.Add(ownerComplexity)
	if err != nil {
		return err
	}
	c.dimensions, err = complexity.Add(IntrinsicCreateChainTxComplexities)
	return err
}

func (c *dimensionsCalculator) ExportTx(tx *txs.ExportTx) error {
	complexity, err := baseTx(&tx.BaseTx)
	if err != nil {
		return err
	}
	for _, out := range tx.ExportedOutputs {
		outputComplexity, err := OutputComplexity(out)
		if err != nil {
			return err
		}

		complexity, err = complexity.Add(outputComplexity)
		if err != nil {
			return err
		}
	}
	c.dimensions, err = complexity.Add(IntrinsicExportTxComplexities)
	return err
}

func (c *dimensionsCalculator) ImportTx(tx *txs.ImportTx) error {
	complexity, err := baseTx(&tx.BaseTx)
	if err != nil {
		return err
	}
	for _, in := range tx.ImportedInputs {
		inputComplexity, err := InputComplexity(in)
		if err != nil {
			return err
		}

		complexity, err = complexity.Add(inputComplexity)
		if err != nil {
			return err
		}
	}
	c.dimensions, err = complexity.Add(IntrinsicImportTxComplexities)
	return err
}

func (c *dimensionsCalculator) RemoveSubnetValidatorTx(tx *txs.RemoveSubnetValidatorTx) error {
	complexity, err := baseTx(&tx.BaseTx)
	if err != nil {
		return err
	}
	authComplexity, err := AuthComplexity(tx.SubnetAuth)
	if err != nil {
		return err
	}
	complexity, err = complexity.Add(authComplexity)
	if err != nil {
		return err
	}
	c.dimensions, err = complexity.Add(IntrinsicRemoveSubnetValidatorTxComplexities)
	return err
}

func (c *dimensionsCalculator) TransferSubnetOwnershipTx(tx *txs.TransferSubnetOwnershipTx) error {
	complexity, err := baseTx(&tx.BaseTx)
	if err != nil {
		return err
	}
	authComplexity, err := AuthComplexity(tx.SubnetAuth)
	if err != nil {
		return err
	}
	complexity, err = complexity.Add(authComplexity)
	if err != nil {
		return err
	}
	ownerComplexity, err := OwnerComplexity(tx.Owner)
	if err != nil {
		return err
	}
	complexity, err = complexity.Add(ownerComplexity)
	if err != nil {
		return err
	}
	c.dimensions, err = complexity.Add(IntrinsicTransferSubnetOwnershipTxComplexities)
	return err
}

func baseTx(tx *txs.BaseTx) (fee.Dimensions, error) {
	var complexity fee.Dimensions
	for _, out := range tx.Outs {
		outputComplexity, err := OutputComplexity(out)
		if err != nil {
			return fee.Dimensions{}, err
		}

		complexity, err = complexity.Add(outputComplexity)
		if err != nil {
			return fee.Dimensions{}, err
		}
	}
	for _, in := range tx.Ins {
		inputComplexity, err := InputComplexity(in)
		if err != nil {
			return fee.Dimensions{}, err
		}

		complexity, err = complexity.Add(inputComplexity)
		if err != nil {
			return fee.Dimensions{}, err
		}
	}
	complexity[fee.Bandwidth] += uint64(len(tx.Memo))
	return complexity, nil
}
