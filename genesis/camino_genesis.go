// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var errEmptyAllocation = errors.New("allocation with zero value")

func validateCaminoConfig(config *Config) error {
	// validation initial admin address
	_, err := address.Format(
		configChainIDAlias,
		constants.GetHRP(config.NetworkID),
		config.Camino.InitialAdmin.Bytes(),
	)
	if err != nil {
		return fmt.Errorf(
			"unable to format address from %s",
			config.Camino.InitialAdmin.String(),
		)
	}

	// the rest of the checks are only for LockModeBondDeposit == true
	if !config.Camino.LockModeBondDeposit {
		return nil
	}

	if config.InitialStakeDuration != 0 {
		return errors.New("config.InitialStakeDuration != 0")
	}

	if len(config.InitialStakedFunds) != 0 {
		return errors.New("config.InitialStakedFunds != 0")
	}

	if len(config.InitialStakers) != 0 {
		return errors.New("config.InitialStakers != 0")
	}

	if len(config.Allocations) != 0 {
		return errors.New("config.Allocations != 0")
	}

	// validation deposit offers
	offers := make(map[ids.ID]genesis.DepositOffer, len(config.Camino.DepositOffers))
	for _, offer := range config.Camino.DepositOffers {
		if err := offer.Verify(); err != nil {
			return err
		}

		offerID, err := offer.ID()
		if err != nil {
			return err
		}

		if _, ok := offers[offerID]; ok {
			return errors.New("deposit offer duplicate")
		}
		offers[offerID] = offer
	}

	// validation allocations and stakers
	nodes := set.Set[ids.NodeID]{}
	allPlatformAllocations := map[ids.ShortID]set.Set[PlatformAllocation]{}
	for _, allocation := range config.Camino.Allocations {
		platformAllocations, ok := allPlatformAllocations[allocation.AVAXAddr]
		if !ok {
			platformAllocations = set.NewSet[PlatformAllocation](len(allocation.PlatformAllocations))
			allPlatformAllocations[allocation.AVAXAddr] = platformAllocations
		}
		for _, platformAllocation := range allocation.PlatformAllocations {
			if platformAllocations.Contains(platformAllocation) {
				addr, _ := address.Format(
					configChainIDAlias,
					constants.GetHRP(config.NetworkID),
					allocation.AVAXAddr[:],
				)
				return fmt.Errorf("platform allocation duplicate (%s)", addr)
			}
			platformAllocations.Add(platformAllocation)

			if allocation.AddressStates.ConsortiumMember && !allocation.AddressStates.KYCVerified {
				return errors.New("consortium member not kyc verified")
			}

			if platformAllocation.DepositOfferID != ids.Empty {
				if _, ok := offers[platformAllocation.DepositOfferID]; !ok {
					return errors.New("allocation deposit offer id doesn't match any offer")
				}
			}
			if platformAllocation.NodeID != ids.EmptyNodeID {
				if _, ok := nodes[platformAllocation.NodeID]; ok {
					return errors.New("repeated staker allocation")
				}
				nodes.Add(platformAllocation.NodeID)
				if !allocation.AddressStates.ConsortiumMember {
					return errors.New("staker ins't consortium member")
				}
			}
			if platformAllocation.NodeID != ids.EmptyNodeID &&
				platformAllocation.ValidatorDuration == 0 ||
				platformAllocation.NodeID == ids.EmptyNodeID &&
					platformAllocation.ValidatorDuration != 0 {
				return fmt.Errorf("wrong validator duration: %s %d",
					platformAllocation.NodeID, platformAllocation.ValidatorDuration)
			}
		}
	}

	// validate msig aliases
	for idx, msig := range config.Camino.InitialMultisigAddresses {
		rowTxID := ids.FromInt(uint64(idx))
		if err := msig.Verify(rowTxID); err != nil {
			return fmt.Errorf("wrong msig alias definition: %w", err)
		}
	}

	if nodes.Len() == 0 {
		return errors.New("no staker allocations")
	}

	return nil
}

func caminoArgFromConfig(config *Config) api.Camino {
	return api.Camino{
		VerifyNodeSignature:      config.Camino.VerifyNodeSignature,
		LockModeBondDeposit:      config.Camino.LockModeBondDeposit,
		InitialAdmin:             config.Camino.InitialAdmin,
		DepositOffers:            config.Camino.DepositOffers,
		InitialMultisigAddresses: config.Camino.InitialMultisigAddresses,
	}
}

func buildCaminoGenesis(config *Config, hrp string) ([]byte, ids.ID, error) {
	xGenesisBytes, avmReply, err := buildXGenesis(config, hrp)
	if err != nil {
		return nil, ids.Empty, err
	}

	return buildPGenesis(config, hrp, xGenesisBytes, avmReply)
}

func buildXGenesis(config *Config, hrp string) ([]byte, string, error) {
	amount := uint64(0)

	// Specify the genesis state of the AVM
	avmArgs := avm.BuildGenesisArgs{
		NetworkID: json.Uint32(config.NetworkID),
		Encoding:  defaultEncoding,
	}
	{
		avax := avm.AssetDefinition{
			Name:         constants.TokenName(config.NetworkID),
			Symbol:       constants.TokenSymbol(config.NetworkID),
			Denomination: 9,
			InitialState: map[string][]interface{}{},
		}
		memoBytes := []byte{}
		xAllocations := []CaminoAllocation(nil)
		for _, allocation := range config.Camino.Allocations {
			if allocation.XAmount > 0 {
				xAllocations = append(xAllocations, allocation)
			}
		}
		utils.Sort(xAllocations)

		for _, allocation := range xAllocations {
			addr, err := address.FormatBech32(hrp, allocation.AVAXAddr.Bytes())
			if err != nil {
				return nil, "", err
			}

			avax.InitialState["fixedCap"] = append(avax.InitialState["fixedCap"], avm.Holder{
				Amount:  json.Uint64(allocation.XAmount),
				Address: addr,
			})
			memoBytes = append(memoBytes, allocation.ETHAddr.Bytes()...)
			amount += allocation.XAmount
		}

		var err error
		avax.Memo, err = formatting.Encode(defaultEncoding, memoBytes)
		if err != nil {
			return nil, "", fmt.Errorf("couldn't parse memo bytes to string: %w", err)
		}
		avmArgs.GenesisData = map[string]avm.AssetDefinition{
			avax.Symbol: avax, // The AVM starts out with one asset
		}
	}
	avmReply := avm.BuildGenesisReply{}

	avmSS := avm.CreateStaticService()
	err := avmSS.BuildGenesis(nil, &avmArgs, &avmReply)
	if err != nil {
		return nil, "", err
	}

	genesisBytes, err := formatting.Decode(defaultEncoding, avmReply.Bytes)
	if err != nil {
		return nil, "", fmt.Errorf("couldn't parse avm genesis reply: %w", err)
	}

	return genesisBytes, avmReply.Bytes, nil
}

func buildPGenesis(config *Config, hrp string, xGenesisBytes []byte, xGenesisData string) ([]byte, ids.ID, error) {
	avaxAssetID, err := AVAXAssetID(xGenesisBytes)
	if err != nil {
		return nil, ids.ID{}, fmt.Errorf("couldn't generate AVAX asset ID: %w", err)
	}

	genesisTime := time.Unix(int64(config.StartTime), 0)
	initialSupply, err := config.InitialSupply()
	if err != nil {
		return nil, ids.Empty, fmt.Errorf("couldn't calculate the initial supply: %w", err)
	}

	// Specify the initial state of the Platform Chain
	platformvmArgs := api.BuildGenesisArgs{
		AvaxAssetID:   avaxAssetID,
		NetworkID:     json.Uint32(config.NetworkID),
		Time:          json.Uint64(config.StartTime),
		InitialSupply: json.Uint64(initialSupply),
		Message:       config.Message,
		Encoding:      defaultEncoding,
		Camino:        caminoArgFromConfig(config),
	}

	stakingOffset := time.Duration(0)

	for _, allocation := range config.Camino.Allocations {
		var addrState uint64
		if allocation.AddressStates.ConsortiumMember {
			addrState |= txs.AddressStateConsortiumBit
		}
		if allocation.AddressStates.KYCVerified {
			addrState |= txs.AddressStateKycVerifiedBit
		}
		if addrState != 0 {
			platformvmArgs.Camino.AddressStates = append(platformvmArgs.Camino.AddressStates, genesis.AddressState{
				Address: allocation.AVAXAddr,
				State:   addrState,
			})
		}

		for _, platformAllocation := range allocation.PlatformAllocations {
			if platformAllocation.Amount == 0 {
				return nil, ids.Empty, errEmptyAllocation
			}

			allocationAddress, err := address.FormatBech32(hrp, allocation.AVAXAddr.Bytes())
			if err != nil {
				return nil, ids.Empty, err
			}

			allocationMessage, err := formatting.Encode(defaultEncoding, allocation.ETHAddr.Bytes())
			if err != nil {
				return nil, ids.Empty, fmt.Errorf("couldn't encode message: %w", err)
			}

			utxo := api.UTXO{
				Amount:  json.Uint64(platformAllocation.Amount),
				Address: allocationAddress,
				Message: allocationMessage,
			}

			if platformAllocation.NodeID != ids.EmptyNodeID {
				stakingDuration := time.Duration(platformAllocation.ValidatorDuration) * time.Second
				endStakingTime := genesisTime.Add(stakingDuration).Add(-stakingOffset)
				stakingOffset += time.Duration(config.InitialStakeDurationOffset) * time.Second

				platformvmArgs.Validators = append(platformvmArgs.Validators,
					api.PermissionlessValidator{
						Staker: api.Staker{
							StartTime: json.Uint64(genesisTime.Unix()),
							EndTime:   json.Uint64(endStakingTime.Unix()),
							NodeID:    platformAllocation.NodeID,
						},
						RewardOwner: &api.Owner{
							Threshold: 1,
							Addresses: []string{allocationAddress},
						},
						Staked: []api.UTXO{utxo},
					},
				)
				platformvmArgs.Camino.ValidatorDeposits = append(platformvmArgs.Camino.ValidatorDeposits,
					[]api.UTXODeposit{{
						OfferID: platformAllocation.DepositOfferID,
						Memo:    platformAllocation.Memo,
					}})

				platformvmArgs.Camino.ValidatorConsortiumMembers = append(platformvmArgs.Camino.ValidatorConsortiumMembers, allocation.AVAXAddr)
			} else {
				platformvmArgs.Camino.UTXODeposits = append(platformvmArgs.Camino.UTXODeposits,
					api.UTXODeposit{
						OfferID: platformAllocation.DepositOfferID,
						Memo:    platformAllocation.Memo,
					})
				platformvmArgs.UTXOs = append(platformvmArgs.UTXOs, utxo)
			}
		}
	}

	// Specify the chains that exist upon this network's creation
	genesisStr, err := formatting.Encode(defaultEncoding, []byte(config.CChainGenesis))
	if err != nil {
		return nil, ids.Empty, fmt.Errorf("couldn't encode message: %w", err)
	}
	platformvmArgs.Chains = []api.Chain{
		{
			GenesisData: xGenesisData,
			SubnetID:    constants.PrimaryNetworkID,
			VMID:        constants.AVMID,
			FxIDs: []ids.ID{
				secp256k1fx.ID,
				nftfx.ID,
				propertyfx.ID,
			},
			Name: "X-Chain",
		},
		{
			GenesisData: genesisStr,
			SubnetID:    constants.PrimaryNetworkID,
			VMID:        constants.EVMID,
			Name:        "C-Chain",
		},
	}

	platformvmReply := api.BuildGenesisReply{}
	platformvmSS := api.StaticService{}
	if err := platformvmSS.BuildGenesis(nil, &platformvmArgs, &platformvmReply); err != nil {
		return nil, ids.Empty, fmt.Errorf("problem while building platform chain's genesis state: %w", err)
	}

	genesisBytes, err := formatting.Decode(platformvmReply.Encoding, platformvmReply.Bytes)
	if err != nil {
		return nil, ids.Empty, fmt.Errorf("problem parsing platformvm genesis bytes: %w", err)
	}

	return genesisBytes, avaxAssetID, nil
}
