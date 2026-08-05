// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"encoding/binary"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	ethcommon "github.com/ava-labs/libevm/common"
)

// abiWord left-pads v into a 32-byte ABI word.
func abiWord(v uint64) []byte {
	word := make([]byte, 32)
	binary.BigEndian.PutUint64(word[24:], v)
	return word
}

func delegateCalldata(nodeID ids.NodeID, endTime uint64) []byte {
	calldata := txs.SelectorDelegate[:]
	nodeWord := make([]byte, 32)
	copy(nodeWord, nodeID[:])
	calldata = append(calldata, nodeWord...)
	return append(calldata, abiWord(endTime)...)
}

func addValidatorCalldata(
	nodeID ids.NodeID,
	endTime uint64,
	publicKey []byte,
	pop []byte,
	feeBips uint32,
) []byte {
	calldata := txs.SelectorAddValidator[:]
	nodeWord := make([]byte, 32)
	copy(nodeWord, nodeID[:])
	calldata = append(calldata, nodeWord...)
	calldata = append(calldata, abiWord(endTime)...)
	// Head: two dynamic offsets measured from the start of the argument block.
	const headWords = 5
	pkOffset := uint64(headWords * 32)
	popOffset := pkOffset + 32 + uint64(padTo32(len(publicKey)))
	calldata = append(calldata, abiWord(pkOffset)...)
	calldata = append(calldata, abiWord(popOffset)...)
	calldata = append(calldata, abiWord(uint64(feeBips))...)
	// Tails.
	calldata = append(calldata, abiWord(uint64(len(publicKey)))...)
	calldata = append(calldata, padRight(publicKey)...)
	calldata = append(calldata, abiWord(uint64(len(pop)))...)
	return append(calldata, padRight(pop)...)
}

func padTo32(n int) int {
	return (n + 31) / 32 * 32
}

func padRight(b []byte) []byte {
	padded := make([]byte, padTo32(len(b)))
	copy(padded, b)
	return padded
}

// newSignedEthStake signs a staking call carrying stakeNAVAX of value.
func newSignedEthStake(
	t *testing.T,
	env *environment,
	key *secp256k1.PrivateKey,
	nonce uint64,
	stakeNAVAX uint64,
	calldata []byte,
) *txs.Tx {
	t.Helper()
	return newSignedEthCall(t, key, nonce, ids.ShortID(txs.EthStakingAddress),
		stakeNAVAX, defaultEthGasLimit, defaultFeeCapWei, ethChainID(env), 0, calldata)
}

func newBLSKey(t *testing.T) ([]byte, []byte, *signer.ProofOfPossession) {
	t.Helper()
	sk, err := localsigner.New()
	require.NoError(t, err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)
	return pop.PublicKey[:], pop.ProofOfPossession[:], pop
}

func TestEthRLPTxDelegate(t *testing.T) {
	require := require.New(t)
	env, onAcceptState, feeCalculator := ethFeeEnv(t, upgradetest.Latest, 1)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	sender := ids.ShortID(key.PublicKey().EthAddress())
	fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, 100*units.Avax)

	nodeID := genesistest.DefaultNodeIDs[0]
	endTime := genesistest.DefaultValidatorEndTimeUnix
	const stake = 2 * units.MilliAvax

	tx := newSignedEthStake(t, env, key, 0, stake, delegateCalldata(nodeID, endTime))
	_, _, _, err = StandardTx(&env.backend, feeCalculator, tx, onAcceptState)
	require.NoError(err)

	// The delegator is in the current staker set with the eth-derived owner.
	staker, err := onAcceptState.GetCurrentDelegatorIterator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)
	defer staker.Release()
	require.True(staker.Next())
	delegator := staker.Value()
	require.Equal(uint64(stake), delegator.Weight)
	require.Equal(endTime, uint64(delegator.EndTime.Unix()))

	// The staker points at a native delegator tx, not at the eth tx.
	stakerTx, _, err := onAcceptState.GetTx(delegator.TxID)
	require.NoError(err)
	derived, ok := stakerTx.Unsigned.(*txs.AddPermissionlessDelegatorTx)
	require.True(ok)
	require.NotEqual(tx.ID(), delegator.TxID)

	owner := derived.DelegationRewardsOwner.(*secp256k1fx.OutputOwners)
	require.Equal(uint32(1), owner.Threshold)
	require.Equal([]ids.ShortID{sender}, owner.Addrs)
	require.Equal(uint64(stake), derived.StakeOuts[0].Out.Amount())
	require.Equal(constants.PrimaryNetworkID, derived.Subnet)
	// The derived tx declares the inputs the eth tx actually consumed.
	require.NotEmpty(derived.Ins)

	// Staking the same amount to the same node again is a distinct derivation.
	tx2 := newSignedEthStake(t, env, key, 1, stake, delegateCalldata(nodeID, endTime))
	_, _, _, err = StandardTx(&env.backend, feeCalculator, tx2, onAcceptState)
	require.NoError(err)
}

func TestEthRLPTxDelegateRules(t *testing.T) {
	nodeID := genesistest.DefaultNodeIDs[0]
	endTime := genesistest.DefaultValidatorEndTimeUnix

	tests := []struct {
		name     string
		stake    uint64
		calldata func() []byte
		err      error
	}{
		{
			name:     "below min delegator stake",
			stake:    1, // MinDelegatorStake is 1 MilliAvax in the test config
			calldata: func() []byte { return delegateCalldata(nodeID, endTime) },
			err:      ErrWeightTooSmall,
		},
		{
			name:  "end time after the validator's",
			stake: 2 * units.MilliAvax,
			calldata: func() []byte {
				return delegateCalldata(nodeID, endTime+1)
			},
			err: ErrPeriodMismatch,
		},
		{
			name:  "unknown validator",
			stake: 2 * units.MilliAvax,
			calldata: func() []byte {
				return delegateCalldata(ids.GenerateTestNodeID(), endTime)
			},
			err: database.ErrNotFound,
		},
		{
			name:  "duration too short",
			stake: 2 * units.MilliAvax,
			calldata: func() []byte {
				short := uint64(genesistest.DefaultValidatorStartTime.Add(time.Minute).Unix())
				return delegateCalldata(nodeID, short)
			},
			err: ErrStakeTooShort,
		},
		{
			name:  "unknown selector",
			stake: 2 * units.MilliAvax,
			calldata: func() []byte {
				return append([]byte{0xde, 0xad, 0xbe, 0xef}, abiWord(0)...)
			},
			err: txs.ErrUnknownSelector,
		},
		{
			name:     "truncated calldata",
			stake:    2 * units.MilliAvax,
			calldata: func() []byte { return txs.SelectorDelegate[:] },
			err:      txs.ErrShortCalldata,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, onAcceptState, feeCalculator := ethFeeEnv(t, upgradetest.Latest, 1)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()

			key, err := secp256k1.NewPrivateKey()
			require.NoError(t, err)
			sender := ids.ShortID(key.PublicKey().EthAddress())
			fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, 100*units.Avax)

			tx := newSignedEthStake(t, env, key, 0, tt.stake, tt.calldata())
			_, _, _, err = StandardTx(&env.backend, feeCalculator, tx, onAcceptState)
			require.ErrorIs(t, err, tt.err)
		})
	}
}

func TestEthRLPTxAddValidator(t *testing.T) {
	require := require.New(t)
	env, onAcceptState, feeCalculator := ethFeeEnv(t, upgradetest.Latest, 1)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	sender := ids.ShortID(key.PublicKey().EthAddress())
	fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, 100*units.Avax)

	publicKey, pop, popSigner := newBLSKey(t)
	nodeID := ids.GenerateTestNodeID()
	endTime := uint64(genesistest.DefaultValidatorStartTime.Add(2 * 24 * time.Hour).Unix())
	const stake = 10 * units.MilliAvax
	const feeBips = reward.PercentDenominator / 4

	calldata := addValidatorCalldata(nodeID, endTime, publicKey, pop, feeBips)
	tx := newSignedEthStake(t, env, key, 0, stake, calldata)
	_, _, _, err = StandardTx(&env.backend, feeCalculator, tx, onAcceptState)
	require.NoError(err)

	validator, err := onAcceptState.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)
	require.Equal(uint64(stake), validator.Weight)
	require.Equal(endTime, uint64(validator.EndTime.Unix()))
	require.Equal(popSigner.PublicKey[:], bls.PublicKeyToCompressedBytes(validator.PublicKey))

	stakerTx, _, err := onAcceptState.GetTx(validator.TxID)
	require.NoError(err)
	derived, ok := stakerTx.Unsigned.(*txs.AddPermissionlessValidatorTx)
	require.True(ok)
	require.Equal(uint32(feeBips), derived.DelegationShares)
	for _, ownerIntf := range []any{derived.ValidatorRewardsOwner, derived.DelegatorRewardsOwner} {
		owner := ownerIntf.(*secp256k1fx.OutputOwners)
		require.Equal([]ids.ShortID{sender}, owner.Addrs)
	}

	// A mismatched proof of possession is rejected.
	otherKey, _, _ := newBLSKey(t)
	bad := addValidatorCalldata(ids.GenerateTestNodeID(), endTime, otherKey, pop, feeBips)
	tx = newSignedEthStake(t, env, key, 1, stake, bad)
	_, _, _, err = StandardTx(&env.backend, feeCalculator, tx, onAcceptState)
	require.ErrorIs(err, signer.ErrInvalidProofOfPossession)
}

func TestEthRLPTxAddValidatorRules(t *testing.T) {
	publicKeyOf := func(t *testing.T) ([]byte, []byte) {
		pk, pop, _ := newBLSKey(t)
		return pk, pop
	}
	endTime := uint64(genesistest.DefaultValidatorStartTime.Add(2 * 24 * time.Hour).Unix())

	tests := []struct {
		name    string
		stake   uint64
		endTime uint64
		feeBips uint32
		err     error
	}{
		{
			name:    "below min validator stake",
			stake:   1 * units.MilliAvax, // MinValidatorStake is 5 MilliAvax
			endTime: endTime,
			feeBips: reward.PercentDenominator / 4,
			err:     ErrWeightTooSmall,
		},
		{
			name:    "above max validator stake",
			stake:   50 * units.Avax, // MaxValidatorStake is 500 MilliAvax
			endTime: endTime,
			feeBips: reward.PercentDenominator / 4,
			err:     ErrWeightTooLarge,
		},
		{
			name:    "delegation fee below minimum",
			stake:   10 * units.MilliAvax,
			endTime: endTime,
			feeBips: 1, // MinDelegationFee is 20000
			err:     ErrInsufficientDelegationFee,
		},
		{
			name:    "duration too long",
			stake:   10 * units.MilliAvax,
			endTime: uint64(genesistest.DefaultValidatorStartTime.Add(500 * 24 * time.Hour).Unix()),
			feeBips: reward.PercentDenominator / 4,
			err:     ErrStakeTooLong,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, onAcceptState, feeCalculator := ethFeeEnv(t, upgradetest.Latest, 1)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()

			key, err := secp256k1.NewPrivateKey()
			require.NoError(t, err)
			sender := ids.ShortID(key.PublicKey().EthAddress())
			fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, 1000*units.Avax)

			pk, pop := publicKeyOf(t)
			calldata := addValidatorCalldata(ids.GenerateTestNodeID(), tt.endTime, pk, pop, tt.feeBips)
			tx := newSignedEthStake(t, env, key, 0, tt.stake, calldata)
			_, _, _, err = StandardTx(&env.backend, feeCalculator, tx, onAcceptState)
			require.ErrorIs(t, err, tt.err)
		})
	}
}

// A plain transfer to a non-system address still rejects calldata.
func TestEthRLPTxCalldataOnlyForSystemAddress(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)
	key, err := secp256k1.NewPrivateKey()
	require.NoError(t, err)

	tx := newSignedEthCall(t, key, 0, ids.GenerateTestShortID(), units.Avax, defaultEthGasLimit,
		defaultFeeCapWei, ethChainID(env), 0, delegateCalldata(ids.GenerateTestNodeID(), 1))
	require.ErrorIs(t, tx.Unsigned.SyntacticVerify(env.ctx), txs.ErrNonEmptyCalldata)
}

func TestEthStakingAddressIsNotAnEOA(t *testing.T) {
	// The system address must not be a plausible key-derived address, so no
	// one can hold its private key and receive plain transfers there.
	require.Equal(t, ethcommon.HexToAddress("0x0100000000000000000000000000000000000001"), txs.EthStakingAddress)
	require.NotEqual(t, big.NewInt(0).Bytes(), txs.EthStakingAddress.Bytes())
}

// The reward path resolves the staker by TxID and dispatches on the concrete
// staker type, so an eth-authorized delegator must be rewarded exactly like a
// native one, paying out to the eth-derived owner.
func TestEthRLPTxDelegatorIsRewarded(t *testing.T) {
	require := require.New(t)
	env, onAcceptState, feeCalculator := ethFeeEnv(t, upgradetest.Latest, 1)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	sender := ids.ShortID(key.PublicKey().EthAddress())
	fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, 100*units.Avax)

	nodeID := genesistest.DefaultNodeIDs[0]
	endTime := genesistest.DefaultValidatorEndTimeUnix
	const stake = 2 * units.MilliAvax

	tx := newSignedEthStake(t, env, key, 0, stake, delegateCalldata(nodeID, endTime))
	_, _, _, err = StandardTx(&env.backend, feeCalculator, tx, onAcceptState)
	require.NoError(err)

	delegatorIterator, err := onAcceptState.GetCurrentDelegatorIterator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)
	require.True(delegatorIterator.Next())
	delegator := delegatorIterator.Value()
	delegatorIterator.Release()

	// The reward dispatch requires the staker tx to be a concrete delegator tx.
	stakerTx, _, err := onAcceptState.GetTx(delegator.TxID)
	require.NoError(err)
	require.Implements((*txs.DelegatorTx)(nil), stakerTx.Unsigned)

	require.NoError(onAcceptState.Apply(env.state))
	env.state.SetTimestamp(delegator.EndTime)
	env.state.SetHeight(1)
	require.NoError(env.state.Commit())

	// The primary network validator ends at the same time, so the delegator is
	// the first staker to be rewarded.
	rewardTx, err := newRewardValidatorTx(t, delegator.TxID)
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)
	onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	ownerSet := set.Of(sender)
	oldBalance, err := avax.GetBalance(env.state, ownerSet)
	require.NoError(err)

	require.NoError(ProposalTx(
		&env.backend,
		state.PickFeeCalculator(env.config, onCommitState),
		rewardTx,
		onCommitState,
		onAbortState,
	))
	require.NoError(onCommitState.Apply(env.state))
	env.state.SetHeight(2)
	require.NoError(env.state.Commit())

	// The staked amount returned to the eth address.
	newBalance, err := avax.GetBalance(env.state, ownerSet)
	require.NoError(err)
	require.Equal(oldBalance+stake, newBalance)

	// The reward itself was distributed: the genesis validator takes a 100%
	// delegation fee, so the whole delegator reward is credited to it as a
	// deferred delegatee reward. What matters here is that the reward
	// machinery ran against an eth-authorized delegator at all.
	stakingInfo, err := env.state.GetStakingInfo(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)
	require.NotZero(stakingInfo.DelegateeReward)
}
