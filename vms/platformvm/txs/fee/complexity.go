// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// TODO: Before Etna, address all TODOs in this package and ensure ACP-103
// compliance.
package fee

import (
	"errors"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Signature verification costs were conservatively based on benchmarks run on
// an AWS c5.xlarge instance.
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

	intrinsicSECP256k1FxInputBandwidth = wrappers.IntLen + // num indices
		wrappers.IntLen // num signatures

	intrinsicSECP256k1FxTransferableInputBandwidth = wrappers.LongLen + // amount
		intrinsicSECP256k1FxInputBandwidth

	intrinsicSECP256k1FxSignatureBandwidth = wrappers.IntLen + // signature index
		secp256k1.SignatureLen // signature length

	intrinsicSECP256k1FxSignatureCompute = 200 // secp256k1 signature verification time is around 200us

	intrinsicConvertSubnetToL1ValidatorBandwidth = wrappers.IntLen + // nodeID length
		wrappers.LongLen + // weight
		wrappers.LongLen + // balance
		wrappers.IntLen + // remaining balance owner threshold
		wrappers.IntLen + // remaining balance owner num addresses
		wrappers.IntLen + // deactivation owner threshold
		wrappers.IntLen // deactivation owner num addresses

	intrinsicBLSAggregateCompute           = 5     // BLS public key aggregation time is around 5us
	intrinsicBLSVerifyCompute              = 1_000 // BLS verification time is around 1000us
	intrinsicBLSPublicKeyValidationCompute = 50    // BLS public key validation time is around 50us
	intrinsicBLSPoPVerifyCompute           = intrinsicBLSPublicKeyValidationCompute + intrinsicBLSVerifyCompute

	intrinsicWarpDBReads = 3 + 20 // chainID -> subnetID mapping + apply weight diffs + apply pk diffs + diff application reads

	intrinsicPoPBandwidth = bls.PublicKeyLen + // public key
		bls.SignatureLen // signature

	intrinsicInputDBRead = 1

	intrinsicInputDBWrite                      = 1
	intrinsicOutputDBWrite                     = 1
	intrinsicConvertSubnetToL1ValidatorDBWrite = 4 // weight diff + pub key diff + subnetID/nodeID + validationID
)

var (
	_ txs.Visitor = (*complexityVisitor)(nil)

	IntrinsicAddSubnetValidatorTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			intrinsicSubnetValidatorBandwidth + // subnetValidator
			wrappers.IntLen + // subnetAuth typeID
			wrappers.IntLen, // subnetAuthCredential typeID
		gas.DBRead:  3, // get subnet auth + check for subnet transformation + check for subnet conversion
		gas.DBWrite: 3, // put current staker + write weight diff + write pk diff
	}
	IntrinsicCreateChainTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			ids.IDLen + // subnetID
			wrappers.ShortLen + // chainName length
			ids.IDLen + // vmID
			wrappers.IntLen + // num fxIDs
			wrappers.IntLen + // genesis length
			wrappers.IntLen + // subnetAuth typeID
			wrappers.IntLen, // subnetAuthCredential typeID
		gas.DBRead:  3, // get subnet auth + check for subnet transformation + check for subnet conversion
		gas.DBWrite: 1, // put chain
	}
	IntrinsicCreateSubnetTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			wrappers.IntLen, // owner typeID
		gas.DBWrite: 1, // write subnet owner
	}
	IntrinsicImportTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			ids.IDLen + // source chainID
			wrappers.IntLen, // num importing inputs
	}
	IntrinsicExportTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			ids.IDLen + // destination chainID
			wrappers.IntLen, // num exported outputs
	}
	IntrinsicRemoveSubnetValidatorTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			ids.NodeIDLen + // nodeID
			ids.IDLen + // subnetID
			wrappers.IntLen + // subnetAuth typeID
			wrappers.IntLen, // subnetAuthCredential typeID
		gas.DBRead:  1, // read subnet auth
		gas.DBWrite: 3, // delete validator + write weight diff + write pk diff
	}
	IntrinsicAddPermissionlessValidatorTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			intrinsicValidatorBandwidth + // validator
			ids.IDLen + // subnetID
			wrappers.IntLen + // signer typeID
			wrappers.IntLen + // num stake outs
			wrappers.IntLen + // validator rewards typeID
			wrappers.IntLen + // delegator rewards typeID
			wrappers.IntLen, // delegation shares
		gas.DBRead:  1, // get staking config
		gas.DBWrite: 3, // put current staker + write weight diff + write pk diff
	}
	IntrinsicAddPermissionlessDelegatorTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			intrinsicValidatorBandwidth + // validator
			ids.IDLen + // subnetID
			wrappers.IntLen + // num stake outs
			wrappers.IntLen, // delegator rewards typeID
		gas.DBRead:  1, // get staking config
		gas.DBWrite: 2, // put current staker + write weight diff
	}
	IntrinsicTransferSubnetOwnershipTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			ids.IDLen + // subnetID
			wrappers.IntLen + // subnetAuth typeID
			wrappers.IntLen + // owner typeID
			wrappers.IntLen, // subnetAuthCredential typeID
		gas.DBRead:  1, // read subnet auth
		gas.DBWrite: 1, // set subnet owner
	}
	IntrinsicBaseTxComplexities = gas.Dimensions{
		gas.Bandwidth: codec.VersionSize + // codecVersion
			wrappers.IntLen + // typeID
			wrappers.IntLen + // networkID
			ids.IDLen + // blockchainID
			wrappers.IntLen + // number of outputs
			wrappers.IntLen + // number of inputs
			wrappers.IntLen + // length of memo
			wrappers.IntLen, // number of credentials
	}
	IntrinsicConvertSubnetToL1TxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			ids.IDLen + // subnetID
			ids.IDLen + // chainID
			wrappers.IntLen + // address length
			wrappers.IntLen + // validators length
			wrappers.IntLen + // subnetAuth typeID
			wrappers.IntLen, // subnetAuthCredential typeID
		gas.DBRead:  3, // subnet auth + transformation lookup + conversion lookup
		gas.DBWrite: 2, // write conversion manager + total weight
	}
	IntrinsicRegisterL1ValidatorTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			wrappers.LongLen + // balance
			bls.SignatureLen + // proof of possession
			wrappers.IntLen, // message length
		gas.DBRead:  5, // conversion owner + expiry lookup + sov lookup + subnetID/nodeID lookup + weight lookup
		gas.DBWrite: 6, // write current staker + expiry + write weight diff + write pk diff + subnetID/nodeID lookup + weight lookup
		gas.Compute: intrinsicBLSPoPVerifyCompute,
	}
	IntrinsicSetL1ValidatorWeightTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			wrappers.IntLen, // message length
		gas.DBRead:  3, // read staker + read conversion + read weight
		gas.DBWrite: 5, // remaining balance utxo + write weight diff + write pk diff + weights lookup + validator write
	}
	IntrinsicIncreaseL1ValidatorBalanceTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			ids.IDLen + // validationID
			wrappers.LongLen, // balance
		gas.DBRead:  1, // read staker
		gas.DBWrite: 5, // weight diff + deactivated weight diff + public key diff + delete staker + write staker
	}
	IntrinsicDisableL1ValidatorTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			ids.IDLen + // validationID
			wrappers.IntLen + // auth typeID
			wrappers.IntLen, // authCredential typeID
		gas.DBRead:  1, // read staker
		gas.DBWrite: 6, // write remaining balance utxo + weight diff + deactivated weight diff + public key diff + delete staker + write staker
	}

	IntrinsicAddContinuousValidatorTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			ids.NodeIDLen + // nodeID
			wrappers.LongLen + // period
			wrappers.IntLen + // signer typeID
			wrappers.IntLen + // num stake outs
			wrappers.IntLen + // validator rewards typeID
			wrappers.IntLen + // delegator rewards typeID
			wrappers.IntLen, // delegation shares
		gas.DBRead:  1, // get staking config // todo @razvan:
		gas.DBWrite: 3, // put current staker + write weight diff + write pk diff // todo @razvan:
	}

	IntrinsicStopContinuousValidatorTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			ids.IDLen + // txID
			bls.SignatureLen, // signature
		gas.DBRead:  1, // read staker  // todo @razvan:
		gas.DBWrite: 1, // write staker // todo @razvan:
	}

	errUnsupportedOutput = errors.New("unsupported output type")
	errUnsupportedInput  = errors.New("unsupported input type")
	errUnsupportedOwner  = errors.New("unsupported owner type")
	errUnsupportedAuth   = errors.New("unsupported auth type")
	errUnsupportedSigner = errors.New("unsupported signer type")
)

func TxComplexity(txs ...txs.UnsignedTx) (gas.Dimensions, error) {
	var (
		c          complexityVisitor
		complexity gas.Dimensions
	)
	for _, tx := range txs {
		c = complexityVisitor{}
		err := tx.Visit(&c)
		if err != nil {
			return gas.Dimensions{}, err
		}

		complexity, err = complexity.Add(&c.output)
		if err != nil {
			return gas.Dimensions{}, err
		}
	}
	return complexity, nil
}

// OutputComplexity returns the complexity outputs add to a transaction.
func OutputComplexity(outs ...*avax.TransferableOutput) (gas.Dimensions, error) {
	var complexity gas.Dimensions
	for _, out := range outs {
		outputComplexity, err := outputComplexity(out)
		if err != nil {
			return gas.Dimensions{}, err
		}

		complexity, err = complexity.Add(&outputComplexity)
		if err != nil {
			return gas.Dimensions{}, err
		}
	}
	return complexity, nil
}

func outputComplexity(out *avax.TransferableOutput) (gas.Dimensions, error) {
	complexity := gas.Dimensions{
		gas.Bandwidth: intrinsicOutputBandwidth + intrinsicSECP256k1FxOutputBandwidth,
		gas.DBWrite:   intrinsicOutputDBWrite,
	}

	outIntf := out.Out
	if stakeableOut, ok := outIntf.(*stakeable.LockOut); ok {
		complexity[gas.Bandwidth] += intrinsicStakeableLockedOutputBandwidth
		outIntf = stakeableOut.TransferableOut
	}

	secp256k1Out, ok := outIntf.(*secp256k1fx.TransferOutput)
	if !ok {
		return gas.Dimensions{}, errUnsupportedOutput
	}

	numAddresses := uint64(len(secp256k1Out.Addrs))
	addressBandwidth, err := math.Mul(numAddresses, ids.ShortIDLen)
	if err != nil {
		return gas.Dimensions{}, err
	}
	complexity[gas.Bandwidth], err = math.Add(complexity[gas.Bandwidth], addressBandwidth)
	return complexity, err
}

// InputComplexity returns the complexity inputs add to a transaction.
// It includes the complexity that the corresponding credentials will add.
func InputComplexity(ins ...*avax.TransferableInput) (gas.Dimensions, error) {
	var complexity gas.Dimensions
	for _, in := range ins {
		inputComplexity, err := inputComplexity(in)
		if err != nil {
			return gas.Dimensions{}, err
		}

		complexity, err = complexity.Add(&inputComplexity)
		if err != nil {
			return gas.Dimensions{}, err
		}
	}
	return complexity, nil
}

func inputComplexity(in *avax.TransferableInput) (gas.Dimensions, error) {
	complexity := gas.Dimensions{
		gas.Bandwidth: intrinsicInputBandwidth + intrinsicSECP256k1FxTransferableInputBandwidth,
		gas.DBRead:    intrinsicInputDBRead,
		gas.DBWrite:   intrinsicInputDBWrite,
	}

	inIntf := in.In
	if stakeableIn, ok := inIntf.(*stakeable.LockIn); ok {
		complexity[gas.Bandwidth] += intrinsicStakeableLockedInputBandwidth
		inIntf = stakeableIn.TransferableIn
	}

	secp256k1In, ok := inIntf.(*secp256k1fx.TransferInput)
	if !ok {
		return gas.Dimensions{}, errUnsupportedInput
	}

	numSignatures := uint64(len(secp256k1In.SigIndices))
	// Add signature bandwidth
	signatureBandwidth, err := math.Mul(numSignatures, intrinsicSECP256k1FxSignatureBandwidth)
	if err != nil {
		return gas.Dimensions{}, err
	}
	complexity[gas.Bandwidth], err = math.Add(complexity[gas.Bandwidth], signatureBandwidth)
	if err != nil {
		return gas.Dimensions{}, err
	}

	// Add signature compute
	complexity[gas.Compute], err = math.Mul(numSignatures, intrinsicSECP256k1FxSignatureCompute)
	if err != nil {
		return gas.Dimensions{}, err
	}
	return complexity, err
}

// ConvertSubnetToL1ValidatorComplexity returns the complexity the validators
// add to a transaction.
func ConvertSubnetToL1ValidatorComplexity(l1Validators ...*txs.ConvertSubnetToL1Validator) (gas.Dimensions, error) {
	var complexity gas.Dimensions
	for _, l1Validator := range l1Validators {
		l1ValidatorComplexity, err := convertSubnetToL1ValidatorComplexity(l1Validator)
		if err != nil {
			return gas.Dimensions{}, err
		}

		complexity, err = complexity.Add(&l1ValidatorComplexity)
		if err != nil {
			return gas.Dimensions{}, err
		}
	}
	return complexity, nil
}

func convertSubnetToL1ValidatorComplexity(l1Validator *txs.ConvertSubnetToL1Validator) (gas.Dimensions, error) {
	complexity := gas.Dimensions{
		gas.Bandwidth: intrinsicConvertSubnetToL1ValidatorBandwidth,
		gas.DBWrite:   intrinsicConvertSubnetToL1ValidatorDBWrite,
	}

	signerComplexity, err := SignerComplexity(&l1Validator.Signer)
	if err != nil {
		return gas.Dimensions{}, err
	}

	numAddresses := uint64(len(l1Validator.RemainingBalanceOwner.Addresses) + len(l1Validator.DeactivationOwner.Addresses))
	addressBandwidth, err := math.Mul(numAddresses, ids.ShortIDLen)
	if err != nil {
		return gas.Dimensions{}, err
	}
	return complexity.Add(
		&gas.Dimensions{
			gas.Bandwidth: uint64(len(l1Validator.NodeID)),
		},
		&signerComplexity,
		&gas.Dimensions{
			gas.Bandwidth: addressBandwidth,
		},
	)
}

// OwnerComplexity returns the complexity an owner adds to a transaction.
// It does not include the typeID of the owner.
func OwnerComplexity(ownerIntf fx.Owner) (gas.Dimensions, error) {
	owner, ok := ownerIntf.(*secp256k1fx.OutputOwners)
	if !ok {
		return gas.Dimensions{}, errUnsupportedOwner
	}

	numAddresses := uint64(len(owner.Addrs))
	addressBandwidth, err := math.Mul(numAddresses, ids.ShortIDLen)
	if err != nil {
		return gas.Dimensions{}, err
	}

	bandwidth, err := math.Add(addressBandwidth, intrinsicSECP256k1FxOutputOwnersBandwidth)
	if err != nil {
		return gas.Dimensions{}, err
	}

	return gas.Dimensions{
		gas.Bandwidth: bandwidth,
	}, nil
}

// AuthComplexity returns the complexity an authorization adds to a transaction.
// It does not include the typeID of the authorization.
// It does includes the complexity that the corresponding credential will add.
// It does not include the typeID of the credential.
func AuthComplexity(authIntf verify.Verifiable) (gas.Dimensions, error) {
	auth, ok := authIntf.(*secp256k1fx.Input)
	if !ok {
		return gas.Dimensions{}, errUnsupportedAuth
	}

	numSignatures := uint64(len(auth.SigIndices))
	signatureBandwidth, err := math.Mul(numSignatures, intrinsicSECP256k1FxSignatureBandwidth)
	if err != nil {
		return gas.Dimensions{}, err
	}

	bandwidth, err := math.Add(signatureBandwidth, intrinsicSECP256k1FxInputBandwidth)
	if err != nil {
		return gas.Dimensions{}, err
	}

	signatureCompute, err := math.Mul(numSignatures, intrinsicSECP256k1FxSignatureCompute)
	if err != nil {
		return gas.Dimensions{}, err
	}

	return gas.Dimensions{
		gas.Bandwidth: bandwidth,
		gas.Compute:   signatureCompute,
	}, nil
}

// SignerComplexity returns the complexity a signer adds to a transaction.
// It does not include the typeID of the signer.
func SignerComplexity(s signer.Signer) (gas.Dimensions, error) {
	switch s.(type) {
	case *signer.Empty:
		return gas.Dimensions{}, nil
	case *signer.ProofOfPossession:
		return gas.Dimensions{
			gas.Bandwidth: intrinsicPoPBandwidth,
			gas.Compute:   intrinsicBLSPoPVerifyCompute,
		}, nil
	default:
		return gas.Dimensions{}, errUnsupportedSigner
	}
}

// WarpComplexity returns the complexity a warp message adds to a transaction.
func WarpComplexity(message []byte) (gas.Dimensions, error) {
	msg, err := warp.ParseMessage(message)
	if err != nil {
		return gas.Dimensions{}, err
	}

	numSigners, err := msg.Signature.NumSigners()
	if err != nil {
		return gas.Dimensions{}, err
	}
	aggregationCompute, err := math.Mul(uint64(numSigners), intrinsicBLSAggregateCompute)
	if err != nil {
		return gas.Dimensions{}, err
	}

	compute, err := math.Add(aggregationCompute, intrinsicBLSVerifyCompute)
	if err != nil {
		return gas.Dimensions{}, err
	}

	return gas.Dimensions{
		gas.Bandwidth: uint64(len(message)),
		gas.DBRead:    intrinsicWarpDBReads,
		gas.Compute:   compute,
	}, nil
}

type complexityVisitor struct {
	output gas.Dimensions
}

func (*complexityVisitor) AddValidatorTx(*txs.AddValidatorTx) error {
	return ErrUnsupportedTx
}

func (*complexityVisitor) AddDelegatorTx(*txs.AddDelegatorTx) error {
	return ErrUnsupportedTx
}

func (*complexityVisitor) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return ErrUnsupportedTx
}

func (*complexityVisitor) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return ErrUnsupportedTx
}

func (*complexityVisitor) RewardContinuousValidatorTx(tx *txs.RewardContinuousValidatorTx) error {
	return ErrUnsupportedTx
}

func (*complexityVisitor) TransformSubnetTx(*txs.TransformSubnetTx) error {
	return ErrUnsupportedTx
}

func (c *complexityVisitor) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	baseTxComplexity, err := baseTxComplexity(&tx.BaseTx)
	if err != nil {
		return err
	}
	authComplexity, err := AuthComplexity(tx.SubnetAuth)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicAddSubnetValidatorTxComplexities.Add(
		&baseTxComplexity,
		&authComplexity,
	)
	return err
}

func (c *complexityVisitor) CreateChainTx(tx *txs.CreateChainTx) error {
	bandwidth, err := math.Mul(uint64(len(tx.FxIDs)), ids.IDLen)
	if err != nil {
		return err
	}
	bandwidth, err = math.Add(bandwidth, uint64(len(tx.ChainName)))
	if err != nil {
		return err
	}
	bandwidth, err = math.Add(bandwidth, uint64(len(tx.GenesisData)))
	if err != nil {
		return err
	}
	dynamicComplexity := gas.Dimensions{
		gas.Bandwidth: bandwidth,
	}

	baseTxComplexity, err := baseTxComplexity(&tx.BaseTx)
	if err != nil {
		return err
	}
	authComplexity, err := AuthComplexity(tx.SubnetAuth)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicCreateChainTxComplexities.Add(
		&dynamicComplexity,
		&baseTxComplexity,
		&authComplexity,
	)
	return err
}

func (c *complexityVisitor) CreateSubnetTx(tx *txs.CreateSubnetTx) error {
	baseTxComplexity, err := baseTxComplexity(&tx.BaseTx)
	if err != nil {
		return err
	}
	ownerComplexity, err := OwnerComplexity(tx.Owner)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicCreateSubnetTxComplexities.Add(
		&baseTxComplexity,
		&ownerComplexity,
	)
	return err
}

func (c *complexityVisitor) ImportTx(tx *txs.ImportTx) error {
	baseTxComplexity, err := baseTxComplexity(&tx.BaseTx)
	if err != nil {
		return err
	}
	inputsComplexity, err := InputComplexity(tx.ImportedInputs...)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicImportTxComplexities.Add(
		&baseTxComplexity,
		&inputsComplexity,
	)
	return err
}

func (c *complexityVisitor) ExportTx(tx *txs.ExportTx) error {
	baseTxComplexity, err := baseTxComplexity(&tx.BaseTx)
	if err != nil {
		return err
	}
	outputsComplexity, err := OutputComplexity(tx.ExportedOutputs...)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicExportTxComplexities.Add(
		&baseTxComplexity,
		&outputsComplexity,
	)
	return err
}

func (c *complexityVisitor) RemoveSubnetValidatorTx(tx *txs.RemoveSubnetValidatorTx) error {
	baseTxComplexity, err := baseTxComplexity(&tx.BaseTx)
	if err != nil {
		return err
	}
	authComplexity, err := AuthComplexity(tx.SubnetAuth)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicRemoveSubnetValidatorTxComplexities.Add(
		&baseTxComplexity,
		&authComplexity,
	)
	return err
}

func (c *complexityVisitor) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	baseTxComplexity, err := baseTxComplexity(&tx.BaseTx)
	if err != nil {
		return err
	}
	signerComplexity, err := SignerComplexity(tx.Signer)
	if err != nil {
		return err
	}
	outputsComplexity, err := OutputComplexity(tx.StakeOuts...)
	if err != nil {
		return err
	}
	validatorOwnerComplexity, err := OwnerComplexity(tx.ValidatorRewardsOwner)
	if err != nil {
		return err
	}
	delegatorOwnerComplexity, err := OwnerComplexity(tx.DelegatorRewardsOwner)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicAddPermissionlessValidatorTxComplexities.Add(
		&baseTxComplexity,
		&signerComplexity,
		&outputsComplexity,
		&validatorOwnerComplexity,
		&delegatorOwnerComplexity,
	)
	return err
}

func (c *complexityVisitor) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	baseTxComplexity, err := baseTxComplexity(&tx.BaseTx)
	if err != nil {
		return err
	}
	ownerComplexity, err := OwnerComplexity(tx.DelegationRewardsOwner)
	if err != nil {
		return err
	}
	outputsComplexity, err := OutputComplexity(tx.StakeOuts...)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicAddPermissionlessDelegatorTxComplexities.Add(
		&baseTxComplexity,
		&ownerComplexity,
		&outputsComplexity,
	)
	return err
}

func (c *complexityVisitor) TransferSubnetOwnershipTx(tx *txs.TransferSubnetOwnershipTx) error {
	baseTxComplexity, err := baseTxComplexity(&tx.BaseTx)
	if err != nil {
		return err
	}
	authComplexity, err := AuthComplexity(tx.SubnetAuth)
	if err != nil {
		return err
	}
	ownerComplexity, err := OwnerComplexity(tx.Owner)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicTransferSubnetOwnershipTxComplexities.Add(
		&baseTxComplexity,
		&authComplexity,
		&ownerComplexity,
	)
	return err
}

func (c *complexityVisitor) BaseTx(tx *txs.BaseTx) error {
	baseTxComplexity, err := baseTxComplexity(tx)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicBaseTxComplexities.Add(&baseTxComplexity)
	return err
}

func (c *complexityVisitor) ConvertSubnetToL1Tx(tx *txs.ConvertSubnetToL1Tx) error {
	baseTxComplexity, err := baseTxComplexity(&tx.BaseTx)
	if err != nil {
		return err
	}
	validatorComplexity, err := ConvertSubnetToL1ValidatorComplexity(tx.Validators...)
	if err != nil {
		return err
	}
	authComplexity, err := AuthComplexity(tx.SubnetAuth)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicConvertSubnetToL1TxComplexities.Add(
		&baseTxComplexity,
		&validatorComplexity,
		&authComplexity,
		&gas.Dimensions{
			gas.Bandwidth: uint64(len(tx.Address)),
		},
	)
	return err
}

func (c *complexityVisitor) RegisterL1ValidatorTx(tx *txs.RegisterL1ValidatorTx) error {
	baseTxComplexity, err := baseTxComplexity(&tx.BaseTx)
	if err != nil {
		return err
	}
	warpComplexity, err := WarpComplexity(tx.Message)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicRegisterL1ValidatorTxComplexities.Add(
		&baseTxComplexity,
		&warpComplexity,
	)
	return err
}

func (c *complexityVisitor) SetL1ValidatorWeightTx(tx *txs.SetL1ValidatorWeightTx) error {
	baseTxComplexity, err := baseTxComplexity(&tx.BaseTx)
	if err != nil {
		return err
	}
	warpComplexity, err := WarpComplexity(tx.Message)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicSetL1ValidatorWeightTxComplexities.Add(
		&baseTxComplexity,
		&warpComplexity,
	)
	return err
}

func (c *complexityVisitor) IncreaseL1ValidatorBalanceTx(tx *txs.IncreaseL1ValidatorBalanceTx) error {
	baseTxComplexity, err := baseTxComplexity(&tx.BaseTx)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicIncreaseL1ValidatorBalanceTxComplexities.Add(
		&baseTxComplexity,
	)
	return err
}

func (c *complexityVisitor) DisableL1ValidatorTx(tx *txs.DisableL1ValidatorTx) error {
	baseTxComplexity, err := baseTxComplexity(&tx.BaseTx)
	if err != nil {
		return err
	}
	authComplexity, err := AuthComplexity(tx.DisableAuth)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicDisableL1ValidatorTxComplexities.Add(
		&baseTxComplexity,
		&authComplexity,
	)
	return err
}

func (c *complexityVisitor) AddContinuousValidatorTx(tx *txs.AddContinuousValidatorTx) error {
	baseTxComplexity, err := baseTxComplexity(&tx.BaseTx)
	if err != nil {
		return err
	}
	signerComplexity, err := SignerComplexity(tx.Signer)
	if err != nil {
		return err
	}
	outputsComplexity, err := OutputComplexity(tx.StakeOuts...)
	if err != nil {
		return err
	}
	validatorOwnerComplexity, err := OwnerComplexity(tx.ValidatorRewardsOwner)
	if err != nil {
		return err
	}
	delegatorOwnerComplexity, err := OwnerComplexity(tx.DelegatorRewardsOwner)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicAddContinuousValidatorTxComplexities.Add(
		&baseTxComplexity,
		&signerComplexity,
		&outputsComplexity,
		&validatorOwnerComplexity,
		&delegatorOwnerComplexity,
	)
	return err
}

func (c *complexityVisitor) StopContinuousValidatorTx(tx *txs.StopContinuousValidatorTx) error {
	baseTxComplexity, err := baseTxComplexity(&tx.BaseTx)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicStopContinuousValidatorTxComplexities.Add(&baseTxComplexity)
	return err
}

func baseTxComplexity(tx *txs.BaseTx) (gas.Dimensions, error) {
	outputsComplexity, err := OutputComplexity(tx.Outs...)
	if err != nil {
		return gas.Dimensions{}, err
	}
	inputsComplexity, err := InputComplexity(tx.Ins...)
	if err != nil {
		return gas.Dimensions{}, err
	}
	complexity, err := outputsComplexity.Add(&inputsComplexity)
	if err != nil {
		return gas.Dimensions{}, err
	}
	complexity[gas.Bandwidth], err = math.Add(
		complexity[gas.Bandwidth],
		uint64(len(tx.Memo)),
	)
	return complexity, err
}
