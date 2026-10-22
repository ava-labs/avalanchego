// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	pbuilder "github.com/ava-labs/avalanchego/wallet/chain/p/builder"
	psigner "github.com/ava-labs/avalanchego/wallet/chain/p/signer"
	pwallet "github.com/ava-labs/avalanchego/wallet/chain/p/wallet"
	xwallet "github.com/ava-labs/avalanchego/wallet/chain/x"
	xbuilder "github.com/ava-labs/avalanchego/wallet/chain/x/builder"
	xsigner "github.com/ava-labs/avalanchego/wallet/chain/x/signer"
)

const (
	// decideTimeout bounds how long a tx may stay undecided. A healthy local
	// network decides a tx in about a second, so a longer wait is a liveness
	// failure.
	decideTimeout = 30 * time.Second
	pollInterval  = 50 * time.Millisecond
)

// An outcome is how an op ended. Runs count outcomes per [Kind].
type outcome uint8

const (
	skipped   outcome = iota // nothing to act on, or the wallet could not build the tx
	refused                  // the node refused the tx at issuance
	dropped                  // the node took the tx but did not commit it
	admitted                 // the node took an adversarial tx; later checks decide if that was safe
	committed                // every node committed the tx
)

var outcomeNames = [...]string{
	skipped:   "skipped",
	refused:   "refused",
	dropped:   "dropped",
	admitted:  "admitted",
	committed: "committed",
}

func (o outcome) String() string {
	return outcomeNames[o]
}

// MarshalText implements [encoding.TextMarshaler], so logged counts name
// outcomes.
func (o outcome) MarshalText() ([]byte, error) {
	return []byte(o.String()), nil
}

// Runner runs ops against one network and checks invariants after each op.
//
// The runner owns every P-Chain UTXO of its actors. Its model of them is the
// wallet's own UTXO set, so the check compares the wallet's bookkeeping with
// each node's. Nothing else may pay to or spend from the actors.
type Runner struct {
	tc      tests.TestContext
	log     logging.Logger
	clients []*platformvm.Client // one per node
	// gossipers holds, for each node, a Byzantine peer connected to it.
	gossipers []*byzantinePeer

	actors      []ids.ShortID
	utxos       common.UTXOs
	backend     pwallet.Backend
	builder     pbuilder.Builder
	signer      psigner.Signer
	avaxAssetID ids.ID
	xChainID    ids.ID
	xWallet     xwallet.Wallet
	xClient     *avm.Client
	// inFlight is the AVAX the fuzzer exported from the X-Chain to the P-Chain
	// and has not yet imported. It has left the X-Chain but not entered the
	// P-Chain, so the value oracle counts it to keep a cross-chain move from
	// looking like minting.
	inFlight uint64

	minValidatorStake uint64
	minDelegatorStake uint64

	subnets   registry[ids.ID, *subnet]
	l1        registry[ids.ID, *l1Validator]
	autoRenew registry[ids.ID, autoRenewed]
	// validators is the set of primary network validators the fuzzer added,
	// with their lifecycle. An op references a node ID; syncModel ends it when
	// GetCurrentValidators drops it.
	validators registry[ids.NodeID, struct{}]
	accepted   []*platform.Tx
	// forbidden lists txs that no node may ever commit.
	forbidden []ids.ID

	// returns holds, by UTXO ID, the UTXOs that stakers get back when they end.
	returns map[ids.ID]*avax.UTXO
	// rewardIndex maps a staker tx to the first output index its rewards use.
	rewardIndex map[ids.ID]uint32
	// refunds bounds, by UTXO ID, what each committed disable may refund.
	refunds map[ids.ID]uint64

	height uint64 // highest height whose blocks the nodes agree on
	value  uint64 // the actors' total value at the last check

	stats map[Kind]map[outcome]int
	// reason is the check that refused or dropped the current op's tx, and
	// the path that the tx came in on. Run counts it in reasons.
	reason  string
	reasons map[Kind]map[string]int
}

type subnet struct {
	id        ids.ID
	converted bool
	// validators holds the node IDs the runner added as subnet validators and
	// has not removed. Permissioned subnet validators stake no AVAX, so they
	// need no return bookkeeping.
	validators []ids.NodeID
}

type l1Validator struct {
	validationID ids.ID
	balance      uint64 // as of the last check
}

// autoRenewed tracks an auto-renewed primary network validator, by the ID of
// the tx that added it (the handle that SetAutoRenewedValidatorConfigTx uses)
// and its node ID.
type autoRenewed struct {
	txID   ids.ID
	nodeID ids.NodeID
}

// makeXWallet builds an X-Chain wallet for addrs, reading their current
// X-Chain UTXOs from uri. kc signs its txs.
func makeXWallet(ctx context.Context, tc tests.TestContext, uri string, addrs set.Set[ids.ShortID], kc *secp256k1fx.Keychain) xwallet.Wallet {
	state, err := primary.FetchState(ctx, uri, addrs)
	require.NoError(tc, err)

	backend := xwallet.NewBackend(state.XCTX, common.NewChainUTXOs(state.XCTX.BlockchainID, state.UTXOs))
	return xwallet.NewWallet(xbuilder.New(addrs, state.XCTX, backend), xsigner.New(kc, backend), state.XClient, backend)
}

// NewRunner creates actors and funds them from funder. uris lists every node
// of the network, and gossipers holds a Byzantine peer connected to each node,
// in the same order.
func NewRunner(tc tests.TestContext, uris []string, gossipers []*byzantinePeer, funder *secp256k1.PrivateKey, actors int) *Runner {
	// Enough that locked stake rarely leaves an actor unable to pay.
	const funds = units.MegaAvax

	ctx := tc.DefaultContext()
	keys := make([]*secp256k1.PrivateKey, actors)
	addrs := make([]ids.ShortID, actors)
	for i := range keys {
		key, err := secp256k1.NewPrivateKey()
		require.NoError(tc, err)

		keys[i] = key
		addrs[i] = key.Address()
	}

	funderWallet, err := primary.MakePWallet(ctx, uris[0], secp256k1fx.NewKeychain(funder), primary.WalletConfig{})
	require.NoError(tc, err)

	avaxAssetID := funderWallet.Builder().Context().AVAXAssetID
	outs := make([]*avax.TransferableOutput, actors)
	for i, addr := range addrs {
		outs[i] = &avax.TransferableOutput{
			Asset: avax.Asset{ID: avaxAssetID},
			Out:   &secp256k1fx.TransferOutput{Amt: funds, OutputOwners: *owner(addr)},
		}
	}
	_, err = funderWallet.IssueBaseTx(outs, common.WithContext(ctx))
	require.NoError(tc, err)

	client, context, utxos, err := primary.FetchPState(ctx, uris[0], set.Of(addrs...))
	require.NoError(tc, err)

	minValidatorStake, minDelegatorStake, err := client.GetMinStake(ctx, constants.PrimaryNetworkID)
	require.NoError(tc, err)

	height, err := client.GetHeight(ctx)
	require.NoError(tc, err)

	clients := make([]*platformvm.Client, len(uris))
	for i, uri := range uris {
		clients[i] = platformvm.NewClient(uri)
	}

	// Fund the actors on the X-Chain too, from the funder, so they can export
	// value X->P. The funder is a genesis key with X-Chain funds.
	funderXWallet := makeXWallet(ctx, tc, uris[0], set.Of(funder.Address()), secp256k1fx.NewKeychain(funder))
	xOuts := make([]*avax.TransferableOutput, actors)
	for i, addr := range addrs {
		xOuts[i] = &avax.TransferableOutput{
			Asset: avax.Asset{ID: avaxAssetID},
			Out:   &secp256k1fx.TransferOutput{Amt: funds, OutputOwners: *owner(addr)},
		}
	}
	_, err = funderXWallet.IssueBaseTx(xOuts, common.WithContext(ctx))
	require.NoError(tc, err)

	// The actors' X-Chain wallet, built after funding so it sees the new UTXOs.
	xWallet := makeXWallet(ctx, tc, uris[0], set.Of(addrs...), secp256k1fx.NewKeychain(keys...))
	xClient := avm.NewClient(uris[0], "X")

	// The X-Chain is the export target. Its blockchain ID comes from genesis,
	// so the runner reads it rather than assuming a value.
	chains, err := clients[0].GetBlockchains(ctx)
	require.NoError(tc, err)
	var xChainID ids.ID
	for _, b := range chains {
		if b.VMID == constants.AVMID {
			xChainID = b.ID
		}
	}
	require.NotEqual(tc, ids.Empty, xChainID, "no X-Chain in GetBlockchains")

	backend := pwallet.NewBackend(common.NewChainUTXOs(constants.PlatformChainID, utxos), map[ids.ID]fx.Owner{})
	r := &Runner{
		tc:                tc,
		log:               tc.Log(),
		clients:           clients,
		gossipers:         gossipers,
		actors:            addrs,
		utxos:             utxos,
		backend:           backend,
		builder:           pbuilder.New(set.Of(addrs...), context, backend),
		signer:            psigner.New(secp256k1fx.NewKeychain(keys...), backend),
		avaxAssetID:       avaxAssetID,
		xChainID:          xChainID,
		xWallet:           xWallet,
		xClient:           xClient,
		minValidatorStake: minValidatorStake,
		minDelegatorStake: minDelegatorStake,
		returns:           make(map[ids.ID]*avax.UTXO),
		rewardIndex:       make(map[ids.ID]uint32),
		refunds:           make(map[ids.ID]uint64),
		height:            height,
		value:             math.MaxUint64, // the first check sets the baseline
		stats:             make(map[Kind]map[outcome]int),
		reasons:           make(map[Kind]map[string]int),
	}
	r.check()

	return r
}

// Run runs ops in order and checks invariants after each one. It fails the
// test on the first violation. It logs how often each kind of op ended in
// each outcome, and which check refused or dropped its txs on which path,
// even after a failure: a kind that never commits explores nothing, and a
// check that never decides is never tested.
func (r *Runner) Run(ops []Op) {
	defer func() {
		r.log.Info("op outcomes", zap.Any("counts", r.stats), zap.Any("reasons", r.reasons))
	}()

	for i, op := range ops {
		r.tc.By(fmt.Sprintf("step %d: %s %v", i, op.Kind, op.Args))
		result := r.run(op)
		r.log.Info("ran op",
			zap.Int("step", i),
			zap.Stringer("kind", op.Kind),
			zap.Stringer("outcome", result),
		)
		if r.stats[op.Kind] == nil {
			r.stats[op.Kind] = make(map[outcome]int)
		}
		r.stats[op.Kind][result]++
		if r.reason != "" {
			if r.reasons[op.Kind] == nil {
				r.reasons[op.Kind] = make(map[string]int)
			}
			r.reasons[op.Kind][r.reason]++
			r.reason = ""
		}
		r.check()
	}
}

func (r *Runner) run(op Op) outcome {
	a := op.Args
	switch op.Kind {
	case Transfer:
		return r.transfer(a)
	case CreateSubnet:
		return r.createSubnet(a)
	case TransferSubnetOwnership:
		return r.transferSubnetOwnership(a)
	case AddValidator:
		return r.addValidator(a)
	case AddDelegator:
		return r.addDelegator(a)
	case ConvertSubnetToL1:
		return r.convertSubnetToL1(a)
	case IncreaseL1Balance:
		return r.increaseL1Balance(a)
	case DisableL1Validator:
		return r.disableL1Validator(a)
	case AddAutoRenewedValidator:
		return r.addAutoRenewedValidator(a)
	case SetAutoRenewedValidatorConfig:
		return r.setAutoRenewedValidatorConfig(a)
	case CreateChain:
		return r.createChain(a)
	case AddSubnetValidator:
		return r.addSubnetValidator(a)
	case RemoveSubnetValidator:
		return r.removeSubnetValidator(a)
	case Export:
		return r.export(a)
	case ReAddValidator:
		return r.reAddValidator(a)
	case ExportToP:
		return r.exportToP(a)
	case ImportToP:
		return r.importToP(a)
	case Replay:
		return r.replay(a)
	case DoubleSpend:
		return r.doubleSpend(a)
	case Inflate:
		return r.inflate(a)
	default:
		r.fatalf("unknown op kind %q", op.Kind)
		return skipped
	}
}

func (r *Runner) transfer(a [4]uint64) outcome {
	from := r.actor(a[0])
	utx, err := r.builder.NewBaseTx(r.payment(r.actor(a[1]), a[2]), r.spend(from)...)
	if err != nil {
		return r.skip(err)
	}

	return r.commit(r.sign(utx), a[3])
}

func (r *Runner) createSubnet(a [4]uint64) outcome {
	owner := r.multisig(a[0], a[1])
	utx, err := r.builder.NewCreateSubnetTx(owner, r.options(r.actor(a[2]))...)
	if err != nil {
		return r.skip(err)
	}

	tx := r.sign(utx)
	outcome := r.commit(tx, a[3])
	if outcome == committed {
		r.requireSubnetOwner(tx.ID(), owner)
		r.subnets.put(tx.ID(), &subnet{id: tx.ID()})
	}

	return outcome
}

// requireSubnetOwner fails the test unless every node reports the subnet as
// permissioned with the given owner. It is the guarantee that
// [platform.CreateSubnetTx] and [platform.TransferSubnetOwnershipTx] make.
func (r *Runner) requireSubnetOwner(id ids.ID, owner *secp256k1fx.OutputOwners) {
	for i, c := range r.clients {
		got, err := c.GetSubnet(r.tc.DefaultContext(), id)
		require.NoError(r.tc, err)
		require.True(r.tc, got.IsPermissioned, "node %d reports subnet %s as not permissioned", i, id)
		require.Equal(r.tc, owner.Threshold, got.Threshold, "node %d reports wrong threshold for subnet %s", i, id)
		require.Equal(r.tc, owner.Addrs, got.ControlKeys, "node %d reports wrong owner for subnet %s", i, id)
	}
}

// transferSubnetOwnership runs on converted subnets too. The P-Chain allows it
// there: a converted subnet's owner has no powers left.
func (r *Runner) transferSubnetOwnership(a [4]uint64) outcome {
	_, s, ok := r.subnets.draw(active, a[0])
	if !ok {
		return skipped
	}

	owner := r.multisig(a[1], a[2])
	utx, err := r.builder.NewTransferSubnetOwnershipTx(s.id, owner, r.options(r.actor(a[2]))...)
	if err != nil {
		return r.skip(err)
	}

	outcome := r.commit(r.sign(utx), a[3])
	if outcome == committed {
		// A converted subnet is no longer permissioned, and its control keys
		// have no powers, so the owner is not observable through GetSubnet.
		if !s.converted {
			r.requireSubnetOwner(s.id, owner)
		}
	}

	return outcome
}

// addValidator adds a primary network validator for a node that does not
// exist. It never meets the uptime requirement, so it ends without a reward.
func (r *Runner) addValidator(a [4]uint64) outcome {
	return r.issueValidator(nodeID(newRand(a[0])), a)
}

// reAddValidator adds a primary network validator that reuses the node ID of
// one the fuzzer already added, biased to a lifecycle state by the args. The
// verdict is not asserted: an active node ID is a duplicate the network must
// reject; an ended one is free, so the re-add may commit. The postcondition
// (on commit) and the invariants judge either outcome.
func (r *Runner) reAddValidator(a [4]uint64) outcome {
	state := active
	if a[1]%2 == 0 {
		state = ended
	}

	id, _, ok := r.validators.draw(state, a[2])
	if !ok {
		return skipped
	}

	return r.issueValidator(id, a)
}

// issueValidator builds and issues an AddPermissionlessValidatorTx for id with
// a fresh key, and on commit records the validator. It asserts no verdict: id
// may already be a validator. The commit postcondition holds whenever the tx
// does commit.
func (r *Runner) issueValidator(id ids.NodeID, a [4]uint64) outcome {
	// 2%, the local network's minimum delegation fee.
	const minShares = reward.PercentDenominator / 50

	pop, err := signer.NewProofOfPossession(blsKey(newRand(a[0])))
	require.NoError(r.tc, err)

	from := r.actor(a[3])
	weight := r.minValidatorStake * (1 + a[1]%4)
	vdr := &platform.SubnetValidator{
		Validator: platform.Validator{
			NodeID: id,
			End:    uint64(time.Now().Add(stakeDuration(a[2])).Unix()),
			Wght:   weight,
		},
		Subnet: constants.PrimaryNetworkID,
	}
	shares := minShares + uint32(a[3]%(reward.PercentDenominator-minShares+1))
	utx, err := r.builder.NewAddPermissionlessValidatorTx(vdr, pop, r.avaxAssetID, owner(from), owner(from), shares, r.spend(from)...)
	if err != nil {
		return r.skip(err)
	}

	tx := r.sign(utx)
	outcome := r.commit(tx, a[3]>>32)
	if outcome == committed {
		// The tx guarantees the validator is now in the set. Verify that
		// before trusting it to return its stake: a committed tx that did not
		// register the staker would otherwise be masked, because the model
		// counts the stake as returnable value.
		r.requireValidator(id, weight)
		r.expectStakeReturn(tx.ID(), utx.Outs, utx.StakeOuts)
		r.validators.put(id, struct{}{})
	}

	return outcome
}

// requireValidator fails the test unless every node reports id as a current
// primary network validator with the given weight and end time. It is the
// guarantee that [platform.AddPermissionlessValidatorTx] makes: once the tx
// commits, the validator is in the set.
func (r *Runner) requireValidator(id ids.NodeID, weight uint64) {
	for i, c := range r.clients {
		vdrs, err := c.GetCurrentValidators(r.tc.DefaultContext(), constants.PrimaryNetworkID, []ids.NodeID{id})
		require.NoError(r.tc, err)
		require.Len(r.tc, vdrs, 1, "node %d does not report added validator %s", i, id)
		require.Equal(r.tc, weight, vdrs[0].Weight, "node %d reports weight %d for validator %s, want %d", i, vdrs[0].Weight, id, weight)
	}
}

// requireValidatorPresent fails the test unless every node reports id as a
// current primary network validator. Unlike [Runner.requireValidator] it does
// not check the weight, which an auto-renewed validator changes as it restakes
// rewards.
func (r *Runner) requireValidatorPresent(id ids.NodeID) {
	for i, c := range r.clients {
		vdrs, err := c.GetCurrentValidators(r.tc.DefaultContext(), constants.PrimaryNetworkID, []ids.NodeID{id})
		require.NoError(r.tc, err)
		require.Len(r.tc, vdrs, 1, "node %d does not report validator %s", i, id)
	}
}

// addDelegator delegates to any current primary network validator: a
// genesis node, or one that addValidator added.
func (r *Runner) addDelegator(a [4]uint64) outcome {
	vdrs, err := r.clients[0].GetCurrentValidators(r.tc.DefaultContext(), constants.PrimaryNetworkID, nil)
	require.NoError(r.tc, err)
	if len(vdrs) == 0 {
		return skipped
	}

	// The API does not promise an order, and a replay must pick the same one.
	slices.SortFunc(vdrs, func(x, y platformvm.ClientPermissionlessValidator) int {
		return x.NodeID.Compare(y.NodeID)
	})
	target := vdrs[a[0]%uint64(len(vdrs))]
	from := r.actor(a[3])
	vdr := &platform.SubnetValidator{
		Validator: platform.Validator{
			NodeID: target.NodeID,
			End:    min(target.EndTime, uint64(time.Now().Add(stakeDuration(a[2])).Unix())),
			Wght:   r.minDelegatorStake * (1 + a[1]%4),
		},
		Subnet: constants.PrimaryNetworkID,
	}
	utx, err := r.builder.NewAddPermissionlessDelegatorTx(vdr, r.avaxAssetID, owner(from), r.spend(from)...)
	if err != nil {
		return r.skip(err)
	}

	tx := r.sign(utx)
	outcome := r.commit(tx, a[3]>>32)
	if outcome == committed {
		// The tx guarantees the delegation is counted on the target.
		r.requireDelegation(target.NodeID, vdr.Wght)
		r.expectStakeReturn(tx.ID(), utx.Outs, utx.StakeOuts)
	}

	return outcome
}

// requireDelegation fails the test unless every node reports the target
// validator with delegation weight at least weight. It is the guarantee that
// [platform.AddPermissionlessDelegatorTx] makes: the delegation is counted.
// Stakers end over time, so the check is a lower bound, not an equality.
func (r *Runner) requireDelegation(target ids.NodeID, weight uint64) {
	for i, c := range r.clients {
		vdrs, err := c.GetCurrentValidators(r.tc.DefaultContext(), constants.PrimaryNetworkID, []ids.NodeID{target})
		require.NoError(r.tc, err)
		require.Len(r.tc, vdrs, 1, "node %d does not report delegation target %s", i, target)
		require.NotNil(r.tc, vdrs[0].DelegatorWeight, "node %d reports no delegation on %s", i, target)
		require.GreaterOrEqual(r.tc, *vdrs[0].DelegatorWeight, weight, "node %d reports delegation weight %d on %s, want at least %d", i, *vdrs[0].DelegatorWeight, target, weight)
	}
}

func (r *Runner) convertSubnetToL1(a [4]uint64) outcome {
	open := r.permissionedSubnets()
	if len(open) == 0 {
		return skipped
	}

	s := open[a[0]%uint64(len(open))]
	rng := newRand(a[1])
	validators := make([]*platform.ConvertSubnetToL1Validator, 1+a[2]%2)
	for i := range validators {
		pop, err := signer.NewProofOfPossession(blsKey(rng))
		require.NoError(r.tc, err)

		id := nodeID(rng)
		validators[i] = &platform.ConvertSubnetToL1Validator{
			NodeID:                id[:],
			Weight:                1 + rng.Uint64()%100,
			Balance:               l1Balance(rng.Uint64()),
			Signer:                *pop,
			RemainingBalanceOwner: r.pChainOwner(rng.Uint64(), rng.Uint64()),
			DeactivationOwner:     r.pChainOwner(rng.Uint64(), rng.Uint64()),
		}
	}
	utils.Sort(validators)

	var chainID ids.ID
	fill(rng, chainID[:])
	address := make([]byte, 20)
	fill(rng, address)
	utx, err := r.builder.NewConvertSubnetToL1Tx(s.id, chainID, address, validators, r.options(r.actor(a[3]))...)
	if err != nil {
		return r.skip(err)
	}

	tx := r.sign(utx)
	outcome := r.commit(tx, a[3]>>32)
	if outcome == committed {
		s.converted = true
		for i, vdr := range utx.Validators {
			vID := s.id.Append(uint32(i))
			r.l1.put(vID, &l1Validator{validationID: vID, balance: vdr.Balance})
		}
	}

	return outcome
}

func (r *Runner) increaseL1Balance(a [4]uint64) outcome {
	_, v, ok := r.l1.draw(active, a[0])
	if !ok {
		return skipped
	}

	utx, err := r.builder.NewIncreaseL1ValidatorBalanceTx(v.validationID, 1+a[1]%units.MilliAvax, r.spend(r.actor(a[2]))...)
	if err != nil {
		return r.skip(err)
	}

	return r.commit(r.sign(utx), a[3])
}

func (r *Runner) disableL1Validator(a [4]uint64) outcome {
	vID, v, ok := r.l1.draw(active, a[0])
	if !ok {
		return skipped
	}

	utx, err := r.builder.NewDisableL1ValidatorTx(v.validationID, r.options(r.actor(a[1]))...)
	if err != nil {
		return r.skip(err)
	}

	tx := r.sign(utx)
	outcome := r.commit(tx, a[2])
	if outcome == committed {
		// The tx guarantees the validator keeps no fee balance: it is
		// refunded. A validator that kept its balance here would mint AVAX.
		r.requireL1BalanceZero(v.validationID)
		r.l1.transition(vID, removed)
		// Balances only fall between checks, so the last one bounds the refund.
		refund := avax.UTXOID{TxID: tx.ID(), OutputIndex: uint32(len(utx.Outs))}
		r.refunds[refund.InputID()] = v.balance
	}

	return outcome
}

// requireL1BalanceZero fails the test unless every node reports the L1
// validator with a zero fee balance. It is the guarantee that
// [platform.DisableL1ValidatorTx] makes: the remaining balance is refunded,
// not kept.
func (r *Runner) requireL1BalanceZero(validationID ids.ID) {
	for i, c := range r.clients {
		vdr, _, err := c.GetL1Validator(r.tc.DefaultContext(), validationID)
		require.NoError(r.tc, err)
		require.Zero(r.tc, vdr.Balance, "node %d: disabled L1 validator %s keeps balance %d", i, validationID, vdr.Balance)
	}
}

// addAutoRenewedValidator adds a primary network validator that restakes each
// period, for a node that does not exist.
func (r *Runner) addAutoRenewedValidator(a [4]uint64) outcome {
	// 2%, the local network's minimum delegation fee.
	const minShares = reward.PercentDenominator / 50

	rng := newRand(a[0])
	pop, err := signer.NewProofOfPossession(blsKey(rng))
	require.NoError(r.tc, err)

	from := r.actor(a[3])
	id := nodeID(rng)
	weight := r.minValidatorStake * (1 + a[1]%4)
	shares := minShares + uint32(a[2]%(reward.PercentDenominator-minShares+1))
	utx, err := r.builder.NewAddAutoRenewedValidatorTx(
		id, weight, pop,
		owner(from), owner(from), owner(from),
		shares, shares,
		stakeDuration(a[2]),
		r.spend(from)...,
	)
	if err != nil {
		return r.skip(err)
	}

	tx := r.sign(utx)
	outcome := r.commit(tx, a[3]>>32)
	if outcome == committed {
		r.requireValidator(id, weight)
		r.expectStakeReturn(tx.ID(), utx.Outs, utx.StakeOuts)
		r.autoRenew.put(tx.ID(), autoRenewed{txID: tx.ID(), nodeID: id})
	}

	return outcome
}

// setAutoRenewedValidatorConfig changes the config of an auto-renewed
// validator that addAutoRenewedValidator created.
func (r *Runner) setAutoRenewedValidatorConfig(a [4]uint64) outcome {
	// 2%, the local network's minimum delegation fee.
	const minShares = reward.PercentDenominator / 50

	_, v, ok := r.autoRenew.draw(active, a[0])
	if !ok {
		return skipped
	}
	shares := minShares + uint32(a[1]%(reward.PercentDenominator-minShares+1))
	utx, err := r.builder.NewSetAutoRenewedValidatorConfigTx(v.txID, shares, stakeDuration(a[2]), r.options(r.actor(a[3]))...)
	if err != nil {
		return r.skip(err)
	}

	outcome := r.commit(r.sign(utx), a[3]>>32)
	if outcome == committed {
		// The config change must leave the validator in the set.
		r.requireValidatorPresent(v.nodeID)
	}

	return outcome
}

// export exports AVAX from the P-Chain to the X-Chain. The value leaves the
// P-Chain for shared memory. The model is P-Chain only, so the oracles check
// the P-Chain side: the inputs are consumed and the change is right. The
// value oracle tolerates the drop, because value only leaving is not a bug.
func (r *Runner) export(a [4]uint64) outcome {
	out := r.payment(r.actor(a[1]), a[2])
	utx, err := r.builder.NewExportTx(r.xChainID, out, r.spend(r.actor(a[0]))...)
	if err != nil {
		return r.skip(err)
	}

	return r.commit(r.sign(utx), a[3])
}

// exportToP exports AVAX from the X-Chain to the P-Chain, owned by an actor.
// The value leaves the X-Chain for shared memory, where a later import can
// bring it onto the P-Chain. It is tracked as an atomicUTXO so the value
// oracle keeps counting the in-flight value.
func (r *Runner) exportToP(a [4]uint64) outcome {
	to := owner(r.actor(a[2]))
	value := 1 + a[1]%units.KiloAvax
	out := &avax.TransferableOutput{
		Asset: avax.Asset{ID: r.avaxAssetID},
		Out:   &secp256k1fx.TransferOutput{Amt: value, OutputOwners: *to},
	}
	if _, err := r.xWallet.IssueExportTx(constants.PlatformChainID, []*avax.TransferableOutput{out}, common.WithContext(r.tc.DefaultContext())); err != nil {
		return r.skip(err)
	}

	r.inFlight += value
	return committed
}

// importToP imports the atomic UTXOs the fuzzer exported X->P onto the
// P-Chain. It exercises P's ImportTx. The runner's backend does not fetch
// shared-memory UTXOs on its own, so the op first pulls them in.
func (r *Runner) importToP(a [4]uint64) outcome {
	ctx := r.tc.DefaultContext()
	require.NoError(r.tc, primary.AddAllUTXOs(ctx, r.utxos, r.clients[0], platform.Codec, r.xChainID, constants.PlatformChainID, r.actors))

	utx, err := r.builder.NewImportTx(r.xChainID, owner(r.actor(a[0])), r.options(r.actor(a[0]))...)
	if err != nil {
		return r.skip(err)
	}

	outcome := r.commit(r.sign(utx), a[3])
	if outcome == committed {
		// The imported value left shared memory for the P-Chain.
		for _, in := range utx.ImportedInputs {
			r.inFlight -= in.In.Amount()
		}
	}

	return outcome
}

// permissionedSubnets returns the subnets that are still permissioned: created
// and not converted to an L1. Only these take CreateChainTx and subnet
// validators.
func (r *Runner) permissionedSubnets() []*subnet {
	var open []*subnet
	for _, s := range r.subnets.all() {
		if !s.converted {
			open = append(open, s)
		}
	}
	return open
}

// createChain creates a chain on a permissioned subnet. The chain's VM is
// random and no node tracks the subnet, so no VM runs; the tx only registers
// the chain in P-Chain state.
func (r *Runner) createChain(a [4]uint64) outcome {
	open := r.permissionedSubnets()
	if len(open) == 0 {
		return skipped
	}

	s := open[a[0]%uint64(len(open))]
	rng := newRand(a[1])
	var vmID ids.ID
	fill(rng, vmID[:])
	genesis := make([]byte, 1+a[2]%64)
	fill(rng, genesis)
	utx, err := r.builder.NewCreateChainTx(s.id, genesis, vmID, nil, "fuzz", r.options(r.actor(a[3]))...)
	if err != nil {
		return r.skip(err)
	}

	tx := r.sign(utx)
	outcome := r.commit(tx, a[3]>>32)
	if outcome == committed {
		r.requireChain(tx.ID(), s.id)
	}

	return outcome
}

// requireChain fails the test unless every node reports the chain on the
// subnet. It is the guarantee that [platform.CreateChainTx] makes.
func (r *Runner) requireChain(chainID, subnetID ids.ID) {
	for i, c := range r.clients {
		chains, err := c.GetBlockchains(r.tc.DefaultContext())
		require.NoError(r.tc, err)

		found := false
		for _, b := range chains {
			if b.ID == chainID {
				require.Equal(r.tc, subnetID, b.SubnetID, "node %d reports chain %s on the wrong subnet", i, chainID)
				found = true
				break
			}
		}
		require.True(r.tc, found, "node %d does not report created chain %s", i, chainID)
	}
}

// addSubnetValidator adds a current primary network validator to a
// permissioned subnet. Permissioned subnet validators stake no AVAX; their
// weight is assigned by the subnet owner.
func (r *Runner) addSubnetValidator(a [4]uint64) outcome {
	open := r.permissionedSubnets()
	if len(open) == 0 {
		return skipped
	}
	s := open[a[0]%uint64(len(open))]

	vdrs, err := r.clients[0].GetCurrentValidators(r.tc.DefaultContext(), constants.PrimaryNetworkID, nil)
	require.NoError(r.tc, err)

	// A node already validating s cannot be added again, and the API does not
	// promise an order, so sort and skip the ones already on s.
	slices.SortFunc(vdrs, func(x, y platformvm.ClientPermissionlessValidator) int {
		return x.NodeID.Compare(y.NodeID)
	})
	var open2 []platformvm.ClientPermissionlessValidator
	for _, v := range vdrs {
		if !slices.Contains(s.validators, v.NodeID) {
			open2 = append(open2, v)
		}
	}
	if len(open2) == 0 {
		return skipped
	}

	target := open2[a[1]%uint64(len(open2))]
	vdr := &platform.SubnetValidator{
		Validator: platform.Validator{
			NodeID: target.NodeID,
			// A subnet validator cannot outlast its primary validation.
			End:  min(target.EndTime, uint64(time.Now().Add(stakeDuration(a[2])).Unix())),
			Wght: 1 + a[2]%100,
		},
		Subnet: s.id,
	}
	utx, err := r.builder.NewAddSubnetValidatorTx(vdr, r.options(r.actor(a[3]))...)
	if err != nil {
		return r.skip(err)
	}

	outcome := r.commit(r.sign(utx), a[3]>>32)
	if outcome == committed {
		r.requireSubnetValidator(s.id, target.NodeID)
		s.validators = append(s.validators, target.NodeID)
	}

	return outcome
}

// requireSubnetValidator fails the test unless every node reports the node as
// a current validator of the subnet.
func (r *Runner) requireSubnetValidator(subnetID ids.ID, nodeID ids.NodeID) {
	for i, c := range r.clients {
		vdrs, err := c.GetCurrentValidators(r.tc.DefaultContext(), subnetID, []ids.NodeID{nodeID})
		require.NoError(r.tc, err)
		require.Len(r.tc, vdrs, 1, "node %d does not report subnet validator %s on %s", i, nodeID, subnetID)
	}
}

// removeSubnetValidator removes a subnet validator that addSubnetValidator
// added.
func (r *Runner) removeSubnetValidator(a [4]uint64) outcome {
	var cands []*subnet
	for _, s := range r.permissionedSubnets() {
		if len(s.validators) > 0 {
			cands = append(cands, s)
		}
	}
	if len(cands) == 0 {
		return skipped
	}

	s := cands[a[0]%uint64(len(cands))]
	i := a[1] % uint64(len(s.validators))
	nodeID := s.validators[i]
	utx, err := r.builder.NewRemoveSubnetValidatorTx(nodeID, s.id, r.options(r.actor(a[2]))...)
	if err != nil {
		return r.skip(err)
	}

	outcome := r.commit(r.sign(utx), a[3])
	if outcome == committed {
		r.requireNoSubnetValidator(s.id, nodeID)
		s.validators = slices.Delete(s.validators, int(i), int(i)+1)
	}

	return outcome
}

// requireNoSubnetValidator fails the test unless every node reports the node
// as no longer a validator of the subnet.
func (r *Runner) requireNoSubnetValidator(subnetID ids.ID, nodeID ids.NodeID) {
	for i, c := range r.clients {
		vdrs, err := c.GetCurrentValidators(r.tc.DefaultContext(), subnetID, []ids.NodeID{nodeID})
		require.NoError(r.tc, err)
		require.Empty(r.tc, vdrs, "node %d still reports removed subnet validator %s on %s", i, nodeID, subnetID)
	}
}

// replay reissues a committed tx. Whether a node admits it does not matter;
// the next check fails if the network applies it twice.
func (r *Runner) replay(a [4]uint64) outcome {
	if len(r.accepted) == 0 {
		return skipped
	}

	tx := r.accepted[a[0]%uint64(len(r.accepted))]
	if _, path, err := r.issue(tx, a[1]); err != nil {
		r.decided(path, err.Error())
		return refused
	}
	return admitted
}

// doubleSpend issues two txs that spend the same inputs to two nodes at once.
// The network must commit exactly one. It uses only the API: the two issues
// run in their own goroutines, which cannot fail the test as issue does.
func (r *Runner) doubleSpend(a [4]uint64) outcome {
	utx, err := r.builder.NewBaseTx(r.payment(r.actor(a[1]), a[2]), r.spend(r.actor(a[0]))...)
	if err != nil {
		return r.skip(err)
	}

	txs := []*platform.Tx{r.sign(utx), r.sign(r.redirect(utx, a[1]+1))}
	var (
		wg   sync.WaitGroup
		errs [2]error
	)
	for i, tx := range txs {
		wg.Go(func() {
			_, errs[i] = r.client(a[3]+uint64(i)).IssueTx(r.tc.DefaultContext(), tx.Bytes())
		})
	}
	wg.Wait()
	if errs[0] != nil && errs[1] != nil {
		return refused
	}

	var winner int
	r.tc.Eventually(func() bool {
		for i, tx := range txs {
			if r.txStatus(r.clients[0], tx.ID()) == status.Committed {
				winner = i
				return true
			}
		}

		return false
	}, decideTimeout, pollInterval, "neither of two conflicting txs committed")

	tx := txs[winner]
	r.awaitAll(tx.ID())
	require.NoError(r.tc, r.backend.AcceptTx(r.tc.DefaultContext(), tx))
	r.accepted = append(r.accepted, tx)
	r.forbidden = append(r.forbidden, txs[1-winner].ID())

	return committed
}

// inflate issues a tx whose outputs exceed its inputs.
func (r *Runner) inflate(a [4]uint64) outcome {
	utx, err := r.builder.NewBaseTx(r.payment(r.actor(a[1]), a[2]), r.spend(r.actor(a[0]))...)
	if err != nil {
		return r.skip(err)
	}

	outs := cloneOutputs(utx.Outs)
	out := outs[0].Out.(*secp256k1fx.TransferOutput)
	outs[0].Out = &secp256k1fx.TransferOutput{
		Amt:          out.Amt + 1 + a[3]%units.Avax,
		OutputOwners: out.OutputOwners,
	}
	avax.SortTransferableOutputs(outs, platform.Codec)
	utx.Outs = outs

	tx := r.sign(utx)
	c, path, err := r.issue(tx, a[3])
	if err != nil {
		r.decided(path, err.Error())
		return refused
	}
	// Gossip has no reply, so the node's status tells whether it dropped the
	// tx.
	if path == pathGossip {
		if st, reason := r.await(c, tx.ID()); st == status.Dropped {
			r.decided(path, reason)
			return refused
		}
	}

	r.forbidden = append(r.forbidden, tx.ID())

	return admitted
}

// commit issues tx on the node and path that arg picks, and waits until every
// node commits it. The tx's effects join the model only if it commits.
func (r *Runner) commit(tx *platform.Tx, arg uint64) outcome {
	txID := tx.ID()
	c, path, err := r.issue(tx, arg)
	if err != nil {
		r.log.Info("tx refused", zap.Stringer("txID", txID), zap.Error(err))
		r.decided(path, err.Error())
		return refused
	}

	if st, reason := r.await(c, txID); st != status.Committed {
		r.decided(path, reason)
		return dropped
	}

	r.awaitAll(txID)
	require.NoError(r.tc, r.backend.AcceptTx(r.tc.DefaultContext(), tx))
	r.accepted = append(r.accepted, tx)

	return committed
}

// await polls c until it decides txID, and returns the decision and, for a
// dropped tx, the reason. A tx left undecided past decideTimeout fails the
// test.
func (r *Runner) await(c *platformvm.Client, txID ids.ID) (status.Status, string) {
	deadline := time.Now().Add(decideTimeout)
	for {
		res, err := c.GetTxStatus(r.tc.DefaultContext(), txID)
		require.NoError(r.tc, err)

		switch res.Status {
		case status.Committed, status.Aborted:
			return res.Status, ""
		case status.Dropped:
			r.log.Info("tx dropped", zap.Stringer("txID", txID), zap.String("reason", res.Reason))
			return res.Status, res.Reason
		}
		require.True(r.tc, time.Now().Before(deadline), "tx %s still %s after %s", txID, res.Status, decideTimeout)
		time.Sleep(pollInterval)
	}
}

// The paths that a tx can come in on.
const (
	pathAPI    = "api"
	pathGossip = "gossip"
)

// issue sends tx to the node that arg picks, on the path that arg picks: the
// node's API, or gossip from the Byzantine peer connected to the node. It
// returns the node's client, the path, and the API's error. Gossip has no
// reply, so its error is always nil.
func (r *Runner) issue(tx *platform.Tx, arg uint64) (*platformvm.Client, string, error) {
	n := uint64(len(r.clients))
	i := arg % n
	c := r.clients[i]
	ctx := r.tc.DefaultContext()
	if (arg/n)%2 == 0 {
		_, err := c.IssueTx(ctx, tx.Bytes())
		return c, pathAPI, err
	}

	require.NoError(r.tc, r.gossipers[i].gossipTx(ctx, tx))
	return c, pathGossip, nil
}

// decided records the check that refused or dropped the current op's tx.
// IDs and amounts are cut from reason, so that one check counts as one
// reason.
func (r *Runner) decided(path, reason string) {
	idPattern := regexp.MustCompile(`[1-9A-HJ-NP-Za-km-z]{30,}`)
	numberPattern := regexp.MustCompile(`[0-9]+`)
	reason = numberPattern.ReplaceAllString(idPattern.ReplaceAllString(reason, "<id>"), "<n>")
	r.reason = path + ": " + reason
}

// awaitAll fails the test unless every node commits txID. Call it only after
// one node has.
func (r *Runner) awaitAll(txID ids.ID) {
	for i, c := range r.clients {
		r.tc.Eventually(func() bool {
			return r.txStatus(c, txID) == status.Committed
		}, decideTimeout, pollInterval, fmt.Sprintf("node %d did not commit tx %s", i, txID))
	}
}

func (r *Runner) txStatus(c *platformvm.Client, txID ids.ID) status.Status {
	res, err := c.GetTxStatus(r.tc.DefaultContext(), txID)
	require.NoError(r.tc, err)
	return res.Status
}

func (r *Runner) sign(utx platform.UnsignedTx) *platform.Tx {
	tx, err := psigner.SignUnsigned(r.tc.DefaultContext(), r.signer, utx)
	require.NoError(r.tc, err)
	return tx
}

func (r *Runner) fatalf(format string, args ...any) {
	r.tc.Errorf(format, args...)
	r.tc.FailNow()
}

func (r *Runner) skip(err error) outcome {
	r.log.Info("skipped op", zap.Error(err))
	return skipped
}

// expectStakeReturn records the UTXOs a staker gets back when it ends, and
// where its rewards start.
func (r *Runner) expectStakeReturn(txID ids.ID, outs, stake []*avax.TransferableOutput) {
	for i, out := range stake {
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{TxID: txID, OutputIndex: uint32(len(outs) + i)},
			Asset:  out.Asset,
			Out:    out.Output(),
		}
		r.returns[utxo.InputID()] = utxo
	}
	r.rewardIndex[txID] = uint32(len(outs) + len(stake))
}

// redirect returns a copy of utx that spends the same inputs but pays its
// first output to another actor. The copy has the same size, so it pays the
// same fee.
func (r *Runner) redirect(utx *platform.BaseTx, arg uint64) *platform.BaseTx {
	outs := cloneOutputs(utx.Outs)
	out := outs[0].Out.(*secp256k1fx.TransferOutput)
	to := r.actor(arg)
	if to == out.Addrs[0] {
		to = r.actor(arg + 1)
	}
	outs[0].Out = &secp256k1fx.TransferOutput{Amt: out.Amt, OutputOwners: *owner(to)}
	avax.SortTransferableOutputs(outs, platform.Codec)

	return &platform.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    utx.NetworkID,
		BlockchainID: utx.BlockchainID,
		Outs:         outs,
		Ins:          utx.Ins,
		Memo:         utx.Memo,
	}}
}

func (r *Runner) client(arg uint64) *platformvm.Client {
	return r.clients[arg%uint64(len(r.clients))]
}

func (r *Runner) actor(arg uint64) ids.ShortID {
	return r.actors[arg%uint64(len(r.actors))]
}

// payment returns one output that pays to up to 1000 AVAX.
func (r *Runner) payment(to ids.ShortID, arg uint64) []*avax.TransferableOutput {
	return []*avax.TransferableOutput{{
		Asset: avax.Asset{ID: r.avaxAssetID},
		Out:   &secp256k1fx.TransferOutput{Amt: 1 + arg%units.KiloAvax, OutputOwners: *owner(to)},
	}}
}

// spend returns options that pay only from addr, with change to addr.
func (r *Runner) spend(addr ids.ShortID) []common.Option {
	return append(r.options(addr), common.WithCustomAddresses(set.Of(addr)))
}

// options returns options that pay from any actor, with change to addr. Txs
// that need a subnet or validator owner's signature use it: limiting the
// addresses would also limit who can sign.
func (r *Runner) options(addr ids.ShortID) []common.Option {
	return []common.Option{
		common.WithContext(r.tc.DefaultContext()),
		common.WithChangeOwner(owner(addr)),
	}
}

// multisig returns an owner over a non-empty subset of actors picked by mask,
// with a threshold picked by t.
func (r *Runner) multisig(mask, t uint64) *secp256k1fx.OutputOwners {
	var (
		subset = mask%(uint64(1)<<len(r.actors)-1) + 1
		addrs  []ids.ShortID
	)
	for i, addr := range r.actors {
		if subset&(1<<i) != 0 {
			addrs = append(addrs, addr)
		}
	}
	utils.Sort(addrs)

	return &secp256k1fx.OutputOwners{
		Threshold: 1 + uint32(t%uint64(len(addrs))),
		Addrs:     addrs,
	}
}

func (r *Runner) pChainOwner(mask, t uint64) message.PChainOwner {
	o := r.multisig(mask, t)
	return message.PChainOwner{Threshold: o.Threshold, Addresses: o.Addrs}
}

func owner(addr ids.ShortID) *secp256k1fx.OutputOwners {
	return &secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{addr}}
}

func cloneOutputs(outs []*avax.TransferableOutput) []*avax.TransferableOutput {
	clones := make([]*avax.TransferableOutput, len(outs))
	for i, out := range outs {
		clone := *out
		clones[i] = &clone
	}

	return clones
}

// stakeDuration returns 15s to 2m15s. tmpnet's minimum is 2s, so stakers end,
// and get their stake back, within a run.
func stakeDuration(arg uint64) time.Duration {
	return 15*time.Second + time.Duration(arg%120)*time.Second
}

// l1Balance returns zero (an inactive validator), a balance that runs out in
// seconds at the local network's minimum fee of 1 nAVAX per second, or one
// that lasts the run.
func l1Balance(arg uint64) uint64 {
	switch arg % 3 {
	case 0:
		return 0
	case 1:
		return 10 + arg%100
	default:
		return units.MilliAvax
	}
}

func nodeID(rng *rand.Rand) ids.NodeID {
	var id ids.NodeID
	fill(rng, id[:])
	return id
}

func blsKey(rng *rand.Rand) *localsigner.LocalSigner {
	b := make([]byte, 32)
	for {
		// About half of all 32-byte strings are valid scalars.
		fill(rng, b)
		if sk, err := localsigner.FromBytes(b); err == nil {
			return sk
		}
	}
}

func fill(rng *rand.Rand, b []byte) {
	for i := range b {
		b[i] = byte(rng.Uint32())
	}
}
