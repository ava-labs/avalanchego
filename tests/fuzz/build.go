// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"slices"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// errUnsupported means the generator cannot build a tx of the type yet.
var errUnsupported = errors.New("tx type is not supported yet")

// txTypes holds a zero value of each tx type that issue draws from. The
// disabled and proposer-only types are here too: nodes must reject them, and
// the oracles judge a node that does not.
var txTypes = []platform.UnsignedTx{
	&platform.BaseTx{},
	&platform.CreateSubnetTx{},
	&platform.AddPermissionlessValidatorTx{},
	&platform.AddAutoRenewedValidatorTx{},
	&platform.ExportTx{},
	&platform.ImportTx{},

	// Reference an entity that the model holds, drawn by the ID rule.
	&platform.TransferSubnetOwnershipTx{},
	&platform.ConvertSubnetToL1Tx{},
	&platform.IncreaseL1ValidatorBalanceTx{},
	&platform.DisableL1ValidatorTx{},
	&platform.SetAutoRenewedValidatorConfigTx{},
	&platform.CreateChainTx{},
	&platform.AddSubnetValidatorTx{},
	&platform.RemoveSubnetValidatorTx{},
	&platform.AddPermissionlessDelegatorTx{},

	// Disabled by an upgrade.
	&platform.AddValidatorTx{},
	&platform.AddDelegatorTx{},
	&platform.TransformSubnetTx{},

	// Built only by a block proposer. The mempool drops them.
	&platform.AdvanceTimeTx{},
	&platform.RewardValidatorTx{},
	&platform.RewardAutoRenewedValidatorTx{},
}

// txName returns the name of utx's type, which is also its
// [platform.TxVisitor] method name.
func txName(utx platform.UnsignedTx) string {
	return reflect.TypeOf(utx).Elem().Name()
}

// build returns a new unsigned tx of proto's type, with each field drawn from
// t against the model.
func (r *runner) build(t *rapid.T, proto platform.UnsignedTx) (platform.UnsignedTx, error) {
	g := &generator{r: r, t: t}
	if err := proto.Visit(g); err != nil {
		return nil, err
	}

	return g.tx, nil
}

// A generator builds a new tx of the type it visits and keeps it in tx. It
// ignores the visited value.
type generator struct {
	r  *runner
	t  *rapid.T
	tx platform.UnsignedTx
	// offKind reports that a reference is not an active entity of its kind.
	offKind bool
}

var _ platform.TxVisitor = (*generator)(nil)

func (g *generator) BaseTx(*platform.BaseTx) error {
	payer := g.r.drawPayer(g.t, g.r.actors)
	utx, err := g.r.builder(payer).NewBaseTx(g.r.payment(g.t, g.r.drawRecipient(g.t)), options(g.t, payer)...)
	if err != nil {
		return err
	}

	g.tx = utx
	return nil
}

func (g *generator) CreateSubnetTx(*platform.CreateSubnetTx) error {
	subnetOwner := g.r.multisig(g.t)
	payer := g.r.drawPayer(g.t, g.r.actors)
	utx, err := g.r.builder(payer).NewCreateSubnetTx(subnetOwner, options(g.t, payer)...)
	if err != nil {
		return err
	}

	g.tx = utx
	return nil
}

// newNodeID draws the node ID of a new validator. A new validator matches no
// active entity, so it is any node ID seen, such as one that ended or is still
// validating, or a fresh one.
func (g *generator) newNodeID() ids.NodeID {
	return drawID(g.t, g.r.ev, "nodeID", nil, &g.r.seenNodeIDs.all)
}

// newValidator draws the node ID and BLS key of a new primary network
// validator. Half the time it is online: the fuzzer holds its keys and keeps a
// peer with them on every honest node (connectStaker), so the nodes measure
// its uptime and its reward can commit. Otherwise no node is behind its node
// ID (newNodeID), its uptime is 0, and its reward aborts.
func (g *generator) newValidator() (ids.NodeID, *signer.ProofOfPossession) {
	if rapid.Bool().Draw(g.t, "online") {
		return g.r.connectStaker(g.t)
	}

	return g.newNodeID(), proofOfPossession(g.t)
}

// connectStaker makes the staking and BLS keys of a new validator, and
// connects a peer with them to every honest node, so that the nodes measure
// the uptime of the validator with that node ID. It returns the node ID and
// the BLS key's proof of possession. The peers stay until the node that a
// check reads stops listing the validator (applySync), or until t ends. Like
// the proposer's, they vote for whatever a node queries (byzantinePeer.vote).
//
// The node ID comes from a TLS key, not from rapid's bytes, so a logged
// program names it but cannot make it again.
func (r *runner) connectStaker(t *rapid.T) (ids.NodeID, *signer.ProofOfPossession) {
	key, staker := newValidatorKey(t)
	peers := make([]*byzantinePeer, len(r.stakingAddrs))
	for i, addr := range r.stakingAddrs {
		p, err := connectValidator(t.Context(), r.network, addr, key, true)
		require.NoError(t, err)
		peers[i] = p
	}
	t.Cleanup(func() {
		require.NoError(t, closePeers(context.WithoutCancel(t.Context()), peers))
	})

	if r.online == nil {
		r.online = make(map[ids.NodeID][]*byzantinePeer)
	}
	r.online[key.nodeID] = peers
	r.ev.Append("draws", draw{Label: "nodeID", Ref: "online", ID: key.nodeID.String()})

	return key.nodeID, staker.Signer
}

// AddPermissionlessValidatorTx builds a primary network validator, online or
// not (newValidator).
func (g *generator) AddPermissionlessValidatorTx(*platform.AddPermissionlessValidatorTx) error {
	payer := g.r.drawPayer(g.t, g.r.actors)
	nodeID, pop := g.newValidator()
	vdr := &platform.SubnetValidator{
		Validator: platform.Validator{
			NodeID: nodeID,
			End:    endTime(stakeDuration(g.t, heliconMinStakeDuration)),
			Wght:   stakeWeight(g.t, g.r.minValidatorStake),
		},
		Subnet: constants.PrimaryNetworkID,
	}
	utx, err := g.r.builder(payer).NewAddPermissionlessValidatorTx(vdr, pop, g.r.avaxAssetID, owner(payer), owner(payer), delegationShares(g.t), options(g.t, payer)...)
	if err != nil {
		return err
	}

	g.tx = utx
	return nil
}

// AddAutoRenewedValidatorTx builds an auto-renewed validator, online or not
// (newValidator).
func (g *generator) AddAutoRenewedValidatorTx(*platform.AddAutoRenewedValidatorTx) error {
	payer := g.r.drawPayer(g.t, g.r.actors)
	nodeID, pop := g.newValidator()
	shares := delegationShares(g.t)
	utx, err := g.r.builder(payer).NewAddAutoRenewedValidatorTx(
		nodeID, stakeWeight(g.t, g.r.minValidatorStake), pop,
		owner(payer), owner(payer), owner(payer),
		shares, shares,
		stakeDuration(g.t, heliconMinStakeDuration),
		options(g.t, payer)...,
	)
	if err != nil {
		return err
	}

	g.tx = utx
	return nil
}

// ExportTx exports AVAX to the X-Chain. The model is P-Chain only, and the
// value oracle allows value to leave.
func (g *generator) ExportTx(*platform.ExportTx) error {
	payer := g.r.drawPayer(g.t, g.r.actors)
	utx, err := g.r.builder(payer).NewExportTx(g.r.xChainID, g.r.payment(g.t, g.r.drawRecipient(g.t)), options(g.t, payer)...)
	if err != nil {
		return err
	}

	g.tx = utx
	return nil
}

// ImportTx imports the payer's atomic UTXOs from the X-Chain, which check
// reads from the nodes. The builder imports only the payer's, so the payer is
// drawn from the actors that hold one, or from every actor when none does.
func (g *generator) ImportTx(*platform.ImportTx) error {
	var holders []ids.ShortID
	for _, a := range g.r.actors {
		utxos, err := payerUTXOs{Backend: g.r.backend, payer: a}.UTXOs(g.t.Context(), g.r.xChainID)
		if err != nil {
			return err
		}
		if len(utxos) > 0 {
			holders = append(holders, a)
		}
	}
	if len(holders) == 0 {
		holders = g.r.actors
	}

	payer := g.r.drawPayer(g.t, holders)
	utx, err := g.r.builder(payer).NewImportTx(g.r.xChainID, owner(g.r.drawRecipient(g.t)), options(g.t, payer)...)
	if err != nil {
		return err
	}

	g.tx = utx
	return nil
}

func (g *generator) AddValidatorTx(*platform.AddValidatorTx) error {
	payer := g.r.drawPayer(g.t, g.r.actors)
	vdr := &platform.Validator{
		NodeID: g.newNodeID(),
		End:    endTime(stakeDuration(g.t, minStakeDuration)),
		Wght:   stakeWeight(g.t, g.r.minValidatorStake),
	}
	utx, err := g.r.builder(payer).NewAddValidatorTx(vdr, owner(payer), delegationShares(g.t), options(g.t, payer)...)
	if err != nil {
		return err
	}

	g.tx = utx
	return nil
}

func (g *generator) AddDelegatorTx(*platform.AddDelegatorTx) error {
	payer := g.r.drawPayer(g.t, g.r.actors)
	vdr := &platform.Validator{
		NodeID: refID(g, "target", g.r.validators.keys(active), &g.r.seenNodeIDs.all),
		End:    endTime(stakeDuration(g.t, minStakeDuration)),
		Wght:   stakeWeight(g.t, g.r.minDelegatorStake),
	}
	utx, err := g.r.builder(payer).NewAddDelegatorTx(vdr, owner(payer), options(g.t, payer)...)
	if err != nil {
		return err
	}

	g.tx = utx
	return nil
}

// TransformSubnetTx burns nothing: its initial supply is its maximum, so the
// payer needs no units of the drawn asset.
func (g *generator) TransformSubnetTx(*platform.TransformSubnetTx) error {
	subnetID := refID(g, "subnet", g.r.permissionedSubnets(), &g.r.seenIDs.all)
	assetID := drawID(g.t, g.r.ev, "asset", nil, &g.r.seenIDs.all)
	supply := rapid.Uint64Range(1, units.MegaAvax).Draw(g.t, "supply")
	minStake := rapid.Uint64Range(1, supply).Draw(g.t, "minValidatorStake")
	payer := g.r.drawPayer(g.t, g.r.actors)
	utx, err := g.r.builder(payer).NewTransformSubnetTx(
		subnetID, assetID,
		supply, supply,
		reward.PercentDenominator/10, reward.PercentDenominator/10,
		minStake, supply,
		minStakeDuration, stakeDuration(g.t, minStakeDuration),
		delegationShares(g.t), minStake,
		5, reward.PercentDenominator*4/5,
		options(g.t, payer)...,
	)
	if err != nil {
		return err
	}

	g.tx = utx
	return nil
}

// AdvanceTimeTx proposes a time up to a minute either side of now.
func (g *generator) AdvanceTimeTx(*platform.AdvanceTimeTx) error {
	offset := time.Duration(rapid.IntRange(-60, 60).Draw(g.t, "advance")) * time.Second
	g.tx = &platform.AdvanceTimeTx{Time: uint64(time.Now().Add(offset).Unix())}
	return nil
}

// RewardValidatorTx rewards a staker that the model knows, by the tx that
// added it.
func (g *generator) RewardValidatorTx(*platform.RewardValidatorTx) error {
	stakers := slices.SortedFunc(maps.Keys(g.r.rewardIndex), ids.ID.Compare)
	g.tx = &platform.RewardValidatorTx{TxID: refID(g, "staker", stakers, &g.r.seenIDs.all)}
	return nil
}

func (g *generator) RewardAutoRenewedValidatorTx(*platform.RewardAutoRenewedValidatorTx) error {
	g.tx = &platform.RewardAutoRenewedValidatorTx{
		TxID:      refID(g, "validator", g.r.autoRenew.keys(active), &g.r.seenIDs.all),
		Timestamp: uint64(time.Now().Unix()),
	}
	return nil
}

// TransferSubnetOwnershipTx draws from every subnet, converted ones too. The
// P-Chain allows it there: a converted subnet's owner has no powers left.
func (g *generator) TransferSubnetOwnershipTx(*platform.TransferSubnetOwnershipTx) error {
	subnetID := refID(g, "subnet", g.r.subnets.keys(active), &g.r.seenIDs.all)
	subnetOwner := g.r.multisig(g.t)
	payer := g.r.drawPayer(g.t, g.r.actors)
	utx, err := g.r.builder(payer).NewTransferSubnetOwnershipTx(subnetID, subnetOwner, options(g.t, payer)...)
	if err != nil {
		return err
	}

	g.tx = utx
	return nil
}

// ConvertSubnetToL1Tx converts a subnet to an L1 with one or two validators.
// No registry holds chains, so the manager chain and its address are any seen
// ID or a fresh one.
func (g *generator) ConvertSubnetToL1Tx(*platform.ConvertSubnetToL1Tx) error {
	subnetID := refID(g, "subnet", g.r.permissionedSubnets(), &g.r.seenIDs.all)
	validators := make([]*platform.ConvertSubnetToL1Validator, rapid.IntRange(1, 2).Draw(g.t, "validators"))
	for i := range validators {
		nodeID := g.newNodeID()
		validators[i] = &platform.ConvertSubnetToL1Validator{
			NodeID:                nodeID[:],
			Weight:                rapid.Uint64Range(1, 100).Draw(g.t, "weight"),
			Balance:               l1Balance(g.t),
			Signer:                *proofOfPossession(g.t),
			RemainingBalanceOwner: g.r.pChainOwner(g.t),
			DeactivationOwner:     g.r.pChainOwner(g.t),
		}
	}
	utils.Sort(validators)

	chainID := drawID(g.t, g.r.ev, "chain", nil, &g.r.seenIDs.all)
	address := drawID(g.t, g.r.ev, "address", nil, &g.r.seenShortIDs.all)
	payer := g.r.drawPayer(g.t, g.r.actors)
	utx, err := g.r.builder(payer).NewConvertSubnetToL1Tx(subnetID, chainID, address[:], validators, options(g.t, payer)...)
	if err != nil {
		return err
	}

	g.tx = utx
	return nil
}

func (g *generator) IncreaseL1ValidatorBalanceTx(*platform.IncreaseL1ValidatorBalanceTx) error {
	validationID := refID(g, "validation", g.r.l1.keys(active), &g.r.seenIDs.all)
	amount := rapid.Uint64Range(1, units.MilliAvax).Draw(g.t, "amount")
	payer := g.r.drawPayer(g.t, g.r.actors)
	utx, err := g.r.builder(payer).NewIncreaseL1ValidatorBalanceTx(validationID, amount, options(g.t, payer)...)
	if err != nil {
		return err
	}

	g.tx = utx
	return nil
}

func (g *generator) DisableL1ValidatorTx(*platform.DisableL1ValidatorTx) error {
	validationID := refID(g, "validation", g.r.l1.keys(active), &g.r.seenIDs.all)
	payer := g.r.drawPayer(g.t, g.r.actors)
	utx, err := g.r.builder(payer).NewDisableL1ValidatorTx(validationID, options(g.t, payer)...)
	if err != nil {
		return err
	}

	g.tx = utx
	return nil
}

// SetAutoRenewedValidatorConfigTx sets a period of 0 one draw in 4: the
// validator then leaves at the end of its cycle, and a reward that commits
// pays it out (vms/platformvm/txs/executor/proposal_tx_executor.go
// RewardAutoRenewedValidatorTx). stakeDuration alone draws 0 one time in 64.
func (g *generator) SetAutoRenewedValidatorConfigTx(*platform.SetAutoRenewedValidatorConfigTx) error {
	txID := refID(g, "validator", g.r.autoRenew.keys(active), &g.r.seenIDs.all)
	payer := g.r.drawPayer(g.t, g.r.actors)
	var period time.Duration
	if uniform(4).Draw(g.t, "exit") != 0 {
		period = stakeDuration(g.t, heliconMinStakeDuration)
	}
	utx, err := g.r.builder(payer).NewSetAutoRenewedValidatorConfigTx(txID, delegationShares(g.t), period, options(g.t, payer)...)
	if err != nil {
		return err
	}

	g.tx = utx
	return nil
}

// CreateChainTx creates a chain with a fresh VM ID. No node tracks the
// subnet, so no VM runs, and the tx only registers the chain.
func (g *generator) CreateChainTx(*platform.CreateChainTx) error {
	subnetID := refID(g, "subnet", g.r.permissionedSubnets(), &g.r.seenIDs.all)
	genesis := rapid.SliceOfN(rapid.Byte(), 1, 64).Draw(g.t, "genesis")
	payer := g.r.drawPayer(g.t, g.r.actors)
	utx, err := g.r.builder(payer).NewCreateChainTx(subnetID, genesis, drawT[ids.ID](g.t, "vm"), nil, "fuzz", options(g.t, payer)...)
	if err != nil {
		return err
	}

	g.tx = utx
	return nil
}

// AddSubnetValidatorTx adds a primary network validator to a permissioned
// subnet. Its active kind is the primary network validators not on the
// subnet yet. It stakes no AVAX: the subnet owner assigns its weight.
func (g *generator) AddSubnetValidatorTx(*platform.AddSubnetValidatorTx) error {
	subnetID := refID(g, "subnet", g.r.permissionedSubnets(), &g.r.seenIDs.all)
	var candidates []ids.NodeID
	for _, v := range g.r.primaryValidators {
		if !slices.Contains(g.r.subnetValidators[subnetID], v.NodeID) {
			candidates = append(candidates, v.NodeID)
		}
	}

	nodeID := refID(g, "nodeID", candidates, &g.r.seenNodeIDs.all)
	vdr := &platform.SubnetValidator{
		Validator: platform.Validator{
			NodeID: nodeID,
			End:    g.stakerEnd(nodeID),
			Wght:   rapid.Uint64Range(1, 100).Draw(g.t, "weight"),
		},
		Subnet: subnetID,
	}
	payer := g.r.drawPayer(g.t, g.r.actors)
	utx, err := g.r.builder(payer).NewAddSubnetValidatorTx(vdr, options(g.t, payer)...)
	if err != nil {
		return err
	}

	g.tx = utx
	return nil
}

func (g *generator) RemoveSubnetValidatorTx(*platform.RemoveSubnetValidatorTx) error {
	var withValidators []ids.ID
	for _, id := range g.r.permissionedSubnets() {
		if len(g.r.subnetValidators[id]) > 0 {
			withValidators = append(withValidators, id)
		}
	}

	subnetID := refID(g, "subnet", withValidators, &g.r.seenIDs.all)
	nodeID := refID(g, "nodeID", g.r.subnetValidators[subnetID], &g.r.seenNodeIDs.all)
	payer := g.r.drawPayer(g.t, g.r.actors)
	utx, err := g.r.builder(payer).NewRemoveSubnetValidatorTx(nodeID, subnetID, options(g.t, payer)...)
	if err != nil {
		return err
	}

	g.tx = utx
	return nil
}

// AddPermissionlessDelegatorTx delegates to a primary network validator: a
// genesis node, or one that a committed tx added. Half the delegations stake
// as much as a validator, so that some exceed the validator's room under the
// maximum stake (maxValidatorStake).
func (g *generator) AddPermissionlessDelegatorTx(*platform.AddPermissionlessDelegatorTx) error {
	targets := make([]ids.NodeID, len(g.r.primaryValidators))
	for i, v := range g.r.primaryValidators {
		targets[i] = v.NodeID
	}

	target := refID(g, "target", targets, &g.r.seenNodeIDs.all)
	minStake := rapid.SampledFrom([]uint64{g.r.minDelegatorStake, g.r.minValidatorStake}).Draw(g.t, "delegationScale")
	vdr := &platform.SubnetValidator{
		Validator: platform.Validator{
			NodeID: target,
			End:    g.stakerEnd(target),
			Wght:   stakeWeight(g.t, minStake),
		},
		Subnet: constants.PrimaryNetworkID,
	}
	payer := g.r.drawPayer(g.t, g.r.actors)
	utx, err := g.r.builder(payer).NewAddPermissionlessDelegatorTx(vdr, g.r.avaxAssetID, owner(payer), options(g.t, payer)...)
	if err != nil {
		return err
	}

	g.tx = utx
	return nil
}

// stakerEnd returns when a staker on nodeID that lasts a drawn duration ends.
// A staker cannot outlast its node's primary validation, so the end is capped
// by nodeID's end time, if the last check listed it.
func (g *generator) stakerEnd(nodeID ids.NodeID) uint64 {
	end := endTime(stakeDuration(g.t, minStakeDuration))
	for _, v := range g.r.primaryValidators {
		if v.NodeID == nodeID {
			return min(end, v.EndTime)
		}
	}

	return end
}

// The Warp-signed types need the L1 validators' BLS keys (Stateful Fuzzing.md
// 2.3).

func (*generator) RegisterL1ValidatorTx(*platform.RegisterL1ValidatorTx) error {
	return errUnsupported
}

func (*generator) SetL1ValidatorWeightTx(*platform.SetL1ValidatorWeightTx) error {
	return errUnsupported
}

// permissionedSubnets returns the subnets that are still permissioned: created
// and not converted to an L1. Only these take CreateChainTx and subnet
// validators.
func (r *runner) permissionedSubnets() []ids.ID {
	var open []ids.ID
	for _, id := range r.subnets.keys(active) {
		if !r.converted.Contains(id) {
			open = append(open, id)
		}
	}

	return open
}

// multisig returns an owner over a non-empty subset of actors, with a
// threshold no larger than the subset.
func (r *runner) multisig(t *rapid.T) *secp256k1fx.OutputOwners {
	addrs := rapid.Permutation(r.actors).Draw(t, "owners")
	addrs = addrs[:rapid.IntRange(1, len(addrs)).Draw(t, "ownerCount")]
	utils.Sort(addrs)
	for _, addr := range addrs {
		r.logActor("owner", addr)
	}

	return &secp256k1fx.OutputOwners{
		Threshold: uint32(rapid.IntRange(1, len(addrs)).Draw(t, "threshold")),
		Addrs:     addrs,
	}
}

func (r *runner) pChainOwner(t *rapid.T) message.PChainOwner {
	o := r.multisig(t)
	return message.PChainOwner{Threshold: o.Threshold, Addresses: o.Addrs}
}

// proofOfPossession returns the proof of possession of a new BLS key.
func proofOfPossession(t *rapid.T) *signer.ProofOfPossession {
	sk, err := localsigner.New()
	require.NoError(t, err)

	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)

	return pop
}

// stakeDuration returns threshold plus an offset of 0 to 63 s whose bit length
// is drawn from 0 to 6, leaning small (Stateful Fuzzing.md section 2), so
// durations span orders of magnitude: many stakers end within a run, and some
// outlive it. Pass the network's minimum for the staker. About one draw in 25
// is a boundary below it instead: threshold minus 1 s, or a period of 0.
func stakeDuration(t *rapid.T, threshold time.Duration) time.Duration {
	const maxBits = 6

	// rapid leans toward both ends of a range, the low end more, so the
	// boundary sits at the top, where about 4% of draws land.
	if rapid.IntRange(0, 31).Draw(t, "stakeBoundary") == 31 {
		if rapid.Bool().Draw(t, "stakePeriodZero") {
			return 0
		}
		return threshold - time.Second
	}

	var offset int64
	if bits := rapid.IntRange(0, maxBits).Draw(t, "stakeOffsetBits"); bits > 0 {
		offset = rapid.Int64Range(1<<(bits-1), 1<<bits-1).Draw(t, "stakeOffset")
	}

	return threshold + time.Duration(offset)*time.Second
}

// endTime returns the Unix second at which a staker that lasts d from now
// ends. It rounds up: the chain checks a staker's duration against its time in
// nanoseconds, so rounding down would make every draw of the minimum too
// short. So the boundary draw of the minimum minus 1 s ends exactly the
// minimum after the current whole second. A node accepts that staker only
// while its chain time is that whole second, which it is while a staker
// change at that second waits for its block (state/chain_time_helpers.go
// NextBlockTime).
func endTime(d time.Duration) uint64 {
	return uint64(time.Now().Add(d + time.Second - 1).Unix())
}

// stakeWeight returns one to four times minStake.
func stakeWeight(t *rapid.T, minStake uint64) uint64 {
	return minStake * rapid.Uint64Range(1, 4).Draw(t, "stakeMultiple")
}

// delegationShares returns a delegation fee from 2%, the local network's
// minimum, to 100%.
func delegationShares(t *rapid.T) uint32 {
	const minShares = reward.PercentDenominator / 50
	return rapid.Uint32Range(minShares, reward.PercentDenominator).Draw(t, "delegationShares")
}

// l1Balance returns zero (an inactive validator), a balance that runs out in
// seconds at the local network's minimum fee of 1 nAVAX per second, or one
// that lasts the run.
func l1Balance(t *rapid.T) uint64 {
	return rapid.OneOf(
		rapid.Just[uint64](0),
		rapid.Uint64Range(10, 109),
		rapid.Just(units.MilliAvax),
	).Draw(t, "l1Balance")
}
