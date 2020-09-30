// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/codec"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// ID of the EVM VM
var (
	EVMID = ids.NewID([32]byte{'e', 'v', 'm'})
)

// Genesis returns the genesis data of the Platform Chain.
//
// Since an Avalanche network has exactly one Platform Chain, and the Platform
// Chain defines the genesis state of the network (who is staking, which chains
// exist, etc.), defining the genesis state of the Platform Chain is the same as
// defining the genesis state of the network.
//
// The ID of the new network is [networkID].

// FromConfig returns:
// 1) The byte representation of the genesis state of the platform chain
//    (ie the genesis state of the network)
// 2) The asset ID of AVAX
func FromConfig(config *Config) ([]byte, ids.ID, error) {
	hrp := constants.GetHRP(config.NetworkID)

	amount := uint64(0)

	// Specify the genesis state of the AVM
	avmArgs := avm.BuildGenesisArgs{
		NetworkID: json.Uint32(config.NetworkID),
	}
	{
		avax := avm.AssetDefinition{
			Name:         "Avalanche",
			Symbol:       "AVAX",
			Denomination: 9,
			InitialState: map[string][]interface{}{},
		}
		xAllocations := []Allocation(nil)
		for _, allocation := range config.Allocations {
			if allocation.InitialAmount > 0 {
				xAllocations = append(xAllocations, allocation)
			}
		}
		sortXAllocation(xAllocations)

		for _, allocation := range xAllocations {
			addr, err := formatting.FormatBech32(hrp, allocation.AVAXAddr.Bytes())
			if err != nil {
				return nil, ids.ID{}, err
			}

			avax.InitialState["fixedCap"] = append(avax.InitialState["fixedCap"], avm.Holder{
				Amount:  json.Uint64(allocation.InitialAmount),
				Address: addr,
			})
			avax.Memo.Bytes = append(avax.Memo.Bytes, allocation.ETHAddr.Bytes()...)
			amount += allocation.InitialAmount
		}

		avmArgs.GenesisData = map[string]avm.AssetDefinition{
			"AVAX": avax, // The AVM starts out with one asset: AVAX
		}
	}
	avmReply := avm.BuildGenesisReply{}

	avmSS := avm.StaticService{}
	err := avmSS.BuildGenesis(nil, &avmArgs, &avmReply)
	if err != nil {
		return nil, ids.ID{}, err
	}

	avaxAssetID, err := AVAXAssetID(avmReply.Bytes.Bytes)
	if err != nil {
		return nil, ids.ID{}, fmt.Errorf("couldn't generate AVAX asset ID: %w", err)
	}

	genesisTime := time.Unix(int64(config.StartTime), 0)
	initialSupply, err := config.InitialSupply()
	if err != nil {
		return nil, ids.ID{}, fmt.Errorf("couldn't calculate the initial supply: %w", err)
	}

	initiallyStaked := ids.ShortSet{}
	initiallyStaked.Add(config.InitialStakedFunds...)
	skippedAllocations := []Allocation(nil)

	// Specify the initial state of the Platform Chain
	platformvmArgs := platformvm.BuildGenesisArgs{
		AvaxAssetID:   avaxAssetID,
		NetworkID:     json.Uint32(config.NetworkID),
		Time:          json.Uint64(config.StartTime),
		InitialSupply: json.Uint64(initialSupply),
		Message:       config.Message,
	}
	for _, allocation := range config.Allocations {
		if initiallyStaked.Contains(allocation.AVAXAddr) {
			skippedAllocations = append(skippedAllocations, allocation)
			continue
		}
		addr, err := formatting.FormatBech32(hrp, allocation.AVAXAddr.Bytes())
		if err != nil {
			return nil, ids.ID{}, err
		}
		for _, unlock := range allocation.UnlockSchedule {
			if unlock.Amount > 0 {
				platformvmArgs.UTXOs = append(platformvmArgs.UTXOs,
					platformvm.APIUTXO{
						Locktime: json.Uint64(unlock.Locktime),
						Amount:   json.Uint64(unlock.Amount),
						Address:  addr,
						Message:  formatting.CB58{Bytes: allocation.ETHAddr.Bytes()},
					},
				)
				amount += unlock.Amount
			}
		}
	}

	allNodeAllocations := splitAllocations(skippedAllocations, len(config.InitialStakers))
	endStakingTime := genesisTime.Add(time.Duration(config.InitialStakeDuration) * time.Second)
	stakingOffset := time.Duration(0)
	for i, staker := range config.InitialStakers {
		nodeAllocations := allNodeAllocations[i]
		endStakingTime := endStakingTime.Add(-stakingOffset)
		stakingOffset += time.Duration(config.InitialStakeDurationOffset) * time.Second

		destAddrStr, err := formatting.FormatBech32(hrp, staker.RewardAddress.Bytes())
		if err != nil {
			return nil, ids.ID{}, err
		}

		utxos := []platformvm.APIUTXO(nil)
		for _, allocation := range nodeAllocations {
			addr, err := formatting.FormatBech32(hrp, allocation.AVAXAddr.Bytes())
			if err != nil {
				return nil, ids.ID{}, err
			}
			for _, unlock := range allocation.UnlockSchedule {
				utxos = append(utxos, platformvm.APIUTXO{
					Locktime: json.Uint64(unlock.Locktime),
					Amount:   json.Uint64(unlock.Amount),
					Address:  addr,
					Message:  formatting.CB58{Bytes: allocation.ETHAddr.Bytes()},
				})
				amount += unlock.Amount
			}
		}

		delegationFee := json.Uint32(staker.DelegationFee)

		platformvmArgs.Validators = append(platformvmArgs.Validators,
			platformvm.APIPrimaryValidator{
				APIStaker: platformvm.APIStaker{
					StartTime: json.Uint64(genesisTime.Unix()),
					EndTime:   json.Uint64(endStakingTime.Unix()),
					NodeID:    staker.NodeID.PrefixedString(constants.NodeIDPrefix),
				},
				RewardOwner: &platformvm.APIOwner{
					Threshold: 1,
					Addresses: []string{destAddrStr},
				},
				Staked:             utxos,
				ExactDelegationFee: &delegationFee,
			},
		)
	}

	// Specify the chains that exist upon this network's creation
	platformvmArgs.Chains = []platformvm.APIChain{
		{
			GenesisData: avmReply.Bytes,
			SubnetID:    constants.PrimaryNetworkID,
			VMID:        avm.ID,
			FxIDs: []ids.ID{
				secp256k1fx.ID,
				nftfx.ID,
				propertyfx.ID,
			},
			Name: "X-Chain",
		},
		{
			GenesisData: formatting.CB58{Bytes: []byte(config.CChainGenesis)},
			SubnetID:    constants.PrimaryNetworkID,
			VMID:        EVMID,
			Name:        "C-Chain",
		},
	}

	platformvmReply := platformvm.BuildGenesisReply{}
	platformvmSS := platformvm.StaticService{}
	if err := platformvmSS.BuildGenesis(nil, &platformvmArgs, &platformvmReply); err != nil {
		return nil, ids.ID{}, fmt.Errorf("problem while building platform chain's genesis state: %w", err)
	}

	return platformvmReply.Bytes.Bytes, avaxAssetID, nil
}

func splitAllocations(allocations []Allocation, numSplits int) [][]Allocation {
	totalAmount := uint64(0)
	for _, allocation := range allocations {
		for _, unlock := range allocation.UnlockSchedule {
			totalAmount += unlock.Amount
		}
	}

	nodeWeight := totalAmount / uint64(numSplits)
	allNodeAllocations := make([][]Allocation, 0, numSplits)

	currentNodeAllocation := []Allocation(nil)
	currentNodeAmount := uint64(0)
	for _, allocation := range allocations {
		currentAllocation := allocation
		// Already added to the X-chain
		currentAllocation.InitialAmount = 0
		// Going to be added until the correct amount is reached
		currentAllocation.UnlockSchedule = nil

		for _, unlock := range allocation.UnlockSchedule {
			unlock := unlock
			for currentNodeAmount+unlock.Amount > nodeWeight && len(allNodeAllocations) < numSplits-1 {
				amountToAdd := nodeWeight - currentNodeAmount
				currentAllocation.UnlockSchedule = append(currentAllocation.UnlockSchedule, LockedAmount{
					Amount:   amountToAdd,
					Locktime: unlock.Locktime,
				})
				unlock.Amount -= amountToAdd

				currentNodeAllocation = append(currentNodeAllocation, currentAllocation)

				allNodeAllocations = append(allNodeAllocations, currentNodeAllocation)

				currentNodeAllocation = nil
				currentNodeAmount = 0

				currentAllocation = allocation
				// Already added to the X-chain
				currentAllocation.InitialAmount = 0
				// Going to be added until the correct amount is reached
				currentAllocation.UnlockSchedule = nil
			}

			if unlock.Amount == 0 {
				continue
			}

			currentAllocation.UnlockSchedule = append(currentAllocation.UnlockSchedule, LockedAmount{
				Amount:   unlock.Amount,
				Locktime: unlock.Locktime,
			})
			currentNodeAmount += unlock.Amount
		}

		if len(currentAllocation.UnlockSchedule) > 0 {
			currentNodeAllocation = append(currentNodeAllocation, currentAllocation)
		}
	}

	return append(allNodeAllocations, currentNodeAllocation)
}

// Genesis returns:
// 1) The byte representation of the genesis state of the platform chain
//    (ie the genesis state of the network)
// 2) The asset ID of AVAX
func Genesis(networkID uint32) ([]byte, ids.ID, error) {
	return FromConfig(GetConfig(networkID))
}

// VMGenesis ...
func VMGenesis(networkID uint32, vmID ids.ID) (*platformvm.Tx, error) {
	genesisBytes, _, err := Genesis(networkID)
	if err != nil {
		return nil, err
	}
	genesis := platformvm.Genesis{}
	if err := platformvm.GenesisCodec.Unmarshal(genesisBytes, &genesis); err != nil {
		return nil, fmt.Errorf("couldn't unmarshal genesis bytes due to: %w", err)
	}
	if err := genesis.Initialize(); err != nil {
		return nil, err
	}
	for _, chain := range genesis.Chains {
		uChain := chain.UnsignedTx.(*platformvm.UnsignedCreateChainTx)
		if uChain.VMID.Equals(vmID) {
			return chain, nil
		}
	}
	return nil, fmt.Errorf("couldn't find blockchain with VM ID %s", vmID)
}

// AVAXAssetID ...
func AVAXAssetID(avmGenesisBytes []byte) (ids.ID, error) {
	c := codec.New(math.MaxUint32, 1<<20)
	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&avm.BaseTx{}),
		c.RegisterType(&avm.CreateAssetTx{}),
		c.RegisterType(&avm.OperationTx{}),
		c.RegisterType(&avm.ImportTx{}),
		c.RegisterType(&avm.ExportTx{}),
		c.RegisterType(&secp256k1fx.TransferInput{}),
		c.RegisterType(&secp256k1fx.MintOutput{}),
		c.RegisterType(&secp256k1fx.TransferOutput{}),
		c.RegisterType(&secp256k1fx.MintOperation{}),
		c.RegisterType(&secp256k1fx.Credential{}),
	)
	if errs.Errored() {
		return ids.ID{}, errs.Err
	}

	genesis := avm.Genesis{}
	if err := c.Unmarshal(avmGenesisBytes, &genesis); err != nil {
		return ids.ID{}, err
	}

	if len(genesis.Txs) == 0 {
		return ids.ID{}, errors.New("genesis creates no transactions")
	}
	genesisTx := genesis.Txs[0]

	tx := avm.Tx{UnsignedTx: &genesisTx.CreateAssetTx}
	unsignedBytes, err := c.Marshal(tx.UnsignedTx)
	if err != nil {
		return ids.ID{}, err
	}
	signedBytes, err := c.Marshal(&tx)
	if err != nil {
		return ids.ID{}, err
	}
	tx.Initialize(unsignedBytes, signedBytes)

	return tx.ID(), nil
}

type innerSortXAllocation []Allocation

func (xa innerSortXAllocation) Less(i, j int) bool {
	return xa[i].InitialAmount < xa[j].InitialAmount ||
		(xa[i].InitialAmount == xa[j].InitialAmount &&
			bytes.Compare(xa[i].AVAXAddr.Bytes(), xa[j].AVAXAddr.Bytes()) == -1)
}

func (xa innerSortXAllocation) Len() int      { return len(xa) }
func (xa innerSortXAllocation) Swap(i, j int) { xa[j], xa[i] = xa[i], xa[j] }

func sortXAllocation(a []Allocation) { sort.Sort(innerSortXAllocation(a)) }
