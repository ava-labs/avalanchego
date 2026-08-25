// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warpauth

import (
	_ "embed"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/vm/runtime"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

//go:embed MockWarp.bin-runtime
var mockWarpRuntime string

// ABI tuples, field names match PChain.sol.
type (
	utxo struct {
		TxID        [32]byte
		OutputIndex uint32
		Amount      uint64
	}
	owners struct {
		Locktime  uint64
		Threshold uint32
		Addrs     []common.Address
	}
	out struct {
		Amount uint64
		Owners owners
	}
	validator struct {
		NodeID [20]byte
		Start  uint64
		End    uint64
		Weight uint64
	}
	blsKeys struct {
		PublicKey         []byte
		ProofOfPossession []byte
	}
	pChainOwner struct {
		Threshold uint32
		Addrs     []common.Address
	}
	l1Validator struct {
		NodeID                []byte
		Weight                uint64
		Balance               uint64
		Bls                   blsKeys
		RemainingBalanceOwner pChainOwner
		DeactivationOwner     pChainOwner
	}
	staking struct {
		Validator        validator
		SubnetID         [32]byte
		Bls              blsKeys
		Stake            []out
		ValidatorRewards owners
		DelegatorRewards owners
		DelegationShares uint32
	}
	autoRenewed struct {
		NodeID                   []byte
		Bls                      blsKeys
		Stake                    []out
		ValidatorRewards         owners
		DelegatorRewards         owners
		ValidatorAuthority       owners
		DelegationShares         uint32
		AutoCompoundRewardShares uint32
		Period                   uint64
	}
)

var (
	networkID   = uint32(12345)
	pChainID    = ids.ID{0x0a}
	avaxAssetID = ids.ID{0x0b}
	owner       = ids.ShortID{0xde, 0xad}
	ownerA      = ids.ShortID{0x01}
	ownerB      = ids.ShortID{0x02}
	subnetID    = ids.ID{0x5b}
	nodeID      = ids.NodeID{0x0e}
	popBytes    = [bls.SignatureLen]byte{0x02}
	pkBytes     = [bls.PublicKeyLen]byte{0x01}

	ins = []utxo{
		{TxID: ids.ID{0x10}, OutputIndex: 2, Amount: 5},
		{TxID: ids.ID{0x10}, OutputIndex: 3, Amount: 6},
		{TxID: ids.ID{0x11}, OutputIndex: 0, Amount: 7},
	}
	twoOwners   = owners{Locktime: 9, Threshold: 2, Addrs: []common.Address{common.Address(ownerA), common.Address(ownerB)}}
	goTwoOwners = secp256k1fx.OutputOwners{Locktime: 9, Threshold: 2, Addrs: []ids.ShortID{ownerA, ownerB}}
	oneOwner    = owners{Threshold: 1, Addrs: []common.Address{common.Address(ownerA)}}
	goOneOwner  = secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ownerA}}
	outs        = []out{{Amount: 5, Owners: oneOwner}, {Amount: 6, Owners: twoOwners}}
	goOuts      = []*avax.TransferableOutput{
		{Asset: avax.Asset{ID: avaxAssetID}, Out: &secp256k1fx.TransferOutput{Amt: 5, OutputOwners: goOneOwner}},
		{Asset: avax.Asset{ID: avaxAssetID}, Out: &secp256k1fx.TransferOutput{Amt: 6, OutputOwners: goTwoOwners}},
	}
	vdr   = validator{NodeID: nodeID, Start: 1, End: 2, Weight: 3}
	goVdr = txs.Validator{NodeID: nodeID, Start: 1, End: 2, Wght: 3}
	keys  = blsKeys{PublicKey: pkBytes[:], ProofOfPossession: popBytes[:]}
	goPoP = signer.ProofOfPossession{PublicKey: pkBytes, ProofOfPossession: popBytes}
	auth  = []uint32{0, 2}
)

func goIns(ins []utxo) []*avax.TransferableInput {
	res := make([]*avax.TransferableInput, len(ins))
	for i, in := range ins {
		res[i] = &avax.TransferableInput{
			UTXOID: avax.UTXOID{TxID: in.TxID, OutputIndex: in.OutputIndex},
			Asset:  avax.Asset{ID: avaxAssetID},
			In:     &secp256k1fx.TransferInput{Amt: in.Amount, Input: secp256k1fx.Input{SigIndices: []uint32{0}}},
		}
	}
	return res
}

func goBase(change uint64) txs.BaseTx {
	tx := txs.BaseTx{BaseTx: avax.BaseTx{NetworkID: networkID, BlockchainID: pChainID, Ins: goIns(ins)}}
	if change != 0 {
		tx.Outs = []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: avaxAssetID},
			Out:   &secp256k1fx.TransferOutput{Amt: change, OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{owner}}},
		}}
	}
	return tx
}

func goAuth() *secp256k1fx.Input {
	return &secp256k1fx.Input{SigIndices: auth}
}

type harness struct {
	t        *testing.T
	abi      abi.ABI
	mock     abi.ABI
	cfg      *runtime.Config
	contract common.Address
}

func newHarness(t *testing.T) *harness {
	require := require.New(t)
	parsed, err := abi.JSON(strings.NewReader(PChainABI))
	require.NoError(err)
	mock, err := abi.JSON(strings.NewReader(`[{"inputs":[],"name":"last","outputs":[{"type":"bytes"}],"stateMutability":"view","type":"function"}]`))
	require.NoError(err)
	initcode, err := hex.DecodeString(PChainBin)
	require.NoError(err)
	mockCode, err := hex.DecodeString(strings.TrimSpace(mockWarpRuntime))
	require.NoError(err)
	ctorArgs, err := parsed.Pack("", networkID, pChainID, avaxAssetID)
	require.NoError(err)

	statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
	require.NoError(err)
	statedb.SetCode(warp.ContractAddress, mockCode)
	cfg := &runtime.Config{
		ChainConfig: params.MergedTestChainConfig,
		State:       statedb,
		GasLimit:    30_000_000,
		Random:      &common.Hash{},
		Origin:      common.Address(owner),
	}
	_, contract, _, err := runtime.Create(append(initcode, ctorArgs...), cfg)
	require.NoError(err)
	return &harness{t: t, abi: parsed, mock: mock, cfg: cfg, contract: contract}
}

// call runs a PChain function as [owner] and returns the warp payload it sent.
func (h *harness) call(method string, args ...any) ([]byte, error) {
	input, err := h.abi.Pack(method, args...)
	require.NoError(h.t, err)
	if _, _, err := runtime.Call(h.contract, input, h.cfg); err != nil {
		return nil, err
	}
	query, err := h.mock.Pack("last")
	require.NoError(h.t, err)
	ret, _, err := runtime.Call(warp.ContractAddress, query, h.cfg)
	require.NoError(h.t, err)
	var payload []byte
	require.NoError(h.t, h.mock.UnpackIntoInterface(&payload, "last", ret))
	return payload, nil
}

func (h *harness) expect(method string, tx txs.UnsignedTx, args ...any) {
	h.t.Helper()
	b, err := txs.Codec.Marshal(txs.CodecVersion, &tx)
	require.NoError(h.t, err)
	got, err := h.call(method, args...)
	require.NoError(h.t, err, method)
	require.Equal(h.t, append(owner[:], b...), got, method)
}

func TestEncodeAllTxTypes(t *testing.T) {
	h := newHarness(t)

	h.expect("transfer",
		&txs.BaseTx{BaseTx: avax.BaseTx{NetworkID: networkID, BlockchainID: pChainID, Ins: goIns(ins), Outs: goOuts}},
		ins, outs)

	h.expect("createSubnet", &txs.CreateSubnetTx{BaseTx: goBase(4), Owner: &goTwoOwners}, ins, uint64(4), twoOwners)
	h.expect("createSubnet", &txs.CreateSubnetTx{BaseTx: goBase(0), Owner: &goTwoOwners}, ins, uint64(0), twoOwners)

	h.expect("createChain", &txs.CreateChainTx{
		BaseTx: goBase(4), SubnetID: subnetID, ChainName: "chain", VMID: ids.ID{0x77},
		FxIDs: []ids.ID{{0x88}, {0x89}}, GenesisData: []byte{1, 2, 3}, SubnetAuth: goAuth(),
	}, ins, uint64(4), subnetID, "chain", ids.ID{0x77}, [][32]byte{{0x88}, {0x89}}, []byte{1, 2, 3}, auth)

	h.expect("addSubnetValidator", &txs.AddSubnetValidatorTx{
		BaseTx: goBase(4), SubnetValidator: txs.SubnetValidator{Validator: goVdr, Subnet: subnetID}, SubnetAuth: goAuth(),
	}, ins, uint64(4), vdr, subnetID, auth)

	h.expect("removeSubnetValidator", &txs.RemoveSubnetValidatorTx{
		BaseTx: goBase(4), NodeID: nodeID, Subnet: subnetID, SubnetAuth: goAuth(),
	}, ins, uint64(4), nodeID, subnetID, auth)

	h.expect("addPermissionlessValidator", &txs.AddPermissionlessValidatorTx{
		BaseTx: goBase(4), Validator: goVdr, Subnet: subnetID, Signer: &goPoP, StakeOuts: goOuts,
		ValidatorRewardsOwner: &goOneOwner, DelegatorRewardsOwner: &goTwoOwners, DelegationShares: 20_000,
	}, ins, uint64(4), staking{
		Validator: vdr, SubnetID: subnetID, Bls: keys, Stake: outs,
		ValidatorRewards: oneOwner, DelegatorRewards: twoOwners, DelegationShares: 20_000,
	})
	h.expect("addPermissionlessValidator", &txs.AddPermissionlessValidatorTx{
		BaseTx: goBase(4), Validator: goVdr, Subnet: subnetID, Signer: &signer.Empty{}, StakeOuts: goOuts,
		ValidatorRewardsOwner: &goOneOwner, DelegatorRewardsOwner: &goTwoOwners, DelegationShares: 20_000,
	}, ins, uint64(4), staking{
		Validator: vdr, SubnetID: subnetID, Stake: outs,
		ValidatorRewards: oneOwner, DelegatorRewards: twoOwners, DelegationShares: 20_000,
	})

	h.expect("addPermissionlessDelegator", &txs.AddPermissionlessDelegatorTx{
		BaseTx: goBase(4), Validator: goVdr, Subnet: subnetID, StakeOuts: goOuts, DelegationRewardsOwner: &goOneOwner,
	}, ins, uint64(4), vdr, subnetID, outs, oneOwner)

	h.expect("transferSubnetOwnership", &txs.TransferSubnetOwnershipTx{
		BaseTx: goBase(4), Subnet: subnetID, SubnetAuth: goAuth(), Owner: &goTwoOwners,
	}, ins, uint64(4), subnetID, auth, twoOwners)

	imported := []utxo{{TxID: ids.ID{0x20}, OutputIndex: 1, Amount: 8}}
	h.expect("importTx", &txs.ImportTx{
		BaseTx: goBase(4), SourceChain: ids.ID{0xcc}, ImportedInputs: goIns(imported),
	}, ins, uint64(4), ids.ID{0xcc}, imported)

	h.expect("exportTx", &txs.ExportTx{
		BaseTx: goBase(4), DestinationChain: ids.ID{0xcc}, ExportedOutputs: goOuts,
	}, ins, uint64(4), ids.ID{0xcc}, outs)

	h.expect("convertSubnetToL1", &txs.ConvertSubnetToL1Tx{
		BaseTx: goBase(4), Subnet: subnetID, ChainID: ids.ID{0xcc}, Address: []byte{9, 9},
		Validators: []*txs.ConvertSubnetToL1Validator{{
			NodeID: nodeID[:], Weight: 3, Balance: 4, Signer: goPoP,
			RemainingBalanceOwner: message.PChainOwner{Threshold: 1, Addresses: []ids.ShortID{ownerA}},
			DeactivationOwner:     message.PChainOwner{Threshold: 2, Addresses: []ids.ShortID{ownerA, ownerB}},
		}},
		SubnetAuth: goAuth(),
	}, ins, uint64(4), subnetID, ids.ID{0xcc}, []byte{9, 9}, []l1Validator{{
		NodeID: nodeID[:], Weight: 3, Balance: 4, Bls: keys,
		RemainingBalanceOwner: pChainOwner{Threshold: 1, Addrs: []common.Address{common.Address(ownerA)}},
		DeactivationOwner:     pChainOwner{Threshold: 2, Addrs: []common.Address{common.Address(ownerA), common.Address(ownerB)}},
	}}, auth)

	h.expect("registerL1Validator", &txs.RegisterL1ValidatorTx{
		BaseTx: goBase(4), Balance: 5, ProofOfPossession: popBytes, Message: []byte{7, 7, 7},
	}, ins, uint64(4), uint64(5), popBytes[:], []byte{7, 7, 7})

	h.expect("setL1ValidatorWeight", &txs.SetL1ValidatorWeightTx{BaseTx: goBase(4), Message: []byte{7, 7}},
		ins, uint64(4), []byte{7, 7})

	h.expect("increaseL1ValidatorBalance", &txs.IncreaseL1ValidatorBalanceTx{
		BaseTx: goBase(4), ValidationID: ids.ID{0x33}, Balance: 6,
	}, ins, uint64(4), ids.ID{0x33}, uint64(6))

	h.expect("disableL1Validator", &txs.DisableL1ValidatorTx{
		BaseTx: goBase(4), ValidationID: ids.ID{0x33}, DisableAuth: goAuth(),
	}, ins, uint64(4), ids.ID{0x33}, auth)

	h.expect("addAutoRenewedValidator", &txs.AddAutoRenewedValidatorTx{
		BaseTx: goBase(4), ValidatorNodeID: nodeID[:], Signer: &goPoP, StakeOuts: goOuts,
		ValidatorRewardsOwner: &goOneOwner, DelegatorRewardsOwner: &goTwoOwners, ValidatorAuthority: &goOneOwner,
		DelegationShares: 20_000, AutoCompoundRewardShares: 30_000, Period: 86_400,
	}, ins, uint64(4), autoRenewed{
		NodeID: nodeID[:], Bls: keys, Stake: outs,
		ValidatorRewards: oneOwner, DelegatorRewards: twoOwners, ValidatorAuthority: oneOwner,
		DelegationShares: 20_000, AutoCompoundRewardShares: 30_000, Period: 86_400,
	})

	h.expect("setAutoRenewedValidatorConfig", &txs.SetAutoRenewedValidatorConfigTx{
		BaseTx: goBase(4), TxID: ids.ID{0x44}, Auth: goAuth(), AutoCompoundRewardShares: 30_000, Period: 86_400,
	}, ins, uint64(4), ids.ID{0x44}, auth, uint32(30_000), uint64(86_400))
}

func TestEncodeRejects(t *testing.T) {
	h := newHarness(t)
	reverts := func(method string, args ...any) {
		t.Helper()
		_, err := h.call(method, args...)
		require.ErrorContains(t, err, "execution reverted", method)
	}
	reverts("createSubnet", []utxo{ins[1], ins[0]}, uint64(4), twoOwners)
	reverts("createSubnet", ins, uint64(4), owners{Threshold: 1, Addrs: []common.Address{common.Address(ownerB), common.Address(ownerA)}})
	reverts("createSubnet", ins, uint64(4), owners{Threshold: 3, Addrs: twoOwners.Addrs})
	reverts("transfer", ins, []out{outs[1], outs[0]})
	reverts("registerL1Validator", ins, uint64(4), uint64(5), []byte{1}, []byte{})
	reverts("addPermissionlessDelegator", ins, uint64(4), vdr, subnetID, []out{outs[1], outs[0]}, twoOwners)
}

// The hardcoded helper addresses must match the current contract bytes.
func TestDefaultHelperAddressesMatchContract(t *testing.T) {
	for networkID, evmChainID := range map[uint32]int64{constants.MainnetID: 43114, constants.FujiID: 43113} {
		_, avaxAssetID, err := genesis.FromConfig(genesis.GetConfig(networkID))
		require.NoError(t, err)
		_, deployer, err := NickDeployTx(big.NewInt(evmChainID), networkID, avaxAssetID)
		require.NoError(t, err)
		require.Equal(t,
			[]ids.ShortID{ids.ShortID(crypto.CreateAddress(deployer, 0))},
			config.DefaultWarpHelperAddresses[networkID],
			"run: go run ./tests/warpauth/nick -network %s", constants.NetworkName(networkID),
		)
	}
}
