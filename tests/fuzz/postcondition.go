// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// A postcondition checks, on nodes at or past the block that accepted a tx,
// the guarantee that the tx's type makes once it commits (Stateful Fuzzing.md
// 3). It runs only on an accepted tx, so it never says whether a tx should
// commit. It records nothing: the block scan learns every accepted tx. Each
// method fails t through the API reads it makes. A reward tx's method returns
// the broken guarantee as an error instead (requireOption).
//
// The check runs it on each node at the model's height (readState), so the
// node has applied the tx's block and no block the scan did not read. It
// holds the check's ctx because [platform.TxVisitor]'s methods take none.
type postcondition struct {
	ctx   context.Context
	r     *runner
	t     require.TestingT
	nodes []int
	txID  ids.ID
	// accepted is the tx with its block and height, unless the postcondition
	// judges a reward option.
	accepted acceptedTx
	// option is how the option block decided a reward tx. No op commits a
	// reward tx, so only the check sets it.
	option *decision
}

var _ platform.TxVisitor = (*postcondition)(nil)

// BaseTx has no check of its own: reconcile matches its outputs, the actors'
// UTXOs, on every node at the next check.
func (*postcondition) BaseTx(*platform.BaseTx) error { return nil }

// ImportTx has no check of its own: reconcile finds the imported UTXO on
// every node at the next check, once the model holds it.
func (*postcondition) ImportTx(*platform.ImportTx) error { return nil }

// ExportTx has no check of its own: the model is P-Chain only, and reconcile
// matches the change.
func (*postcondition) ExportTx(*platform.ExportTx) error { return nil }

func (p *postcondition) CreateSubnetTx(utx *platform.CreateSubnetTx) error {
	subnetOwner, ok := utx.Owner.(*secp256k1fx.OutputOwners)
	require.True(p.t, ok, "subnet %s has a %T owner, want an OutputOwners", p.txID, utx.Owner)
	p.r.requireSubnetOwner(p.ctx, p.t, p.nodes, p.txID, subnetOwner)
	return nil
}

// AddPermissionlessValidatorTx is checked before the model returns the
// stake: the model counts the stake as value, which would hide a tx that
// registered no staker.
func (p *postcondition) AddPermissionlessValidatorTx(utx *platform.AddPermissionlessValidatorTx) error {
	p.r.requireValidator(p.ctx, p.t, p.nodes, utx.NodeID(), utx.Wght, utx.End)
	return nil
}

// AddAutoRenewedValidatorTx's first period starts at the timestamp of the
// block that holds it.
func (p *postcondition) AddAutoRenewedValidatorTx(utx *platform.AddAutoRenewedValidatorTx) error {
	banff, ok := p.accepted.block.(platform.BanffBlock)
	require.True(p.t, ok, "block at height %d is a %T, which has no timestamp", p.accepted.height, p.accepted.block)
	end := uint64(banff.Timestamp().Unix()) + utx.Period
	p.r.requireValidator(p.ctx, p.t, p.nodes, utx.NodeID(), utx.Weight(), end)
	return nil
}

func (p *postcondition) TransferSubnetOwnershipTx(utx *platform.TransferSubnetOwnershipTx) error {
	subnetOwner, ok := utx.Owner.(*secp256k1fx.OutputOwners)
	require.True(p.t, ok, "subnet %s has a %T owner, want an OutputOwners", utx.Subnet, utx.Owner)
	p.r.requireSubnetOwner(p.ctx, p.t, p.nodes, utx.Subnet, subnetOwner)
	return nil
}

func (p *postcondition) ConvertSubnetToL1Tx(utx *platform.ConvertSubnetToL1Tx) error {
	for _, i := range p.nodes {
		got, err := p.r.clients[i].GetSubnet(p.ctx, utx.Subnet)
		require.NoError(p.t, err)
		require.False(p.t, got.IsPermissioned, "node %d reports converted subnet %s as permissioned", i, utx.Subnet)
		require.NotEqual(p.t, ids.Empty, got.ConversionID, "node %d reports no conversion of subnet %s", i, utx.Subnet)
		require.Equal(p.t, utx.ChainID, got.ManagerChainID, "node %d reports the wrong manager chain for L1 %s", i, utx.Subnet)
		require.Equal(p.t, []byte(utx.Address), got.ManagerAddress, "node %d reports the wrong manager address for L1 %s", i, utx.Subnet)
	}

	return nil
}

// IncreaseL1ValidatorBalanceTx checks only that the validator exists.
// checkL1Balances judges the balance, which drains every second.
func (p *postcondition) IncreaseL1ValidatorBalanceTx(utx *platform.IncreaseL1ValidatorBalanceTx) error {
	for _, i := range p.nodes {
		_, _, err := p.r.clients[i].GetL1Validator(p.ctx, utx.ValidationID)
		require.NoError(p.t, err, "node %d does not report L1 validator %s", i, utx.ValidationID)
	}

	return nil
}

// DisableL1ValidatorTx refunds the remaining balance. A validator that kept it
// would mint AVAX.
func (p *postcondition) DisableL1ValidatorTx(utx *platform.DisableL1ValidatorTx) error {
	for _, i := range p.nodes {
		vdr, _, err := p.r.clients[i].GetL1Validator(p.ctx, utx.ValidationID)
		require.NoError(p.t, err)
		require.Zero(p.t, vdr.Balance, "node %d: disabled L1 validator %s keeps balance %d", i, utx.ValidationID, vdr.Balance)
	}

	return nil
}

// SetAutoRenewedValidatorConfigTx's config shows until the period that was
// current at the last check ends. After that the validator may leave, so the
// check stops. A validator that the last check did not list has no known
// period, and is not checked.
func (p *postcondition) SetAutoRenewedValidatorConfigTx(utx *platform.SetAutoRenewedValidatorConfigTx) error {
	i := slices.IndexFunc(p.r.primaryValidators, func(v platformvm.ClientPermissionlessValidator) bool {
		return v.TxID == utx.TxID
	})
	if i < 0 {
		return nil
	}

	nodeID, end := p.r.primaryValidators[i].NodeID, p.r.primaryValidators[i].EndTime
	for _, n := range p.nodes {
		vdrs, err := p.r.clients[n].GetCurrentValidators(p.ctx, constants.PrimaryNetworkID, []ids.NodeID{nodeID})
		require.NoError(p.t, err)
		if p.r.reached(p.ctx, p.t, n, end) {
			continue
		}

		j := slices.IndexFunc(vdrs, func(v platformvm.ClientPermissionlessValidator) bool {
			return v.TxID == utx.TxID
		})
		require.NotEqual(p.t, -1, j, "node %d does not report auto-renewed validator %s, whose period ends at %d", n, utx.TxID, end)
		got := vdrs[j].AutoRenewedConfig
		require.NotNil(p.t, got, "node %d reports no auto-renewed config for validator %s", n, utx.TxID)
		require.Equal(p.t, utx.Period, got.NextPeriod, "node %d reports the wrong next period for validator %s", n, utx.TxID)
		require.Equal(p.t, utx.AutoCompoundRewardShares, got.AutoCompoundRewardShares, "node %d reports the wrong reward shares for validator %s", n, utx.TxID)
	}

	return nil
}

func (p *postcondition) CreateChainTx(utx *platform.CreateChainTx) error {
	for _, i := range p.nodes {
		chains, err := p.r.clients[i].GetBlockchains(p.ctx)
		require.NoError(p.t, err)

		j := slices.IndexFunc(chains, func(c platformvm.APIBlockchain) bool { return c.ID == p.txID })
		require.NotEqual(p.t, -1, j, "node %d does not report created chain %s", i, p.txID)
		require.Equal(p.t, utx.SubnetID, chains[j].SubnetID, "node %d reports chain %s on the wrong subnet", i, p.txID)
	}

	return nil
}

func (p *postcondition) AddSubnetValidatorTx(utx *platform.AddSubnetValidatorTx) error {
	p.r.requireSubnetValidator(p.ctx, p.t, p.nodes, utx.Subnet, utx.NodeID(), utx.End)
	return nil
}

func (p *postcondition) RemoveSubnetValidatorTx(utx *platform.RemoveSubnetValidatorTx) error {
	for _, i := range p.nodes {
		vdrs, err := p.r.clients[i].GetCurrentValidators(p.ctx, utx.Subnet, []ids.NodeID{utx.NodeID})
		require.NoError(p.t, err)
		require.Empty(p.t, subnetStakers(vdrs), "node %d still reports removed subnet validator %s on %s", i, utx.NodeID, utx.Subnet)
	}

	return nil
}

func (p *postcondition) AddPermissionlessDelegatorTx(utx *platform.AddPermissionlessDelegatorTx) error {
	p.r.requireDelegation(p.ctx, p.t, p.nodes, utx.NodeID(), utx.Wght, utx.End)
	return nil
}

// The disabled types have no check. After Etna the block's complexity limit
// refuses them as well as the executor (block/executor/verifier.go:538), so
// no single bug commits one.

func (*postcondition) AddValidatorTx(*platform.AddValidatorTx) error { return nil }

func (*postcondition) AddDelegatorTx(*platform.AddDelegatorTx) error { return nil }

func (*postcondition) TransformSubnetTx(*platform.TransformSubnetTx) error { return nil }

// AdvanceTimeTx has no check: the mempool drops it, so no op commits one
// until the block path exists.
func (*postcondition) AdvanceTimeTx(*platform.AdvanceTimeTx) error { return nil }

// RewardValidatorTx removes its staker on either option (checkOption). Only
// the check runs it, after the option block.
func (p *postcondition) RewardValidatorTx(*platform.RewardValidatorTx) error {
	return p.requireOption()
}

// RewardAutoRenewedValidatorTx removes its validator, or renews it until a new
// end time (checkOption). Only the check runs it, after the option block.
func (p *postcondition) RewardAutoRenewedValidatorTx(*platform.RewardAutoRenewedValidatorTx) error {
	return p.requireOption()
}

// requireOption returns errRewardOption if a node lists the staker of a
// reward tx otherwise than p.option, the option block that decided it, left it
// (checkOption). It reads each node's validators, then the node's chain time.
func (p *postcondition) requireOption() error {
	if p.option == nil {
		return nil
	}

	for _, i := range p.nodes {
		vdrs, err := p.r.clients[i].GetCurrentValidators(p.ctx, constants.PrimaryNetworkID, nil)
		require.NoError(p.t, err)
		now, err := p.r.clients[i].GetTimestamp(p.ctx)
		require.NoError(p.t, err)

		if err := checkOption(*p.option, vdrs, uint64(now.Unix())); err != nil {
			return fmt.Errorf("node %d, reward tx %s: %w", i, p.txID, err)
		}
	}

	return nil
}

// errRewardOption means a node lists a staker otherwise than the option block
// that decided its reward left it.
var errRewardOption = errors.New("validators disagree with the reward's option")

// checkOption returns errRewardOption unless vdrs, a node's current primary
// network validators, read before its chain time was now, agree with d. A
// staker that d removed is not listed. A validator that d renewed is listed,
// with an end time of d's time plus its new period, until now reaches it.
//
// Abort removes the staker, and so does commit of a RewardValidatorTx, or of
// a RewardAutoRenewedValidatorTx whose next period is 0
// (vms/platformvm/txs/executor/proposal_tx_executor.go). A tx ID is never
// added twice, so the staker does not come back. Commit with a next period
// starts a cycle at the old end time, the reward's time
// (getNextStakerToReward), that lasts the period
// (restakeAutoRenewedValidatorOnCommit), and nothing ends the cycle before
// the chain time reaches its end. The scan follows the period from the
// validator's tx and its config txs; it does not copy the restake.
func checkOption(d decision, vdrs []platformvm.ClientPermissionlessValidator, now uint64) error {
	i := slices.IndexFunc(vdrs, func(v platformvm.ClientPermissionlessValidator) bool {
		return v.TxID == d.staker
	})
	switch {
	case d.removes():
		if i >= 0 {
			return fmt.Errorf("%w: staker %s is listed after its reward at %d removed it", errRewardOption, d.staker, d.at)
		}
		return nil
	case d.renew == 0:
		return nil // the scan does not know the period
	}

	end := d.at + d.renew
	switch {
	case now >= end:
		return nil
	case i < 0:
		return fmt.Errorf("%w: validator %s is not listed, but its reward at %d renewed it until %d", errRewardOption, d.staker, d.at, end)
	case vdrs[i].EndTime != end:
		return fmt.Errorf("%w: validator %s is listed until %d, but its reward at %d renewed it until %d", errRewardOption, d.staker, vdrs[i].EndTime, d.at, end)
	default:
		return nil
	}
}

// The Warp-signed types have no check: issue does not build them yet.

func (*postcondition) RegisterL1ValidatorTx(*platform.RegisterL1ValidatorTx) error { return nil }

func (*postcondition) SetL1ValidatorWeightTx(*platform.SetL1ValidatorWeightTx) error { return nil }

// requireSubnetOwner fails the test unless each of nodes reports owner want
// for the subnet. GetSubnet reports the owner of a converted subnet too, so
// the check holds whether or not the subnet is an L1.
func (r *runner) requireSubnetOwner(ctx context.Context, t require.TestingT, nodes []int, id ids.ID, want *secp256k1fx.OutputOwners) {
	for _, i := range nodes {
		got, err := r.clients[i].GetSubnet(ctx, id)
		require.NoError(t, err)
		require.Equal(t, want.Threshold, got.Threshold, "node %d reports wrong threshold for subnet %s", i, id)
		require.Equal(t, want.Addrs, got.ControlKeys, "node %d reports wrong owner for subnet %s", i, id)
	}
}

// requireValidator fails the test unless each of nodes reports id as a
// current primary network validator with the given weight, until end. Once a
// node's chain time reaches end, the validator may have ended, or renewed with
// its rewards restaked, and the check stops.
func (r *runner) requireValidator(ctx context.Context, t require.TestingT, nodes []int, id ids.NodeID, weight, end uint64) {
	for _, i := range nodes {
		vdrs, err := r.clients[i].GetCurrentValidators(ctx, constants.PrimaryNetworkID, []ids.NodeID{id})
		require.NoError(t, err)
		if r.reached(ctx, t, i, end) {
			continue
		}

		require.Len(t, vdrs, 1, "node %d does not report validator %s, which ends at %d", i, id, end)
		require.Equal(t, weight, vdrs[0].Weight, "node %d reports weight %d for validator %s, want %d", i, vdrs[0].Weight, id, weight)
	}
}

// requireDelegation fails the test unless each of nodes reports the target
// validator with delegation weight at least weight, until end. Other
// delegations only add weight, so the check is a lower bound. Once a node's
// chain time reaches end, the delegation may have ended, and the check stops.
func (r *runner) requireDelegation(ctx context.Context, t require.TestingT, nodes []int, target ids.NodeID, weight, end uint64) {
	for _, i := range nodes {
		vdrs, err := r.clients[i].GetCurrentValidators(ctx, constants.PrimaryNetworkID, []ids.NodeID{target})
		require.NoError(t, err)
		if r.reached(ctx, t, i, end) {
			continue
		}

		require.Len(t, vdrs, 1, "node %d does not report delegation target %s", i, target)
		require.NotNil(t, vdrs[0].DelegatorWeight, "node %d reports no delegation on %s", i, target)
		require.GreaterOrEqual(t, *vdrs[0].DelegatorWeight, weight, "node %d reports delegation weight %d on %s, want at least %d", i, *vdrs[0].DelegatorWeight, target, weight)
	}
}

// requireSubnetValidator fails the test unless each of nodes reports the node
// as a current validator of the subnet, until end.
func (r *runner) requireSubnetValidator(ctx context.Context, t require.TestingT, nodes []int, subnetID ids.ID, nodeID ids.NodeID, end uint64) {
	for _, i := range nodes {
		vdrs, err := r.clients[i].GetCurrentValidators(ctx, subnetID, []ids.NodeID{nodeID})
		require.NoError(t, err)
		if len(subnetStakers(vdrs)) == 0 {
			require.True(t, r.reached(ctx, t, i, end), "node %d does not report subnet validator %s on %s, which ends at %d", i, nodeID, subnetID, end)
		}
	}
}

// subnetStakers returns the validators in vdrs that an AddSubnetValidatorTx
// added. Once a subnet converts to an L1, GetCurrentValidators also lists its
// L1 validators (vms/platformvm/service.go getL1Validators), which can reuse a
// removed staker's node ID. Only L1 validators carry a validation ID.
func subnetStakers(vdrs []platformvm.ClientPermissionlessValidator) []platformvm.ClientPermissionlessValidator {
	return slices.DeleteFunc(vdrs, func(v platformvm.ClientPermissionlessValidator) bool {
		return v.ValidationID != nil
	})
}

// reached reports whether node i's chain time, the timestamp of its last
// accepted block, is at or past end. A staker can leave the current set from
// then on, because the chain ends it in a block at its end time. Call it after
// the read that it excuses: chain time only grows, so a time before end means
// the staker was still current at that read.
func (r *runner) reached(ctx context.Context, t require.TestingT, i int, end uint64) bool {
	now, err := r.clients[i].GetTimestamp(ctx)
	require.NoError(t, err)

	return uint64(now.Unix()) >= end
}

// An acceptedTx is a tx of a scanned block, at the height of the block.
type acceptedTx struct {
	tx     *platform.Tx
	height uint64
	block  platform.Block
	seq    int // the tx's place among every tx the scan read
}

// postconditionQueue holds the txs of the scanned blocks until a check reads
// a node at the model's height and judges their postconditions there. The
// zero value is ready to use.
//
// A postcondition reads the node's state now, not at its tx's height, so a
// later tx of the scanned blocks can change what it reads: a second transfer
// of one subnet changes its owner. The queue judges a tx only if no later tx
// has its subject. It knows every tx up to the model's height, the height the
// reads are at, so none it does not know can come between.
type postconditionQueue struct {
	waiting []acceptedTx
	last    map[string]int // by subject, the seq of the last tx about it
	seq     int
}

// add queues tx, from blk at height h.
func (q *postconditionQueue) add(tx *platform.Tx, h uint64, blk platform.Block) {
	q.seq++
	q.waiting = append(q.waiting, acceptedTx{tx: tx, height: h, block: blk, seq: q.seq})
	if s := subject(tx); s != "" {
		if q.last == nil {
			q.last = make(map[string]int)
		}
		q.last[s] = q.seq
	}
}

// judgeable returns the waiting txs whose subject no later tx has, and the
// others, in scan order.
func (q *postconditionQueue) judgeable() (judged, skipped []acceptedTx) {
	for _, a := range q.waiting {
		if s := subject(a.tx); s != "" && q.last[s] != a.seq {
			skipped = append(skipped, a)
			continue
		}
		judged = append(judged, a)
	}

	return judged, skipped
}

// clear drops every waiting tx, once a check judged them.
func (q *postconditionQueue) clear() {
	q.waiting = nil
}

// subject returns what tx's postcondition reads that a later tx can change,
// or "" if no later tx changes it: a subnet's owner, an L1 validator's
// balance, an auto-renewed validator's config, or whether a node validates a
// subnet. A staker postcondition holds until the staker's end time, which no
// later tx moves, and the others read what never changes once set.
func subject(tx *platform.Tx) string {
	switch utx := tx.Unsigned.(type) {
	case *platform.CreateSubnetTx:
		return "owner " + tx.ID().String()
	case *platform.TransferSubnetOwnershipTx:
		return "owner " + utx.Subnet.String()
	case *platform.IncreaseL1ValidatorBalanceTx:
		return "balance " + utx.ValidationID.String()
	case *platform.DisableL1ValidatorTx:
		return "balance " + utx.ValidationID.String()
	case *platform.SetAutoRenewedValidatorConfigTx:
		return "config " + utx.TxID.String()
	case *platform.AddSubnetValidatorTx:
		return "subnet validator " + utx.Subnet.String() + " " + utx.NodeID().String()
	case *platform.RemoveSubnetValidatorTx:
		return "subnet validator " + utx.Subnet.String() + " " + utx.NodeID.String()
	default:
		return ""
	}
}
