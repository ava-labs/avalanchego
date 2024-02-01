// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"strings"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/chain4travel/camino-node/tools/genesis/workbook"
)

var EmptyETHAddress = "0x" + hex.EncodeToString(ids.ShortEmpty.Bytes())

func generateDepositOffers(depositOffersRows workbook.DepositOffersWithOrder, genesisConfig genesis.UnparsedConfig, maxStartOffset uint64) (
	workbook.DepositOffersWithOrder, []genesis.UnparsedDepositOffer, error,
) {
	// Set ID on DepositOffers
	depositOffers := []genesis.UnparsedDepositOffer{}

	for _, offerID := range depositOffersRows.Order {
		offer := depositOffersRows.Offers[offerID]
		parsedOffer, err := offer.Parse(genesisConfig.StartTime)
		if err != nil {
			return depositOffersRows, depositOffers, fmt.Errorf("error parsing offer %s: %w", offerID, err)
		}
		parsedOffer.End += maxStartOffset
		fmt.Println("DepositOffer  ", offerID, "\t Memo:", parsedOffer.Memo)

		depositOffer, err := parsedOffer.Unparse(genesisConfig.StartTime)
		if err != nil {
			return depositOffersRows, depositOffers, fmt.Errorf("error unparsing offer %s after modifications: %w", offerID, err)
		}
		depositOffersRows.Offers[offerID] = &depositOffer
		depositOffers = append(depositOffers, depositOffer)
	}
	return depositOffersRows, depositOffers, nil
}

func generateMSigDefinitions(networkID uint32, msigs []*workbook.MultiSigGroup) (MultisigDefs, error) {
	var (
		msDefs   = []genesis.MultisigAlias{}
		cgToMSig = map[string]ids.ShortID{}
	)

	txID := ids.Empty
	for _, ms := range msigs {
		// Note: only control_group makes an alias, I'm ignoring unlikely possible hashing collisions
		memo := ms.ControlGroup
		ma, err := newMultisigAlias(txID, ms.Addrs, ms.Threshold, memo)
		if err != nil {
			log.Panic("Could not create multisig definition for ", ms.ControlGroup, " error: ", err)
		}

		msDefs = append(msDefs, ma)
		cgToMSig[memo] = ma.Alias

		fmt.Println("MSig alias generated ", memo, "  Addr:", addrToString(networkID, ma.Alias))
	}

	defs := MultisigDefs{
		ControlGroupToAlias: cgToMSig,
		MultisigAliaseas:    make([]genesis.UnparsedMultisigAlias, 0, len(msDefs)),
	}

	strAliases := map[ids.ShortID]genesis.UnparsedMultisigAlias{}
	for _, ali := range msDefs {
		uma, err := ali.Unparse(networkID)
		if err != nil {
			fmt.Println("Could not unparse multisig definition for ", ali.Alias, err)
		}
		strAliases[ali.Alias] = uma
		defs.MultisigAliaseas = append(defs.MultisigAliaseas, uma)
	}

	return defs, nil
}

type UnlockedFunds int

// TransferToPChain for now is the default. At some point we want to have a choice.
const (
	TransferToPChain UnlockedFunds = iota
	// TransferToXChain
)

func generateMemo(lineNumber uint32, bucketName string, isDeposit bool, isIncentive bool) string {
	// The memo should look like this:
	// ID: <line>, Bucket: <bucket>, Type: <type> <type> <...>
	// Examples:
	// ID: 276, Bucket: 14 Presale
	// ID: 276, Bucket: 14 Presale, Types: unlocked incentive

	memo := fmt.Sprintf("ID: %d, Bucket: %s", lineNumber, bucketName)

	typeStrings := make([]string, 0, 2)
	if !isDeposit {
		typeStrings = append(typeStrings, "unlocked")
	}

	if isIncentive {
		typeStrings = append(typeStrings, "incentive")
	}

	if len(typeStrings) > 0 {
		memo += fmt.Sprintf(", Type: %s", strings.Join(typeStrings, " "))
	}

	return memo
}

func generateAllocations(
	networkID uint32,
	allocations []*workbook.AllocationRow,
	offersMap workbook.DepositOffersWithOrder,
	msigCtrlGrpToAlias map[string]ids.ShortID,
	unlockedFundsDestination UnlockedFunds,
) ([]genesis.UnparsedCaminoAllocation, ids.ShortID) {
	unparsedAlloc := make([]genesis.UnparsedCaminoAllocation, 0, len(allocations))
	skippedRows := 0
	adminAddr := ids.ShortEmpty
	for _, al := range allocations {
		msigAlias, hasAlias := msigCtrlGrpToAlias[al.ControlGroup]

		if hasAlias {
			al.Address = msigAlias
			fmt.Printf("replaced row %3d address with its control group alias %s\n", al.RowNo, al.ControlGroup)
		}

		// print addresses generated from public keys
		if !hasAlias && al.PublicKey != "" {
			fmt.Printf("replaced row %3d public key %s resolved to address %s\n", al.RowNo, al.PublicKey[:11], addrToString(networkID, al.Address))
		}

		// early exits
		if al.Address == ids.ShortEmpty {
			fmt.Println("\033[31mSkipping Row # ", al.RowNo, " Reason: Address Empty\033[0m")
			skippedRows++
			continue
		}

		if al.FirstName == "ADMIN" {
			adminAddr = al.Address
		}

		if al.Amount == 0 {
			fmt.Println("\033[31mSkipping Row # ", al.RowNo, " Reason: No allocation amount given\033[0m")
			skippedRows++
			continue
		}

		offer, hasOffer := offersMap.Offers[al.OfferID]
		if al.OfferID != "" && !hasOffer {
			log.Panic("Error row ", al.RowNo, " specified offer id cannot be found: ", al.OfferID)
		}

		directAmount := uint64(0)
		if !hasOffer {
			directAmount = al.Amount
		}

		onePercent := uint64(0)
		if al.Additional1Percent == "y" {
			onePercent = al.Amount / 100
		}

		isConsortiumMember := al.ConsortiumMember == workbook.CheckedValue
		isKycVerified := al.Kyc == workbook.YesValue

		a := genesis.UnparsedCaminoAllocation{
			ETHAddr:       EmptyETHAddress,
			AVAXAddr:      addrToString(networkID, al.Address),
			AddressStates: genesis.AddressStates{ConsortiumMember: isConsortiumMember, KYCVerified: isKycVerified},
		}

		if hasOffer {
			duration := offer.MinDuration
			if offer.MinDuration <= al.DepositDuration && al.DepositDuration <= offer.MaxDuration {
				duration = al.DepositDuration
			} else if al.DepositDuration > 0 {
				fmt.Printf("Error row %3d: Wrong duration set on allocation deposit duration %d is outside of offer's range [%d, %d]. OfferID %s.\n", al.RowNo, al.DepositDuration, offer.MinDuration, offer.MaxDuration, al.OfferID)
			}
			pa := genesis.UnparsedPlatformAllocation{
				Amount:            al.Amount,
				DepositOfferMemo:  offer.Memo,
				DepositDuration:   uint64(duration),
				NodeID:            nodeIDToString(al.NodeID),
				ValidatorDuration: uint64(al.ValidatorPeriodDays * 24 * 60 * 60),
				TimestampOffset:   al.TokenDeliveryOffset,
				Memo:              generateMemo(uint32(al.RowNo), al.Bucket, true, false),
			}
			a.PlatformAllocations = append(a.PlatformAllocations, pa)
		}

		unlockedFunds := directAmount + onePercent
		if unlockedFunds > 0 && unlockedFundsDestination == TransferToPChain {
			additionalUnlocked := genesis.UnparsedPlatformAllocation{
				Amount:          unlockedFunds,
				TimestampOffset: al.TokenDeliveryOffset,
				Memo:            generateMemo(uint32(al.RowNo), al.Bucket, false, onePercent > 0),
			}
			a.PlatformAllocations = append(a.PlatformAllocations, additionalUnlocked)
		} else {
			a.XAmount = unlockedFunds
		}

		unparsedAlloc = append(unparsedAlloc, a)
	}

	return unparsedAlloc, adminAddr
}

func addrToString(networkID uint32, addr ids.ShortID) string {
	fmtAddr, _ := address.Format("X", constants.NetworkIDToHRP[networkID], addr.Bytes())
	return fmtAddr
}

func nodeIDToString(id ids.NodeID) string {
	if ids.ShortID(id) != ids.ShortEmpty {
		return id.String()
	}
	return ""
}

type MultisigDefs struct {
	ControlGroupToAlias map[string]ids.ShortID
	MultisigAliaseas    []genesis.UnparsedMultisigAlias
}

func newMultisigAlias(txID ids.ID, addrs []ids.ShortID, threshold uint32, memo string) (genesis.MultisigAlias, error) {
	utils.Sort(addrs)
	ma := genesis.MultisigAlias{
		Threshold: threshold,
		Addresses: addrs,
		Memo:      memo,
	}
	ma.Alias = ma.ComputeAlias(txID)
	return ma, msigSanityCheck(ma, txID)
}

func msigSanityCheck(ma genesis.MultisigAlias, txID ids.ID) error {
	// Sanity check: Unparse & Parse & Verify
	uma, err := ma.Unparse(1)
	if err != nil {
		return err
	}
	mm, err := uma.Parse()
	if err != nil {
		return err
	}

	if ma.Alias != mm.ComputeAlias(txID) {
		return fmt.Errorf("alias mismatch between original and recreated one")
	}

	internalAlias, err := genesis.MultisigAliasFromConfig(mm)
	if err != nil {
		return err
	}

	return internalAlias.Verify()
}
