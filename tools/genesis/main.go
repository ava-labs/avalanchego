// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"time"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/chain4travel/camino-node/tools/genesis/workbook"
	"github.com/xuri/excelize/v2"
)

func main() {
	if len(os.Args) < 5 {
		usage := fmt.Sprintf("Usage: %s <workbook> <genesis_json> <network> <output_dir>", os.Args[0])
		log.Panic(usage)
	}

	spreadsheetFile := os.Args[1]
	genesisFile := os.Args[2]
	networkName := os.Args[3]
	outputPath := os.Args[4]

	// Allows to choose where additional unlocked funds (e.g. 1%) will be sent
	unlockedFunds := TransferToPChain

	networkID := uint32(0)
	switch networkName {
	case "camino":
		networkID = constants.CaminoID
	case "columbus":
		networkID = constants.ColumbusID
	case "kopernikus":
		networkID = constants.KopernikusID
	default:
		log.Panic("Need to provide a valid network name (camino|columbus|kopernikus)")
	}

	genesisConfig, err := readGenesisConfig(genesisFile)
	if err != nil {
		log.Panic("Could not open the genesis template file", err)
	}
	fmt.Println("Read genesis template with NetworkID", genesisConfig.NetworkID, " overwriting with ", networkID)
	genesisConfig.NetworkID = networkID

	fmt.Println("Loadingspreadsheet", spreadsheetFile)
	xls, err := excelize.OpenFile(spreadsheetFile)
	if err != nil {
		log.Panic("Could not open the file", err)
	}
	defer xls.Close()

	multiSigRows := workbook.ParseMultiSigGroups(xls)
	allocationRows := workbook.ParseAllocations(xls)
	depositOfferRows := workbook.ParseDepositOfferRows(xls)

	fmt.Println("Loaded multiSigRows groups", len(multiSigRows), "err", err)
	msigGroups, _ := generateMSigDefinitions(genesisConfig.NetworkID, multiSigRows)
	genesisConfig.Camino.InitialMultisigAddresses = msigGroups.MultisigAliaseas

	fmt.Println("Loaded allocationRows", len(allocationRows), "err", err)
	// Pick the max start offset to delay deposit offers end
	maxStartOffset := uint64(0)
	for _, allocation := range allocationRows {
		if allocation.TokenDeliveryOffset > maxStartOffset {
			maxStartOffset = allocation.TokenDeliveryOffset
		}
	}

	offersMap, depositOffers, err := generateDepositOffers(depositOfferRows, genesisConfig, maxStartOffset)
	if err != nil {
		log.Panic("Could not generate deposit offers", err)
	}
	genesisConfig.Camino.DepositOffers = depositOffers

	// create Genesis allocation records
	genAlloc, adminAddr := generateAllocations(genesisConfig.NetworkID, allocationRows, offersMap, msigGroups.ControlGroupToAlias, unlockedFunds)
	// Overwrite the admin addr if given
	if adminAddr != ids.ShortEmpty {
		avaxAddr, _ := address.Format(
			"X",
			constants.GetHRP(networkID),
			adminAddr.Bytes(),
		)
		genesisConfig.Camino.InitialAdmin = avaxAddr
		fmt.Println("InitialAdmin address set with:", avaxAddr)
	}
	genesisConfig.Camino.Allocations = genAlloc

	// saving the json file
	bytes, err := json.MarshalIndent(genesisConfig, "", "  ")
	if err != nil {
		fmt.Println("Could not marshal genesis config - error: ", err)
		return
	}

	outputFileName := fmt.Sprintf("%s/genesis_%s.json", outputPath, constants.NetworkIDToHRP[networkID])
	fmt.Println("Saving genesis to", outputFileName)
	err = os.WriteFile(outputFileName, bytes, 0o600)
	if err != nil {
		log.Panic("Could not write the output file: ", outputFileName, err)
	}

	fmt.Println("Sanity check the generated genesis file")
	if err := validateConfig(bytes); err != nil {
		log.Panic("Generated genesis file is invalid ", err)
	}
	fmt.Println("DONE")
}

func readGenesisConfig(genesisFile string) (genesis.UnparsedConfig, error) {
	genesisConfig := genesis.UnparsedConfig{}
	file, err := os.Open(genesisFile)
	if err != nil {
		log.Panic("unable to read genesis file", genesisFile, err)
	}
	fileBytes, _ := io.ReadAll(file)
	err = json.Unmarshal(fileBytes, &genesisConfig)
	if err != nil {
		log.Panic("error while parsing genesis json", err)
	}

	return genesisConfig, err
}

func validateConfig(jsonFileContent []byte) error {
	genesisConfig := genesis.UnparsedConfig{}
	err := json.Unmarshal(jsonFileContent, &genesisConfig)
	if err != nil {
		return fmt.Errorf("error while unserializing generated json, %w", err)
	}
	config, err := genesisConfig.Parse()
	if err != nil {
		return fmt.Errorf("error while parsing generated genesis json, %w", err)
	}

	return genesis.ValidateConfig(&config, &genesis.StakingConfig{
		MaxStakeDuration: time.Duration(math.MaxInt64),
	})
}
