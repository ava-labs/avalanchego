package state

import (
	"github.com/ava-labs/avalanchego/ids"
	"time"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/constants"
	"fmt"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/iterator"
)

// StakingPeriod contains data derived from the state transitions of a Staker's current staking period.
type StakingPeriod struct {
	// TxID refers to the transaction that initially added this Staker. Data derived from it should be looked up
	// from the tx itself as the source-of-truth.
	TxID      ids.ID
	Weight    uint64
	StartTime time.Time
	EndTime   time.Time
	priority  platform.Priority
	subnetID  ids.ID
	nodeID    ids.NodeID
}

// SubnetID returns the subnet the staker validates.
func (p StakingPeriod) SubnetID() ids.ID {
	return p.subnetID
}

// NodeID returns the node performing the validation.
func (p StakingPeriod) NodeID() ids.NodeID {
	return p.nodeID
}

// IsPermissionedValidator returns whether the period represents a permissioned
// subnet validator.
func (p StakingPeriod) IsPermissionedValidator() bool {
	return p.priority.IsPermissionedValidator()
}

// PendingPrimaryNetworkValidator is a Primary Network validator waiting to
// become current.
type PendingPrimaryNetworkValidator struct {
	StakingPeriod
	publicKey *bls.PublicKey
}

func (PendingPrimaryNetworkValidator) pendingValidator() {}

// PublicKey returns the validator's public key. Legacy Primary Network
// validators do not have a public key.
func (v PendingPrimaryNetworkValidator) PublicKey() *bls.PublicKey {
	return v.publicKey
}

// Promote returns the current validator corresponding to v.
func (v PendingPrimaryNetworkValidator) Promote(potentialReward uint64) CurrentValidator {
	period := v.StakingPeriod
	period.priority = platform.PendingToCurrentPriorities[period.priority]
	return CurrentPrimaryNetworkValidator{
		StakingPeriod:   period,
		PotentialReward: potentialReward,
		publicKey:       v.publicKey,
	}
}

// PendingSubnetValidator is a subnet validator waiting to become current.
type PendingSubnetValidator struct {
	StakingPeriod
}

func (PendingSubnetValidator) pendingValidator() {}

// Promote returns the current validator corresponding to v.
func (v PendingSubnetValidator) Promote(potentialReward uint64) CurrentValidator {
	period := v.StakingPeriod
	period.priority = platform.PendingToCurrentPriorities[period.priority]
	return CurrentSubnetValidator{
		StakingPeriod:   period,
		PotentialReward: potentialReward,
	}
}

// NewPendingPrimaryNetworkValidator returns a pending Primary Network
// validator built from staker.
func NewPendingPrimaryNetworkValidator(txID ids.ID, staker platform.ScheduledStaker) (PendingPrimaryNetworkValidator, error) {
	if staker.SubnetID() != constants.PrimaryNetworkID {
		return PendingPrimaryNetworkValidator{}, fmt.Errorf("%w: expected Primary Network validator", errUnexpectedStaker)
	}
	period, publicKey, err := newPendingStakingPeriod(txID, staker)
	return PendingPrimaryNetworkValidator{
		StakingPeriod: period,
		publicKey:     publicKey,
	}, err
}

// NewPendingSubnetValidator returns a pending subnet validator built from
// staker.
func NewPendingSubnetValidator(txID ids.ID, staker platform.ScheduledStaker) (PendingSubnetValidator, error) {
	if staker.SubnetID() == constants.PrimaryNetworkID {
		return PendingSubnetValidator{}, fmt.Errorf("%w: expected subnet validator", errUnexpectedStaker)
	}
	period, _, err := newPendingStakingPeriod(txID, staker)
	return PendingSubnetValidator{
		StakingPeriod: period,
	}, err
}

// CurrentPrimaryNetworkValidator is an active Primary Network validator.
type CurrentPrimaryNetworkValidator struct {
	StakingPeriod
	PotentialReward uint64
	publicKey       *bls.PublicKey
}

func (CurrentPrimaryNetworkValidator) currentValidator() {}

// PublicKey returns the validator's public key. Legacy Primary Network
// validators do not have a public key.
func (v CurrentPrimaryNetworkValidator) PublicKey() *bls.PublicKey {
	return v.publicKey
}

// ContinuousValidatorMetadata contains the mutable state of a continuous
// validator. It is not meaningful for bounded validators.
type ContinuousValidatorMetadata struct {
	// AccruedValidationRewards is the sum of validation rewards restaked from
	// previous cycles.
	AccruedValidationRewards uint64
	// AccruedDelegateeRewards is the sum of delegatee rewards restaked from
	// previous cycles.
	AccruedDelegateeRewards uint64
	// AutoCompoundRewardShares is the percentage of rewards to restake at the
	// end of a cycle.
	AutoCompoundRewardShares uint32
	// NextPeriod is the next validation cycle duration, in seconds.
	NextPeriod uint64
}

// CurrentContinuousPrimaryNetworkValidator is an active continuous Primary
// Network validator.
//
// A continuous validator is also readable through
// [CurrentPrimaryNetworkValidator], which exposes only its common fields.
type CurrentContinuousPrimaryNetworkValidator struct {
	Validator CurrentPrimaryNetworkValidator
	ContinuousValidatorMetadata
}

// CurrentSubnetValidator is an active subnet validator.
type CurrentSubnetValidator struct {
	StakingPeriod
	PotentialReward uint64
}

func (CurrentSubnetValidator) currentValidator() {}

// NewCurrentPrimaryNetworkValidator returns a current Primary Network
// validator built from staker.
func NewCurrentPrimaryNetworkValidator(
	txID ids.ID,
	staker platform.Staker,
	startTime, endTime time.Time,
	weight, potentialReward uint64,
) (CurrentPrimaryNetworkValidator, error) {
	if staker.SubnetID() != constants.PrimaryNetworkID {
		return CurrentPrimaryNetworkValidator{}, fmt.Errorf("%w: expected Primary Network validator", errUnexpectedStaker)
	}
	period, publicKey, err := newCurrentStakingPeriod(txID, staker, startTime, endTime, weight)
	return CurrentPrimaryNetworkValidator{
		StakingPeriod:   period,
		PotentialReward: potentialReward,
		publicKey:       publicKey,
	}, err
}

// NewCurrentContinuousPrimaryNetworkValidator returns a current continuous
// Primary Network validator built from staker.
func NewCurrentContinuousPrimaryNetworkValidator(
	txID ids.ID,
	staker *platform.AddAutoRenewedValidatorTx,
	startTime, endTime time.Time,
	weight, potentialReward uint64,
) (CurrentContinuousPrimaryNetworkValidator, error) {
	validator, err := NewCurrentPrimaryNetworkValidator(
		txID,
		staker,
		startTime,
		endTime,
		weight,
		potentialReward,
	)
	return CurrentContinuousPrimaryNetworkValidator{
		Validator: validator,
		ContinuousValidatorMetadata: ContinuousValidatorMetadata{
			AutoCompoundRewardShares: staker.AutoCompoundRewardShares,
			NextPeriod:               staker.Period,
		},
	}, err
}

// NewCurrentSubnetValidator returns a current subnet validator built from
// staker.
func NewCurrentSubnetValidator(
	txID ids.ID,
	staker platform.Staker,
	startTime, endTime time.Time,
	weight, potentialReward uint64,
) (CurrentSubnetValidator, error) {
	if staker.SubnetID() == constants.PrimaryNetworkID {
		return CurrentSubnetValidator{}, fmt.Errorf("%w: expected subnet validator", errUnexpectedStaker)
	}
	period, _, err := newCurrentStakingPeriod(txID, staker, startTime, endTime, weight)
	return CurrentSubnetValidator{
		StakingPeriod:   period,
		PotentialReward: potentialReward,
	}, err
}

// PendingDelegator is a delegation waiting to become current.
type PendingDelegator struct {
	StakingPeriod
}

// NewPendingDelegator returns a pending delegator built from staker.
func NewPendingDelegator(txID ids.ID, staker platform.ScheduledStaker) (PendingDelegator, error) {
	period, _, err := newPendingStakingPeriod(txID, staker)
	return PendingDelegator{StakingPeriod: period}, err
}

// Promote returns the current delegator corresponding to d.
func (d PendingDelegator) Promote(potentialReward uint64) CurrentDelegator {
	period := d.StakingPeriod
	period.priority = platform.PendingToCurrentPriorities[period.priority]
	return CurrentDelegator{
		StakingPeriod:   period,
		PotentialReward: potentialReward,
	}
}

// CurrentDelegator is an active delegation.
type CurrentDelegator struct {
	StakingPeriod
	PotentialReward uint64
}

// NewCurrentDelegator returns a current delegator built from staker.
func NewCurrentDelegator(
	txID ids.ID,
	staker platform.Staker,
	startTime, endTime time.Time,
	weight, potentialReward uint64,
) (CurrentDelegator, error) {
	period, _, err := newCurrentStakingPeriod(txID, staker, startTime, endTime, weight)
	return CurrentDelegator{
		StakingPeriod:   period,
		PotentialReward: potentialReward,
	}, err
}

// CurrentStaker is the sealed sum of [CurrentValidator] and
// [CurrentDelegator].
type CurrentStaker interface {
	Period() StakingPeriod
	Reward() uint64
	SubnetID() ids.ID
	NodeID() ids.NodeID
	currentStaker()
}

// CurrentValidator is the sealed sum of [CurrentPrimaryNetworkValidator] and
// [CurrentSubnetValidator].
type CurrentValidator interface {
	CurrentStaker
	currentValidator()
}

func (v CurrentPrimaryNetworkValidator) Period() StakingPeriod { return v.StakingPeriod }
func (v CurrentPrimaryNetworkValidator) Reward() uint64        { return v.PotentialReward }
func (CurrentPrimaryNetworkValidator) currentStaker()          {}

func (v CurrentSubnetValidator) Period() StakingPeriod { return v.StakingPeriod }
func (v CurrentSubnetValidator) Reward() uint64        { return v.PotentialReward }
func (CurrentSubnetValidator) currentStaker()          {}

func (d CurrentDelegator) Period() StakingPeriod { return d.StakingPeriod }
func (d CurrentDelegator) Reward() uint64        { return d.PotentialReward }
func (CurrentDelegator) currentStaker()          {}

// PendingStaker is the sealed sum of [PendingValidator] and [PendingDelegator].
type PendingStaker interface {
	Period() StakingPeriod
	SubnetID() ids.ID
	NodeID() ids.NodeID
	pendingStaker()
}

// PendingValidator is the sealed sum of [PendingPrimaryNetworkValidator] and
// [PendingSubnetValidator].
type PendingValidator interface {
	PendingStaker
	Promote(potentialReward uint64) CurrentValidator
	pendingValidator()
}

func (v PendingPrimaryNetworkValidator) Period() StakingPeriod { return v.StakingPeriod }
func (PendingPrimaryNetworkValidator) pendingStaker()          {}

func (v PendingSubnetValidator) Period() StakingPeriod { return v.StakingPeriod }
func (PendingSubnetValidator) pendingStaker()          {}

func (d PendingDelegator) Period() StakingPeriod { return d.StakingPeriod }
func (PendingDelegator) pendingStaker()          {}

func newPendingStakingPeriod(txID ids.ID, staker platform.ScheduledStaker) (StakingPeriod, *bls.PublicKey, error) {
	publicKey, _, err := staker.PublicKey()
	if err != nil {
		return StakingPeriod{}, nil, err
	}
	return StakingPeriod{
		TxID:      txID,
		Weight:    staker.Weight(),
		StartTime: staker.StartTime(),
		EndTime:   staker.EndTime(),
		priority:  staker.PendingPriority(),
		subnetID:  staker.SubnetID(),
		nodeID:    staker.NodeID(),
	}, publicKey, nil
}

func newCurrentStakingPeriod(
	txID ids.ID,
	staker platform.Staker,
	startTime, endTime time.Time,
	weight uint64,
) (StakingPeriod, *bls.PublicKey, error) {
	publicKey, _, err := staker.PublicKey()
	if err != nil {
		return StakingPeriod{}, nil, err
	}
	return StakingPeriod{
		TxID:      txID,
		Weight:    weight,
		StartTime: startTime,
		EndTime:   endTime,
		priority:  staker.CurrentPriority(),
		subnetID:  staker.SubnetID(),
		nodeID:    staker.NodeID(),
	}, publicKey, nil
}

func pendingStaker(period StakingPeriod, publicKey *bls.PublicKey) *Staker {
	return &Staker{
		TxID:      period.TxID,
		SubnetID:  period.SubnetID(),
		NodeID:    period.NodeID(),
		PublicKey: publicKey,
		Weight:    period.Weight,
		StartTime: period.StartTime,
		EndTime:   period.EndTime,
		NextTime:  period.StartTime,
		Priority:  period.priority,
	}
}

func currentStaker(period StakingPeriod, publicKey *bls.PublicKey, potentialReward uint64) *Staker {
	return &Staker{
		TxID:            period.TxID,
		SubnetID:        period.SubnetID(),
		NodeID:          period.NodeID(),
		PublicKey:       publicKey,
		Weight:          period.Weight,
		StartTime:       period.StartTime,
		EndTime:         period.EndTime,
		PotentialReward: potentialReward,
		NextTime:        period.EndTime,
		Priority:        period.priority,
	}
}

func pendingPrimaryNetworkValidator(staker *Staker) PendingPrimaryNetworkValidator {
	return PendingPrimaryNetworkValidator{
		StakingPeriod: stakingPeriod(staker),
		publicKey:     staker.PublicKey,
	}
}

func pendingSubnetValidator(staker *Staker) PendingSubnetValidator {
	return PendingSubnetValidator{
		StakingPeriod: stakingPeriod(staker),
	}
}

func pendingDelegator(staker *Staker) PendingDelegator {
	return PendingDelegator{StakingPeriod: stakingPeriod(staker)}
}

func currentPrimaryNetworkValidator(staker *Staker) CurrentPrimaryNetworkValidator {
	return CurrentPrimaryNetworkValidator{
		StakingPeriod:   stakingPeriod(staker),
		PotentialReward: staker.PotentialReward,
		publicKey:       staker.PublicKey,
	}
}

func currentSubnetValidator(staker *Staker) CurrentSubnetValidator {
	return CurrentSubnetValidator{
		StakingPeriod:   stakingPeriod(staker),
		PotentialReward: staker.PotentialReward,
	}
}

func currentDelegator(staker *Staker) CurrentDelegator {
	return CurrentDelegator{
		StakingPeriod:   stakingPeriod(staker),
		PotentialReward: staker.PotentialReward,
	}
}

func stakingPeriod(staker *Staker) StakingPeriod {
	return StakingPeriod{
		TxID:      staker.TxID,
		Weight:    staker.Weight,
		StartTime: staker.StartTime,
		EndTime:   staker.EndTime,
		priority:  staker.Priority,
		subnetID:  staker.SubnetID,
		nodeID:    staker.NodeID,
	}
}

func newCurrentStaker(staker *Staker) CurrentStaker {
	if !staker.Priority.IsValidator() {
		return currentDelegator(staker)
	}
	if staker.SubnetID == constants.PrimaryNetworkID {
		return currentPrimaryNetworkValidator(staker)
	}
	return currentSubnetValidator(staker)
}

func newPendingStaker(staker *Staker) PendingStaker {
	if !staker.Priority.IsValidator() {
		return pendingDelegator(staker)
	}
	if staker.SubnetID == constants.PrimaryNetworkID {
		return pendingPrimaryNetworkValidator(staker)
	}
	return pendingSubnetValidator(staker)
}

type currentDelegatorIterator struct{ iterator.Iterator[*Staker] }

func (it currentDelegatorIterator) Value() CurrentDelegator {
	return currentDelegator(it.Iterator.Value())
}

type pendingDelegatorIterator struct{ iterator.Iterator[*Staker] }

func (it pendingDelegatorIterator) Value() PendingDelegator {
	return pendingDelegator(it.Iterator.Value())
}

type currentStakerIterator struct{ iterator.Iterator[*Staker] }

func (it currentStakerIterator) Value() CurrentStaker {
	return newCurrentStaker(it.Iterator.Value())
}

type pendingStakerIterator struct{ iterator.Iterator[*Staker] }

func (it pendingStakerIterator) Value() PendingStaker {
	return newPendingStaker(it.Iterator.Value())
}

type adapterCurrentStakerIterator struct {
	iterator.Iterator[CurrentStaker]
}

func (it adapterCurrentStakerIterator) Value() *Staker {
	switch staker := it.Iterator.Value().(type) {
	case CurrentPrimaryNetworkValidator:
		return currentStaker(staker.StakingPeriod, staker.publicKey, staker.PotentialReward)
	case CurrentSubnetValidator:
		return currentStaker(staker.StakingPeriod, nil, staker.PotentialReward)
	case CurrentDelegator:
		return currentStaker(staker.StakingPeriod, nil, staker.PotentialReward)
	default:
		panic("unexpected current staker type")
	}
}

type adapterCurrentDelegatorIterator struct {
	iterator.Iterator[CurrentDelegator]
}

func (it adapterCurrentDelegatorIterator) Value() *Staker {
	delegator := it.Iterator.Value()
	return currentStaker(delegator.StakingPeriod, nil, delegator.PotentialReward)
}

type adapterPendingStakerIterator struct {
	iterator.Iterator[PendingStaker]
}

func (it adapterPendingStakerIterator) Value() *Staker {
	switch staker := it.Iterator.Value().(type) {
	case PendingPrimaryNetworkValidator:
		return pendingStaker(staker.StakingPeriod, staker.publicKey)
	case PendingSubnetValidator:
		return pendingStaker(staker.StakingPeriod, nil)
	case PendingDelegator:
		return pendingStaker(staker.StakingPeriod, nil)
	default:
		panic("unexpected pending staker type")
	}
}

type adapterPendingDelegatorIterator struct {
	iterator.Iterator[PendingDelegator]
}

func (it adapterPendingDelegatorIterator) Value() *Staker {
	delegator := it.Iterator.Value()
	return pendingStaker(delegator.StakingPeriod, nil)
}

func getStakerByTxID(it iterator.Iterator[*Staker], txID ids.ID) (*Staker, error) {
	defer it.Release()
	for it.Next() {
		if staker := it.Value(); staker.TxID == txID {
			return staker, nil
		}
	}
	return nil, database.ErrNotFound
}
