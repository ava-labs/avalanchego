// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
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
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	pchaintxs "github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/types"
)

var (
	errEmptyAllocation    = errors.New("allocation with zero value")
	errNonExistingOffer   = errors.New("allocation deposit offer memo doesn't match any offer")
	errWrongMisgAliasAddr = errors.New("wrong msig alias addr")
)

// ValidateConfig validates the generated config. Exposed for camino-node/tools/genesis generator
// It's not used in caminogo itself. Please don't delete.
func ValidateConfig(config *Config, stakingCfg *StakingConfig) error {
	return validateConfig(config.NetworkID, config, stakingCfg)
}

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
	offers := make(map[string]*deposit.Offer, len(config.Camino.DepositOffers))
	offerIDs := set.NewSet[ids.ID](len(config.Camino.DepositOffers))
	for _, configOffer := range config.Camino.DepositOffers {
		offer, err := DepositOfferFromConfig(configOffer)
		if err != nil {
			return err
		}

		if err := offer.Verify(); err != nil {
			return err
		}

		if offerIDs.Contains(offer.ID) {
			return errors.New("deposit offer duplicate")
		}
		offerIDs.Add(offer.ID)

		if len(offer.Memo) == 0 {
			return errors.New("deposit offer has no memo")
		}

		if _, ok := offers[configOffer.Memo]; ok {
			return errors.New("genesis deposit offer memo duplicate")
		}
		offers[configOffer.Memo] = offer
	}

	// validation allocations and stakers
	nodes := set.Set[ids.NodeID]{}
	consortiumMembersWithNodes := set.Set[ids.ShortID]{}
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

			isDeposited := platformAllocation.DepositOfferMemo != ""
			if isDeposited {
				offer, ok := offers[platformAllocation.DepositOfferMemo]
				if !ok {
					return errNonExistingOffer
				}
				if platformAllocation.DepositDuration < uint64(offer.MinDuration) {
					return errors.New("allocation deposit duration is less than deposit offer min duration")
				}
				if platformAllocation.DepositDuration > uint64(offer.MaxDuration) {
					return errors.New("allocation deposit duration is more than deposit offer max duration")
				}

				sum, err := math.Add64(config.StartTime, platformAllocation.TimestampOffset)
				if err != nil {
					return err
				}
				depositStartTime := sum

				if depositStartTime < offer.Start {
					return errors.New("allocation deposit start time is less than deposit offer start time")
				}

				sum, err = math.Add64(depositStartTime, platformAllocation.DepositDuration)
				if err != nil {
					return err
				}
				depositEndTime := sum

				if depositEndTime > offer.End {
					return errors.New("allocation deposit end time is greater than deposit offer end time")
				}
			}

			if !isDeposited && platformAllocation.DepositDuration != 0 {
				return errors.New("allocation has non-zero deposit duration, while deposit offer memo isn't empty")
			}

			if platformAllocation.NodeID != ids.EmptyNodeID {
				if nodes.Contains(platformAllocation.NodeID) {
					return errors.New("repeated staker allocation")
				}
				nodes.Add(platformAllocation.NodeID)
				if !allocation.AddressStates.ConsortiumMember {
					return errors.New("staker ins't consortium member")
				}
				if consortiumMembersWithNodes.Contains(allocation.AVAXAddr) {
					return errors.New("consortium member has more, than one node")
				}
				consortiumMembersWithNodes.Add(allocation.AVAXAddr)
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
	txID := ids.Empty
	uniqAliases := set.NewSet[ids.ShortID](len(config.Camino.InitialMultisigAddresses))
	for _, configMsigAlias := range config.Camino.InitialMultisigAddresses {
		if expectedAlias := configMsigAlias.ComputeAlias(txID); configMsigAlias.Alias != expectedAlias {
			hrp := constants.GetHRP(config.NetworkID)
			expectedMsigAliasAddr, _ := address.Format(configChainIDAlias, hrp, expectedAlias.Bytes())
			msigAliasAddr, _ := address.Format(configChainIDAlias, hrp, configMsigAlias.Alias.Bytes())
			return fmt.Errorf("%w: expected %s, but got %s",
				errWrongMisgAliasAddr, expectedMsigAliasAddr, msigAliasAddr)
		}

		msigAlias, err := MultisigAliasFromConfig(configMsigAlias)
		if err != nil {
			return err
		}

		if err = msigAlias.Verify(); err != nil {
			return fmt.Errorf("wrong msig alias definition: %w", err)
		}

		if uniqAliases.Contains(configMsigAlias.Alias) {
			return fmt.Errorf("duplicated Multisig alias: %s (%s)", configMsigAlias.Alias.Hex(), configMsigAlias.Memo)
		}
		uniqAliases.Add(configMsigAlias.Alias)
	}

	if nodes.Len() == 0 {
		return errors.New("no staker allocations")
	}

	return nil
}

func caminoArgFromConfig(config *Config) api.Camino {
	return api.Camino{
		VerifyNodeSignature: config.Camino.VerifyNodeSignature,
		LockModeBondDeposit: config.Camino.LockModeBondDeposit,
		InitialAdmin:        config.Camino.InitialAdmin,
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

		avax.InitialState["fixedCap"] = []interface{}{}

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

	minValidatorStake := GetStakingConfig(config.NetworkID).MinValidatorStake
	maxValidatorStake := GetStakingConfig(config.NetworkID).MaxValidatorStake

	// Getting args from deposit offers, filling offer.Memo -> offerID map

	offerIDs := make(map[string]ids.ID, len(config.Camino.DepositOffers))
	platformvmArgs.Camino.DepositOffers = make([]*deposit.Offer, len(config.Camino.DepositOffers))
	for i := range config.Camino.DepositOffers {
		offer, err := DepositOfferFromConfig(config.Camino.DepositOffers[i])
		if err != nil {
			return nil, ids.Empty, err
		}

		offerIDs[config.Camino.DepositOffers[i].Memo] = offer.ID
		platformvmArgs.Camino.DepositOffers[i] = offer
	}

	// Getting args from multisig aliases

	platformvmArgs.Camino.MultisigAliases = make([]*multisig.Alias, len(config.Camino.InitialMultisigAddresses))
	for i := range config.Camino.InitialMultisigAddresses {
		multisigAlias, err := MultisigAliasFromConfig(config.Camino.InitialMultisigAddresses[i])
		if err != nil {
			return nil, ids.Empty, err
		}
		platformvmArgs.Camino.MultisigAliases[i] = multisigAlias
	}

	// Getting args from allocations

	for _, allocation := range config.Camino.Allocations {
		var addrState pchaintxs.AddressState
		if allocation.AddressStates.ConsortiumMember {
			addrState |= pchaintxs.AddressStateConsortiumMember
		}
		if allocation.AddressStates.KYCVerified {
			addrState |= pchaintxs.AddressStateKYCVerified
		}
		if addrState != 0 {
			platformvmArgs.Camino.AddressStates = append(platformvmArgs.Camino.AddressStates, genesis.AddressState{
				Address: allocation.AVAXAddr,
				State:   addrState,
			})
		}

		allocationAddress, err := address.FormatBech32(hrp, allocation.AVAXAddr.Bytes())
		if err != nil {
			return nil, ids.Empty, err
		}

		stakeRemaining := maxValidatorStake
		for _, platformAllocation := range allocation.PlatformAllocations {
			if platformAllocation.Amount == 0 {
				return nil, ids.Empty, errEmptyAllocation
			}

			allocationMessage, err := formatting.Encode(defaultEncoding, []byte(platformAllocation.Memo))
			if err != nil {
				return nil, ids.Empty, fmt.Errorf("couldn't encode message: %w", err)
			}

			amountRemaining := platformAllocation.Amount
			utxo := api.UTXO{
				Address: allocationAddress,
				Message: allocationMessage,
			}

			depositOfferID := ids.Empty
			if platformAllocation.DepositOfferMemo != "" {
				offerID, ok := offerIDs[platformAllocation.DepositOfferMemo]
				if !ok {
					return nil, ids.Empty, errNonExistingOffer
				}
				depositOfferID = offerID
			}

			if platformAllocation.NodeID != ids.EmptyNodeID && stakeRemaining > 0 {
				// Never allocate more than Max
				if amountRemaining > stakeRemaining {
					utxo.Amount = json.Uint64(stakeRemaining)
				} else {
					utxo.Amount = json.Uint64(amountRemaining)
				}

				amountRemaining -= uint64(utxo.Amount)
				stakeRemaining -= uint64(utxo.Amount)

				if uint64(utxo.Amount) < minValidatorStake {
					return nil, ids.Empty, fmt.Errorf("not enough validator stake (%d)", utxo.Amount)
				}

				stakingDuration := time.Duration(platformAllocation.ValidatorDuration) * time.Second
				startStakingTime := genesisTime.Add(time.Duration(platformAllocation.TimestampOffset) * time.Second)
				endStakingTime := genesisTime.Add(stakingDuration).Add(-stakingOffset)
				stakingOffset += time.Duration(config.InitialStakeDurationOffset) * time.Second

				platformvmArgs.Validators = append(platformvmArgs.Validators,
					api.PermissionlessValidator{
						Staker: api.Staker{
							StartTime: json.Uint64(startStakingTime.Unix()),
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
						OfferID:         depositOfferID,
						Duration:        platformAllocation.DepositDuration,
						TimestampOffset: platformAllocation.TimestampOffset,
						Memo:            platformAllocation.Memo,
					}})

				platformvmArgs.Camino.ValidatorConsortiumMembers = append(platformvmArgs.Camino.ValidatorConsortiumMembers, allocation.AVAXAddr)
			}
			if amountRemaining > 0 {
				utxo.Amount = json.Uint64(amountRemaining)
				platformvmArgs.Camino.UTXODeposits = append(platformvmArgs.Camino.UTXODeposits,
					api.UTXODeposit{
						OfferID:         depositOfferID,
						Duration:        platformAllocation.DepositDuration,
						TimestampOffset: platformAllocation.TimestampOffset,
						Memo:            platformAllocation.Memo,
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

	// Building genesis

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

func GenesisChainData(genesisBytes []byte, vmIDs []ids.ID) ([]*pchaintxs.Tx, bool, error) {
	genesis, err := genesis.Parse(genesisBytes)
	if err != nil {
		return nil, false, fmt.Errorf("failed to parse genesis: %w", err)
	}
	result := make([]*pchaintxs.Tx, len(vmIDs))
	for idx, vmID := range vmIDs {
		for _, chain := range genesis.Chains {
			uChain := chain.Unsigned.(*pchaintxs.CreateChainTx)
			if uChain.VMID == vmID {
				result[idx] = chain
				break
			}
		}
		if result[idx] == nil {
			return nil, false, fmt.Errorf("couldn't find blockchain with VM ID %s", vmID)
		}
	}
	return result, genesis.Camino.LockModeBondDeposit, nil
}

func GetGenesisBlocksIDs(genesisBytes []byte, genesis *genesis.Genesis) ([]ids.ID, error) {
	genesisID := hashing.ComputeHash256Array(genesisBytes)
	zeroBlock, err := blocks.NewApricotCommitBlock(genesisID, 0 /*height*/)
	if err != nil {
		return nil, err
	}

	parentID := zeroBlock.ID()
	blockIDs := make([]ids.ID, len(genesis.Camino.Blocks))

	for i, block := range genesis.Camino.Blocks {
		genesisBlock, err := blocks.NewBanffStandardBlock(
			block.Time(),
			parentID,
			uint64(i)+1,
			block.Txs(),
		)
		if err != nil {
			return nil, err
		}
		parentID = genesisBlock.ID()
		blockIDs[i] = parentID
	}

	return blockIDs, nil
}

func DepositOfferFromConfig(configDepositOffer DepositOffer) (*deposit.Offer, error) {
	offer := &deposit.Offer{
		InterestRateNominator:   configDepositOffer.InterestRateNominator,
		Start:                   configDepositOffer.Start,
		End:                     configDepositOffer.End,
		MinAmount:               configDepositOffer.MinAmount,
		MinDuration:             configDepositOffer.MinDuration,
		MaxDuration:             configDepositOffer.MaxDuration,
		UnlockPeriodDuration:    configDepositOffer.UnlockPeriodDuration,
		NoRewardsPeriodDuration: configDepositOffer.NoRewardsPeriodDuration,
		Memo:                    types.JSONByteSlice(configDepositOffer.Memo),
		Flags:                   configDepositOffer.Flags,
	}
	if err := genesis.SetDepositOfferID(offer); err != nil {
		return nil, err
	}
	return offer, nil
}

func MultisigAliasFromConfig(configMsigAlias MultisigAlias) (*multisig.Alias, error) {
	return &multisig.Alias{
		Owners: &secp256k1fx.OutputOwners{
			Threshold: configMsigAlias.Threshold,
			Addrs:     configMsigAlias.Addresses,
		},
		Memo: types.JSONByteSlice(configMsigAlias.Memo),
		ID:   configMsigAlias.Alias,
	}, nil
}
