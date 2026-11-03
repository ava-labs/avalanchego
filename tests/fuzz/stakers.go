// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
)

// stakers follows each staker through the accepted blocks: when its reward tx
// may come, when it leaves, and how each reward proposal was decided. The zero
// value is ready to use.
type stakers struct {
	ends   map[ids.ID]uint64 // by staker tx ID, Unix seconds
	cycles map[ids.ID]*cycle // by auto-renewed validator tx ID
	// delegations maps each delegator tx to its validator's node ID.
	delegations map[ids.ID]ids.NodeID
	pending     *proposal       // from the last proposal block, until its option block
	left        set.Set[ids.ID] // stakers that an option block removed since the last check
	// decided holds each reward proposal that an option block decided since
	// the last check, in block order.
	decided []decision
	// delegated holds the node IDs of the validators whose delegations a
	// reward proposal since the last check rewarded, which can raise their
	// accrued delegatee reward.
	delegated set.Set[ids.NodeID]
}

// cycle is an auto-renewed validator's staking cycle.
type cycle struct {
	end  uint64 // Unix seconds
	next uint64 // the period of the cycle after it, in seconds
}

// A proposal is a reward tx in an accepted proposal block. Abort removes its
// staker, and so does commit when onCommit is set. Otherwise commit renews an
// auto-renewed validator for renew seconds, or for a period the scan does not
// know when renew is 0.
type proposal struct {
	tx       *platform.Tx // a RewardValidatorTx or RewardAutoRenewedValidatorTx
	staker   ids.ID
	at       uint64 // the block's time, Unix seconds
	onCommit bool
	renew    uint64
}

// A decision is a reward proposal and the option block that decided it.
type decision struct {
	proposal
	commit bool
}

// removes reports whether d's option removed its staker.
func (d decision) removes() bool {
	return !d.commit || d.onCommit
}

// payee returns the ID that d's rewards pay under: a RewardValidatorTx pays
// under its staker's tx ID, and a RewardAutoRenewedValidatorTx under its own,
// because the staker keeps its ID across cycles (proposal_tx_executor.go
// rewardValidatorTx, mintRewards).
func (d decision) payee() ids.ID {
	if _, ok := d.tx.Unsigned.(*platform.RewardAutoRenewedValidatorTx); ok {
		return d.tx.ID()
	}

	return d.staker
}

var errRewardTime = errors.New("reward tx is not at its staker's end time")

// scan records the stakers that blk, a block every node accepted, adds and
// configures, the reward proposal it holds, and how its option decided the
// proposal. It returns errRewardTime if a reward tx in blk is not at its
// staker's end time: for an auto-renewed validator, if it comes before its
// cycle ends, and for any other staker, if it comes at any other time.
//
// The executor rewards a staker only when the chain time is its end time
// (vms/platformvm/txs/executor/proposal_tx_executor.go getNextStakerToReward),
// and a Banff block sets the chain time to its own timestamp before its txs
// run (vms/platformvm/block/executor/verifier.go BanffStandardBlock,
// BanffProposalBlock). A staker's end time is its tx's end time
// (standard_tx_executor.go putStaker). An auto-renewed validator's first cycle
// starts at the time of the block that adds it and lasts its tx's period. Each
// later cycle starts where the last one ends, for the period that the last
// config tx set (proposal_tx_executor.go restakeAutoRenewedValidatorOnCommit).
// scan uses that only until the next check, when sync replaces it with the
// cycle that the API lists. A reward that removes the validator
// ends its cycles, so scan need not know which option won to time the next
// reward.
func (s *stakers) scan(blk platform.Block) error {
	// The test network starts after Banff, so only Banff blocks are accepted.
	switch blk.(type) {
	case *platform.BanffCommitBlock:
		s.decide(true)
		return nil
	case *platform.BanffAbortBlock:
		s.decide(false)
		return nil
	}

	b, ok := blk.(platform.BanffBlock)
	if !ok {
		return nil
	}

	at := uint64(b.Timestamp().Unix())
	for _, tx := range blk.Txs() {
		switch utx := tx.Unsigned.(type) {
		case *platform.AddAutoRenewedValidatorTx:
			if s.cycles == nil {
				s.cycles = make(map[ids.ID]*cycle)
			}
			s.cycles[tx.ID()] = &cycle{end: at + utx.Period, next: utx.Period}
		case *platform.SetAutoRenewedValidatorConfigTx:
			if c, ok := s.cycles[utx.TxID]; ok {
				c.next = utx.Period
			}
		case *platform.RewardValidatorTx:
			// Commit and abort both return the stake
			// (proposal_tx_executor.go rewardValidatorTx, rewardDelegatorTx).
			s.pending = &proposal{tx: tx, staker: utx.TxID, at: at, onCommit: true}
			if nodeID, ok := s.delegations[utx.TxID]; ok {
				s.delegated.Add(nodeID)
			}
			if end, ok := s.ends[utx.TxID]; ok && at != end {
				return fmt.Errorf("%w: RewardValidatorTx %s at %d, but staker %s ends at %d", errRewardTime, tx.ID(), at, utx.TxID, end)
			}
		case *platform.RewardAutoRenewedValidatorTx:
			s.pending = &proposal{tx: tx, staker: utx.TxID, at: at}
			c, ok := s.cycles[utx.TxID]
			if !ok {
				continue
			}

			// Commit restakes unless the next period is 0
			// (proposal_tx_executor.go RewardAutoRenewedValidatorTx).
			s.pending.onCommit, s.pending.renew = c.next == 0, c.next
			if at < c.end {
				return fmt.Errorf("%w: RewardAutoRenewedValidatorTx %s at %d, but the cycle of %s ends at %d", errRewardTime, tx.ID(), at, utx.TxID, c.end)
			}
			c.end += c.next
		case platform.BoundedStaker:
			if s.ends == nil {
				s.ends = make(map[ids.ID]uint64)
			}
			s.ends[tx.ID()] = uint64(utx.EndTime().Unix())
			if d, ok := utx.(platform.DelegatorTx); ok {
				if s.delegations == nil {
					s.delegations = make(map[ids.ID]ids.NodeID)
				}
				s.delegations[tx.ID()] = d.NodeID()
			}
		}
	}

	return nil
}

// sync sets the cycle of each auto-renewed validator in vdrs that scan
// follows to what the API lists: the cycle ends at its EndTime, and the next
// one lasts its NextPeriod. vdrs must be the current primary network
// validators at the height of the last scanned block, or a later reward
// looks early.
//
// A validator whose reward proposal waits for its option keeps the cycle
// that scan derived: the API still lists the cycle that the reward ends.
func (s *stakers) sync(vdrs []platformvm.ClientPermissionlessValidator) {
	for _, v := range vdrs {
		c, ok := s.cycles[v.TxID]
		if !ok || v.AutoRenewedConfig == nil || s.pending != nil && s.pending.staker == v.TxID {
			continue
		}

		c.end, c.next = v.EndTime, v.AutoRenewedConfig.NextPeriod
	}
}

// decide records how the accepted option decided the last reward proposal,
// and that its staker left if the option removes it.
func (s *stakers) decide(commit bool) {
	if s.pending == nil {
		return // the proposal came before the first scanned block
	}

	d := decision{proposal: *s.pending, commit: commit}
	if d.removes() {
		s.left.Add(d.staker)
	}
	s.decided = append(s.decided, d)
	s.pending = nil
}

var errOverdue = errors.New("stake is not returned after its staker left")

// overdueReturns returns errOverdue if returns, the stake returns not yet
// found, still holds one of a staker in left. Run it after reconcile.
//
// The option block that removes a staker returns its stake in the same block
// (proposal_tx_executor.go unstakeUTXOs, on both branches of a
// RewardValidatorTx, on abort of a RewardAutoRenewedValidatorTx, and on its
// commit when the next period is 0). The check reads the UTXOs at the height
// of the last block it scanned, so every return for a staker in left is there.
func overdueReturns(returns map[ids.ID]*avax.UTXO, left set.Set[ids.ID]) error {
	for _, id := range slices.SortedFunc(maps.Keys(returns), ids.ID.Compare) {
		if u := returns[id]; left.Contains(u.TxID) {
			return fmt.Errorf("%w: staker %s left, but its stake %s is not among the actors' UTXOs", errOverdue, u.TxID, id)
		}
	}

	return nil
}

// accrual is a validator's accrued delegatee reward, and the start of the
// cycle it accrued in.
type accrual struct {
	start  uint64
	reward uint64
}

var errAccrual = errors.New("accrued delegatee reward is wrong")

// checkAccruals returns errAccrual if a validator in vdrs, one node's current
// primary network validators, reports an accrued delegatee reward that is not
// below supply, or that is below what last holds for it in the same cycle. It
// returns the accruals of vdrs, for the next check.
//
// The API reports the validator's pending commission
// (vms/platformvm/service.go GetCurrentValidators, stakingInfo.DelegateeReward).
// Each delegation that ends adds to it (proposal_tx_executor.go
// rewardDelegatorTx), and only a restake sets it to 0
// (restakeAutoRenewedValidatorOnCommit), which also moves the validator's
// start time to the new cycle. The reward is a part of a delegator's potential
// reward, which the supply already counts (standard_tx_executor.go putStaker),
// so it stays below the supply.
func checkAccruals(last map[ids.ID]accrual, vdrs []platformvm.ClientPermissionlessValidator, supply uint64) (map[ids.ID]accrual, error) {
	next := make(map[ids.ID]accrual, len(vdrs))
	for _, v := range vdrs {
		if v.AccruedDelegateeReward == nil {
			continue
		}

		now := accrual{start: v.StartTime, reward: *v.AccruedDelegateeReward}
		if now.reward >= supply {
			return nil, fmt.Errorf("%w: validator %s reports %d, but the supply is %d", errAccrual, v.TxID, now.reward, supply)
		}
		if before, ok := last[v.TxID]; ok && before.start == now.start && now.reward < before.reward {
			return nil, fmt.Errorf("%w: validator %s reports %d, down from %d in the same cycle", errAccrual, v.TxID, now.reward, before.reward)
		}
		next[v.TxID] = now
	}

	return next, nil
}

var errMaxStake = errors.New("delegations take a validator above the maximum stake")

// checkMaxStake returns errMaxStake if a validator in vdrs, one node's current
// primary network validators, has delegations and a weight plus delegator
// weight above maxStake.
//
// A delegation is refused when the validator's weight plus every current
// delegation would pass maxStake (txs/executor/staker_tx_verification.go
// verifyAddPermissionlessDelegatorTx, overDelegated). Delegators leave no
// later than their validator, and first when they share its end time
// (state/staker.go Less), so a restake never meets one. The API sums the
// delegations of one consistent state (vms/platformvm/service.go
// GetCurrentValidators). A validator that weighs more than maxStake alone, as
// a genesis validator can, takes no delegations.
func checkMaxStake(vdrs []platformvm.ClientPermissionlessValidator, maxStake uint64) error {
	for _, v := range vdrs {
		if v.DelegatorWeight == nil || *v.DelegatorWeight == 0 {
			continue
		}

		if total := v.Weight + *v.DelegatorWeight; total > maxStake {
			return fmt.Errorf("%w: validator %s weighs %d with %d delegated, %d in all, above %d", errMaxStake, v.TxID, v.Weight, *v.DelegatorWeight, total, maxStake)
		}
	}

	return nil
}

var errRewardAmount = errors.New("reward exceeds what its validator was listed to earn")

// checkRewardAmount returns errRewardAmount if paid, what the actors got from
// d's reward, exceeds what listed, a node's validators at the last check,
// promised d's validator: its potential reward, its accrued delegatee reward,
// and, for an auto-renewed validator, what it restaked. It judges a validator
// only if the listing is of the cycle that d rewards, and only if no
// delegation to it was rewarded since (delegated).
//
// It is a bound, not the reward formula. A reward pays the potential reward
// at most, the pending commission (accruedDelegateeReward), and, when an
// auto-renewed validator leaves, what earlier cycles restaked
// (vms/platformvm/txs/executor/proposal_tx_executor.go rewardValidatorTx,
// mintRewardOnAbort, restakeAutoRenewedValidatorOnCommit; the API's
// restakedValidationRewards and restakedDelegateeRewards). Within a cycle the
// potential reward and the restaked amounts stay fixed, and only a rewarded
// delegation raises the commission (rewardDelegatorTx). A listing whose end
// time is the reward's time was read before the reward, or the validator
// would be gone or renewed to a later end.
func checkRewardAmount(d decision, listed []platformvm.ClientPermissionlessValidator, delegated set.Set[ids.NodeID], paid uint64) error {
	i := slices.IndexFunc(listed, func(v platformvm.ClientPermissionlessValidator) bool {
		return v.TxID == d.staker
	})
	if i < 0 {
		return nil // a delegator, or a validator added since
	}

	v := listed[i]
	if v.EndTime != d.at || delegated.Contains(v.NodeID) || v.PotentialReward == nil || v.AccruedDelegateeReward == nil {
		return nil
	}

	bound := *v.PotentialReward + *v.AccruedDelegateeReward
	if c := v.AutoRenewedConfig; c != nil {
		if c.RestakedValidationRewards == nil || c.RestakedDelegateeRewards == nil {
			return nil
		}
		bound += *c.RestakedValidationRewards + *c.RestakedDelegateeRewards
	}
	if paid > bound {
		return fmt.Errorf("%w: the reward at %d paid %d to validator %s, listed with %d potential reward, %d accrued delegatee reward, and %d restaked", errRewardAmount, d.at, paid, d.staker, *v.PotentialReward, *v.AccruedDelegateeReward, bound-*v.PotentialReward-*v.AccruedDelegateeReward)
	}

	return nil
}

var errRewardUTXOs = errors.New("indexed reward UTXOs differ from the reward UTXOs found")

// checkRewardUTXOs returns errRewardUTXOs unless indexed, the actors' reward
// UTXOs that a node's getRewardUTXOs lists under payee, are found, the
// actors' reward UTXOs under payee that reconcile found, in any order.
//
// The executor adds each reward UTXO to the UTXO set and to the reward index
// under payee together, on both options (proposal_tx_executor.go
// rewardValidatorTx, rewardDelegatorTx, mintRewards). A reward UTXO cannot be
// spent before reconcile finds it, because the wallet learns it there.
func checkRewardUTXOs(payee ids.ID, indexed, found [][]byte) error {
	indexed, found = slices.Clone(indexed), slices.Clone(found)
	slices.SortFunc(indexed, bytes.Compare)
	slices.SortFunc(found, bytes.Compare)
	if !slices.EqualFunc(indexed, found, bytes.Equal) {
		return fmt.Errorf("%w: %s indexes %d, %d found", errRewardUTXOs, payee, len(indexed), len(found))
	}

	return nil
}
