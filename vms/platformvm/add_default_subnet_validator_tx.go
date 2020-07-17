// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/vms/secp256k1fx"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/vms/components/verify"
)

var (
	errNilTx          = errors.New("tx is nil")
	errWrongNetworkID = errors.New("tx was issued with a different network ID")
	errWeightTooSmall = errors.New("weight of this validator is too low")
	errStakeTooShort  = errors.New("staking period is too short")
	errStakeTooLong   = errors.New("staking period is too long")
	errTooManyShares  = fmt.Errorf("a staker can only require at most %d shares from delegators", NumberOfShares)
)

// UnsignedAddDefaultSubnetValidatorTx is an unsigned addDefaultSubnetValidatorTx
type UnsignedAddDefaultSubnetValidatorTx struct {
	// Metadata, inputs and outputs
	BaseProposalTx `serialize:"true"`
	// Describes the validator
	DurationValidator `serialize:"true"`
	// Address to send staked AVA (and possibly reward) to when staker is done staking
	Destination ids.ShortID `serialize:"true"`
	// Fee this validator charges delegators as a percentage, times 10,000
	// For example, if this validator has Shares=300,000 then they take 30% of rewards from delegators
	Shares uint32 `serialize:"true"`
}

// addDefaultSubnetValidatorTx is a transaction that, if it is in a ProposeAddValidator block that
// is accepted and followed by a Commit block, adds a validator to the pending validator set of the default subnet.
// (That is, the validator in the tx will validate at some point in the future.)
type addDefaultSubnetValidatorTx struct {
	UnsignedAddDefaultSubnetValidatorTx `serialize:"true"`

	// Credentials that authorize the inputs to spend the corresponding outputs
	Credentials []verify.Verifiable `serialize:"true"`
}

// Creds returns this transactions credentials
func (tx *addDefaultSubnetValidatorTx) Creds() []verify.Verifiable {
	return tx.Credentials
}

// ID is this transaction's ID
func (tx *addDefaultSubnetValidatorTx) ID() ids.ID {
	return tx.BaseProposalTx.BaseTx.ID()
}

// initialize [tx]. Sets [tx.vm], [tx.unsignedBytes], [tx.bytes], [tx.id]
func (tx *addDefaultSubnetValidatorTx) initialize(vm *VM) error {
	if tx.vm != nil { // Already been initialized
		return nil
	}
	tx.vm = vm
	var err error
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx.UnsignedAddDefaultSubnetValidatorTx))
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedAddDefaultSubnetValidatorTx: %w", err)
	}
	tx.bytes, err = Codec.Marshal(tx) // byte representation of the signed transaction
	if err != nil {
		return fmt.Errorf("couldn't marshal addDefaultSubnetValidatorTx: %w", err)
	}
	tx.id = ids.NewID(hashing.ComputeHash256Array(tx.bytes))
	return err
}

// SyntacticVerify that this transaction is well formed
func (tx *addDefaultSubnetValidatorTx) SyntacticVerify() error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.id.IsZero():
		return errInvalidID
	case tx.NetworkID != tx.vm.Ctx.NetworkID:
		return errWrongNetworkID
	case tx.NodeID.IsZero():
		return errInvalidID
	case tx.Destination.IsZero():
		return errInvalidID
	case tx.Wght < MinimumStakeAmount: // Ensure validator is staking at least the minimum amount
		return errWeightTooSmall
	case tx.Shares > NumberOfShares: // Ensure delegators shares are in the allowed amount
		return errTooManyShares
	}
	if err := tx.BaseTx.SyntacticVerify(); err != nil {
		return err
	}

	// Ensure staking length is not too short or long,
	// and that the inputs/outputs of this tx are syntactically valid
	stakingDuration := tx.Duration()
	if stakingDuration < MinimumStakingDuration {
		return errStakeTooShort
	} else if stakingDuration > MaximumStakingDuration {
		return errStakeTooLong
	} else if err := syntacticVerifySpend(tx.OnCommitIns, tx.OnCommitOuts,
		tx.OnCommitCreds, tx.vm.txFee, tx.vm.avaxAssetID); err != nil {
		return err
	} else if err := syntacticVerifySpend(tx.OnAbortIns, tx.OnAbortOuts,
		tx.OnAbortCreds, tx.vm.txFee, tx.vm.avaxAssetID); err != nil {
		return err
	}
	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *addDefaultSubnetValidatorTx) SemanticVerify(db database.Database) (*versiondb.Database, *versiondb.Database, func(), func(), TxError) {
	// Verify the tx is well-formed
	if err := tx.SyntacticVerify(); err != nil {
		return nil, nil, nil, nil, permError{err}
	}
	// Verify inputs/outputs and update the UTXO set
	onCommitDB := versiondb.New(db)
	if err := tx.vm.semanticVerifySpend(onCommitDB, tx, tx.OnCommitIns, tx.OnCommitOuts, tx.OnCommitCreds); err != nil {
		return nil, nil, nil, nil, err
	}
	onAbortDB := versiondb.New(db)
	if err := tx.vm.semanticVerifySpend(onAbortDB, tx, tx.OnAbortIns, tx.OnAbortOuts, tx.OnAbortCreds); err != nil {
		return nil, nil, nil, nil, err
	}

	// Ensure the proposed validator starts after the current time
	if currentTime, err := tx.vm.getTimestamp(db); err != nil {
		return nil, nil, nil, nil, tempError{err}
	} else if startTime := tx.StartTime(); !currentTime.Before(startTime) {
		return nil, nil, nil, nil, permError{fmt.Errorf("validator's start time (%s) at or after current timestamp (%s)",
			currentTime,
			startTime)}
	}

	// Ensure the proposed validator is not already a validator of the specified subnet
	currentValidatorHeap, err := tx.vm.getCurrentValidators(db, DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, tempError{err}
	}
	for _, currentVdr := range tx.vm.getValidators(currentValidatorHeap) {
		if currentVdr.ID().Equals(tx.NodeID) {
			return nil, nil, nil, nil, permError{fmt.Errorf("validator %s already is already a Default Subnet validator",
				tx.NodeID)}
		}
	}

	// Ensure the proposed validator is not already slated to validate for the specified subnet
	pendingValidatorHeap, err := tx.vm.getPendingValidators(db, DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, tempError{err}
	}
	for _, pendingVdr := range tx.vm.getValidators(pendingValidatorHeap) {
		if pendingVdr.ID().Equals(tx.NodeID) {
			return nil, nil, nil, nil, tempError{fmt.Errorf("validator %s is already a pending Default Subnet validator",
				tx.NodeID)}
		}
	}
	pendingValidatorHeap.Add(tx) // add validator to set of pending validators

	// If this proposal is committed, update the pending validator set to include the validator
	if err := tx.vm.putPendingValidators(onCommitDB, pendingValidatorHeap, DefaultSubnetID); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	return onCommitDB, onAbortDB, tx.vm.resetTimer, nil, nil
}

// InitiallyPrefersCommit returns true if the proposed validators start time is
// after the current wall clock time,
func (tx *addDefaultSubnetValidatorTx) InitiallyPrefersCommit() bool {
	return tx.StartTime().After(tx.vm.clock.Time())
}

// NewAddDefaultSubnetValidatorTx returns a new NewAddDefaultSubnetValidatorTx
func (vm *VM) newAddDefaultSubnetValidatorTx(
	stakeAmt uint64, // Amount being staked
	startTime uint64, // Unix time they start validating
	endTime uint64, // Unix time they stop validating
	nodeID ids.ShortID, // ID of node that will validate
	destination ids.ShortID, // Address to returned staked tokens (and maybe reward) to
	shares uint32, // 10,000 times percentage of reward taken from delegators
	keys []*crypto.PrivateKeySECP256K1R, // // Keys providing the staked tokens + fee
) (*addDefaultSubnetValidatorTx, error) {
	// Calculate inputs and outputs
	changeSpend := &spend{
		Threshold: 1,
		Locktime:  0,
		Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
	}
	// On commit, lock stakeAmt until staking ends
	onCommitSpends := []*spend{
		{
			Amount:   stakeAmt,
			Addrs:    []ids.ShortID{destination},
			Locktime: endTime,
		},
	}
	onCommitIns, onCommitOuts, onCommitCredKeys, err := vm.spend(vm.DB, keys, onCommitSpends, changeSpend, vm.txFee)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}
	// On abort, just pay tx fee
	onAbortIns, onAbortOuts, onAbortCredKeys, err := vm.spend(vm.DB, keys, nil, changeSpend, vm.txFee)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Create the tx
	tx := &addDefaultSubnetValidatorTx{
		UnsignedAddDefaultSubnetValidatorTx: UnsignedAddDefaultSubnetValidatorTx{
			BaseProposalTx: BaseProposalTx{
				BaseTx: &BaseTx{
					NetworkID:    vm.Ctx.NetworkID,
					BlockchainID: vm.Ctx.ChainID,
				},
				OnCommitIns:  onCommitIns,
				OnCommitOuts: onCommitOuts,
				OnAbortIns:   onAbortIns,
				OnAbortOuts:  onAbortOuts,
			},
			DurationValidator: DurationValidator{
				Validator: Validator{
					NodeID: nodeID,
					Wght:   stakeAmt,
				},
				Start: startTime,
				End:   endTime,
			},
			Destination: destination,
			Shares:      shares,
		},
	}
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx.UnsignedAddDefaultSubnetValidatorTx))
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal UnsignedAddDefaultSubnetValidatorTx: %w", err)
	}
	hash := hashing.ComputeHash256(tx.unsignedBytes)

	// Attach credentials
	tx.OnCommitCreds = make([]verify.Verifiable, len(onCommitCredKeys))
	for i, credKeys := range onCommitCredKeys {
		cred := &secp256k1fx.Credential{}
		for _, key := range credKeys {
			sig, err := key.SignHash(hash) // Sign hash
			if err != nil {
				return nil, fmt.Errorf("problem generating credential: %w", err)
			}
			sigArr := [crypto.SECP256K1RSigLen]byte{}
			copy(sigArr[:], sig)
			cred.Sigs = append(cred.Sigs, sigArr)
		}
		tx.OnCommitCreds[i] = cred // Attach credential
	}
	for _, credKeys := range onAbortCredKeys {
		cred := &secp256k1fx.Credential{Sigs: make([][crypto.SECP256K1RSigLen]byte, len(credKeys))}
		for i, key := range credKeys {
			sig, err := key.SignHash(hash) // Sign hash
			if err != nil {
				return nil, fmt.Errorf("problem generating credential: %w", err)
			}
			copy(cred.Sigs[i][:], sig)
		}
		tx.OnAbortCreds = append(tx.Credentials, cred) // Attach credential
	}

	return tx, tx.initialize(vm)
}
