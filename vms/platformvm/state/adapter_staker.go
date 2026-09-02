package state

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
)

// StakingPeriod is the state a staker accumulates over one staking period.
//
// Immutable transaction data is duplicated onto this type only where callers
// need it to address the record.
type StakingPeriod struct {
	// TxID refers to the transaction that initially added this [Staker]. Data derived from the tx should be looked up
	// from the [State.GetTx] itself as the source-of-truth.
	TxID ids.ID

	// An auto-renewed validator rewrites these at the end of each cycle, so
	// they cannot be re-derived from the transaction.
	Weight    uint64
	StartTime time.Time
	EndTime   time.Time

	// TODO: derive at insertion time
	priority platform.Priority

	// The slot addressing this record. Callers holding one from an iterator
	// have no other source for it.
	subnetID ids.ID
	nodeID   ids.NodeID

	// publicKey is the BLS key the staker registered, captured from the adding
	// transaction at construction. Only Primary Network validators carry one.
	publicKey *bls.PublicKey
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
//
// TODO: remove. This is a capability of the adding transaction, and it is the
// only thing keeping priority observable.
func (p StakingPeriod) IsPermissionedValidator() bool {
	return p.priority.IsPermissionedValidator()
}

// PendingValidator is a validator waiting to become current. The network it
// validates comes from its staking period; the Primary Network and subnets
// have no distinct pending representation.
type PendingValidator struct {
	StakingPeriod
}

var _ PendingStaker = PendingValidator{}

// Promote returns the current validator corresponding to v.
func (v PendingValidator) Promote(potentialReward uint64) CurrentValidator {
	period := v.StakingPeriod
	period.priority = platform.PendingToCurrentPriorities[period.priority]
	return CurrentValidator{
		StakingPeriod:   period,
		PotentialReward: potentialReward,
	}
}

func (v PendingValidator) Period() StakingPeriod { return v.StakingPeriod }

func (PendingValidator) pendingStaker() {}

// CurrentValidator is an active validator. The network it validates comes from
// its staking period.
type CurrentValidator struct {
	StakingPeriod
	PotentialReward uint64
}

type ValidatorWithEndTime interface {
	platform.ValidatorStaker
	platform.BoundedStaker
}

// NewCurrentValidator returns a current validator built from staker.
func NewCurrentValidator(
	txID ids.ID,
	validator ValidatorWithEndTime,
	startTime, endTime time.Time,
	weight, potentialReward uint64,
) (CurrentValidator, error) {
	sp, err := newCurrentValidatorStakingPeriod(txID, validator, startTime, endTime, weight)
	if err != nil {
		return CurrentValidator{}, err
	}

	return CurrentValidator{
		StakingPeriod:   sp,
		PotentialReward: potentialReward,
	}, nil
}

func newCurrentValidatorStakingPeriod(
	txID ids.ID,
	staker platform.ValidatorStaker,
	start, end time.Time,
	weight uint64,
) (StakingPeriod, error) {
	sp, err := newCurrentStakingPeriod(txID, staker, start, end, weight)
	if err != nil {
		return StakingPeriod{}, err
	}

	sp.publicKey, err = validatorPublicKey(staker, staker.SubnetID())
	if err != nil {
		return StakingPeriod{}, err
	}

	return sp, nil
}

func newCurrentStakingPeriod(txID ids.ID, staker platform.Staker, start, end time.Time, weight uint64) (StakingPeriod, error) {
	return StakingPeriod{
		TxID:      txID,
		Weight:    weight,
		StartTime: start,
		EndTime:   end,
		priority:  staker.CurrentPriority(),
		subnetID:  staker.SubnetID(),
		nodeID:    staker.NodeID(),
	}, nil
}

// AutoRenewedStakingPeriod contains the mutable state of an auto-renewed
// validator.
type AutoRenewedStakingPeriod struct {
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

// AutoRenewedValidator is an active auto-renewed Primary Network validator.
//
// An auto-renewed validator is also readable through [CurrentValidator], which
// exposes only its common fields.
type AutoRenewedValidator struct {
	CurrentValidator
	AutoRenewedStakingPeriod
}

// NewCurrentAutoRenewedValidator returns a current auto-renewed validator
// built from staker.
func NewCurrentAutoRenewedValidator(
	txID ids.ID,
	staker *platform.AddAutoRenewedValidatorTx,
	startTime, endTime time.Time,
	weight, potentialReward uint64,
) (AutoRenewedValidator, error) {
	period, err := newCurrentValidatorStakingPeriod(txID, staker, startTime, endTime, weight)
	if err != nil {
		return AutoRenewedValidator{}, err
	}

	return AutoRenewedValidator{
		CurrentValidator: CurrentValidator{
			StakingPeriod:   period,
			PotentialReward: potentialReward,
		},
		AutoRenewedStakingPeriod: AutoRenewedStakingPeriod{
			AutoCompoundRewardShares: staker.AutoCompoundRewardShares,
			NextPeriod:               staker.Period,
		},
	}, nil
}

// PendingDelegator is a delegation waiting to become current.
type PendingDelegator struct {
	StakingPeriod
}

var _ PendingStaker = PendingDelegator{}

// Promote returns the current delegator corresponding to d.
func (d PendingDelegator) Promote(potentialReward uint64) CurrentDelegator {
	period := d.StakingPeriod
	period.priority = platform.PendingToCurrentPriorities[period.priority]
	return CurrentDelegator{
		StakingPeriod:   period,
		PotentialReward: potentialReward,
	}
}

func (d PendingDelegator) Period() StakingPeriod { return d.StakingPeriod }

func (PendingDelegator) pendingStaker() {}

// CurrentDelegator is an active delegation.
type CurrentDelegator struct {
	StakingPeriod
	PotentialReward uint64
}

// NewCurrentDelegator returns a current delegator built from staker.
func NewCurrentDelegator(
	txID ids.ID,
	staker platform.DelegatorStaker,
	startTime, endTime time.Time,
	weight, potentialReward uint64,
) (CurrentDelegator, error) {
	period, err := newCurrentStakingPeriod(txID, staker, startTime, endTime, weight)
	return CurrentDelegator{
		StakingPeriod:   period,
		PotentialReward: potentialReward,
	}, err
}

// CurrentStaker is the sealed sum of [CurrentValidator] and
// [CurrentDelegator]. Validator and delegator stay distinct because their legal
// operations differ; the Primary Network and subnets do not.
type CurrentStaker interface {
	Period() StakingPeriod
	Reward() uint64
	currentStaker()
}

func (v CurrentValidator) Period() StakingPeriod { return v.StakingPeriod }
func (v CurrentValidator) Reward() uint64        { return v.PotentialReward }
func (CurrentValidator) currentStaker()          {}

func (d CurrentDelegator) Period() StakingPeriod { return d.StakingPeriod }
func (d CurrentDelegator) Reward() uint64        { return d.PotentialReward }
func (CurrentDelegator) currentStaker()          {}

// PendingStaker is the sealed sum of [PendingValidator] and [PendingDelegator].
type PendingStaker interface {
	Period() StakingPeriod
	pendingStaker()
}

func newPendingStakingPeriod(txID ids.ID, staker platform.ScheduledStaker) (StakingPeriod, error) {
	return StakingPeriod{
		TxID:      txID,
		Weight:    staker.Weight(),
		StartTime: staker.StartTime(),
		EndTime:   staker.EndTime(),
		priority:  staker.PendingPriority(),
		subnetID:  staker.SubnetID(),
		nodeID:    staker.NodeID(),
	}, nil
}

func pendingStaker(period StakingPeriod) *Staker {
	return &Staker{
		TxID:      period.TxID,
		SubnetID:  period.SubnetID(),
		NodeID:    period.NodeID(),
		PublicKey: period.publicKey,
		Weight:    period.Weight,
		StartTime: period.StartTime,
		EndTime:   period.EndTime,
		NextTime:  period.StartTime,
		Priority:  period.priority,
	}
}

func currentStaker(period StakingPeriod, potentialReward uint64) *Staker {
	return &Staker{
		TxID:            period.TxID,
		SubnetID:        period.SubnetID(),
		NodeID:          period.NodeID(),
		PublicKey:       period.publicKey,
		Weight:          period.Weight,
		StartTime:       period.StartTime,
		EndTime:         period.EndTime,
		PotentialReward: potentialReward,
		NextTime:        period.EndTime,
		Priority:        period.priority,
	}
}

func pendingValidatorFromStaker(staker *Staker) PendingValidator {
	return PendingValidator{
		StakingPeriod: stakingPeriodFromStaker(staker),
	}
}

func pendingDelegatorFromStaker(staker *Staker) PendingDelegator {
	return PendingDelegator{StakingPeriod: stakingPeriodFromStaker(staker)}
}

func currentValidatorFromStaker(staker *Staker) CurrentValidator {
	return CurrentValidator{
		StakingPeriod:   stakingPeriodFromStaker(staker),
		PotentialReward: staker.PotentialReward,
	}
}

func currentDelegatorFromStaker(staker *Staker) CurrentDelegator {
	return CurrentDelegator{
		StakingPeriod:   stakingPeriodFromStaker(staker),
		PotentialReward: staker.PotentialReward,
	}
}

func stakingPeriodFromStaker(staker *Staker) StakingPeriod {
	return StakingPeriod{
		TxID:      staker.TxID,
		Weight:    staker.Weight,
		StartTime: staker.StartTime,
		EndTime:   staker.EndTime,
		priority:  staker.Priority,
		subnetID:  staker.SubnetID,
		nodeID:    staker.NodeID,
		publicKey: staker.PublicKey,
	}
}

// newCurrentStaker converts a native record to its public variant. The switch
// is exhaustive: an unhandled priority must not be silently misclassified,
// which would route the record into the wrong collection.
func newCurrentStaker(staker *Staker) CurrentStaker {
	switch staker.Priority {
	case platform.PrimaryNetworkDelegatorCurrentPriority,
		platform.SubnetPermissionlessDelegatorCurrentPriority:
		return currentDelegatorFromStaker(staker)
	case platform.PrimaryNetworkValidatorCurrentPriority,
		platform.SubnetPermissionedValidatorCurrentPriority,
		platform.SubnetPermissionlessValidatorCurrentPriority:
		return currentValidatorFromStaker(staker)
	default:
		panic(fmt.Sprintf("unexpected current staker priority %d", staker.Priority))
	}
}

// newPendingStaker converts a native record to its public variant. Exhaustive,
// as in [newCurrentStaker].
func newPendingStaker(staker *Staker) PendingStaker {
	switch staker.Priority {
	case platform.PrimaryNetworkDelegatorApricotPendingPriority,
		platform.PrimaryNetworkDelegatorBanffPendingPriority,
		platform.SubnetPermissionlessDelegatorPendingPriority:
		return pendingDelegatorFromStaker(staker)
	case platform.PrimaryNetworkValidatorPendingPriority,
		platform.SubnetPermissionedValidatorPendingPriority,
		platform.SubnetPermissionlessValidatorPendingPriority:
		return pendingValidatorFromStaker(staker)
	default:
		panic(fmt.Sprintf("unexpected pending staker priority %d", staker.Priority))
	}
}

type currentDelegatorIterator struct{ iterator.Iterator[*Staker] }

func (it currentDelegatorIterator) Value() CurrentDelegator {
	return currentDelegatorFromStaker(it.Iterator.Value())
}

type pendingDelegatorIterator struct{ iterator.Iterator[*Staker] }

func (it pendingDelegatorIterator) Value() PendingDelegator {
	return pendingDelegatorFromStaker(it.Iterator.Value())
}

type currentStakerIterator struct{ iterator.Iterator[*Staker] }

func (it currentStakerIterator) Value() CurrentStaker {
	return newCurrentStaker(it.Iterator.Value())
}

type pendingStakerIterator struct{ iterator.Iterator[*Staker] }

func (it pendingStakerIterator) Value() PendingStaker {
	return newPendingStaker(it.Iterator.Value())
}

type adapterCurrentDelegatorIterator struct {
	iterator.Iterator[CurrentDelegator]
}

func (it adapterCurrentDelegatorIterator) Value() *Staker {
	delegator := it.Iterator.Value()
	return currentStaker(delegator.StakingPeriod, delegator.PotentialReward)
}

type adapterPendingDelegatorIterator struct {
	iterator.Iterator[PendingDelegator]
}

func (it adapterPendingDelegatorIterator) Value() *Staker {
	delegator := it.Iterator.Value()
	return pendingStaker(delegator.StakingPeriod)
}
