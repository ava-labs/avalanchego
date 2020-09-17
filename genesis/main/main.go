// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"math/big"
	"os"
	"strconv"
	"strings"

	"github.com/ava-labs/go-ethereum/common"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
)

var (
	cChainGenesis = `{"config":{"chainId":43110,"homesteadBlock":0,"daoForkBlock":0,"daoForkSupport":true,"eip150Block":0,"eip150Hash":"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0","eip155Block":0,"eip158Block":0,"byzantiumBlock":0,"constantinopleBlock":0,"petersburgBlock":0},"nonce":"0x0","timestamp":"0x0","extraData":"0x00","gasLimit":"0x5f5e100","difficulty":"0x0","mixHash":"0x0000000000000000000000000000000000000000000000000000000000000000","coinbase":"0x0000000000000000000000000000000000000000","alloc":{"0100000000000000000000000000000000000000":{"code":"0x730000000000000000000000000000000000000000301460806040526004361061004b5760003560e01c80631e01043914610050578063abb24ba014610092578063b6510bb3146100a9575b600080fd5b61007c6004803603602081101561006657600080fd5b8101908080359060200190929190505050610118565b6040518082815260200191505060405180910390f35b81801561009e57600080fd5b506100a761013b565b005b8180156100b557600080fd5b50610116600480360360808110156100cc57600080fd5b81019080803573ffffffffffffffffffffffffffffffffffffffff16906020019092919080359060200190929190803590602001909291908035906020019092919050505061013e565b005b60003073ffffffffffffffffffffffffffffffffffffffff1682905d9050919050565b5c565b8373ffffffffffffffffffffffffffffffffffffffff1681836108fc8690811502906040516000604051808303818888878c8af69550505050505015801561018a573d6000803e3d6000fd5b505050505056fea2646970667358221220ed2100d6623a884d196eceefabe5e03da4309a2562bb25262f3874f1acb31cd764736f6c634300060a0033","balance":"0x0"}},"number":"0x0","gasUsed":"0x0","parentHash":"0x0000000000000000000000000000000000000000000000000000000000000000"}`
)

func main() {
	uc := makeAvaxGenesis()
	c, err := uc.Parse()
	if err != nil {
		log.Fatal(err)
	}

	genesisBytes, _, err := genesis.FromConfig(&c)
	if err != nil {
		log.Fatal(err)
	}

	hardCodedGenesisBytes, _, err := genesis.FromConfig(&genesis.ManhattenConfig)
	if err != nil {
		log.Fatal(err)
	}

	if !bytes.Equal(genesisBytes, hardCodedGenesisBytes) {
		log.Fatal("different bytes", len(genesisBytes), len(hardCodedGenesisBytes))
	}
}

func makeAvaxGenesis() *genesis.UnparsedConfig {
	c := &genesis.UnparsedConfig{
		NetworkID:                  constants.ManhattenID,
		StartTime:                  math.MaxUint64,
		InitialStakeDuration:       math.MaxUint64,
		InitialStakeDurationOffset: uint64(90 * 60),
		CChainGenesis:              cChainGenesis,
		Message:                    "Behind the Vast Market Rally: A Tumbling Dollar.",
	}

	{
		file, err := os.Open("./final_gv.out")
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			ethAddr, avaxAddr, initialUnlock, unlockSchedule, err := parseLine(line)
			if err != nil {
				log.Fatal(line, err)
			}
			c.Allocations = append(c.Allocations, genesis.UnparsedAllocation{
				ETHAddr:        ethAddr,
				AVAXAddr:       avaxAddr,
				InitialAmount:  initialUnlock.Amount,
				UnlockSchedule: unlockSchedule,
			})
			if c.StartTime > initialUnlock.Locktime {
				c.StartTime = initialUnlock.Locktime
			}
			if len(unlockSchedule) > 0 && unlockSchedule[0].Locktime < c.InitialStakeDuration {
				c.InitialStakeDuration = unlockSchedule[0].Locktime
			}
		}
		c.InitialStakeDuration -= c.StartTime
		if err := scanner.Err(); err != nil {
			log.Fatal(err)
		}
	}

	{
		file, err := os.Open("./staked_addresses.txt")
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			c.InitialStakeAddresses = append(c.InitialStakeAddresses, line)
		}
		if err := scanner.Err(); err != nil {
			log.Fatal(err)
		}
	}

	{
		file, err := os.Open("./staked_node_ids.txt")
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			c.InitialStakeNodeIDs = append(c.InitialStakeNodeIDs, line)
		}
		if err := scanner.Err(); err != nil {
			log.Fatal(err)
		}
	}

	{
		file, err := os.Open("./stake_reward_addresses.txt")
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			c.InitialStakeRewardAddresses = append(c.InitialStakeRewardAddresses, line)
		}
		if err := scanner.Err(); err != nil {
			log.Fatal(err)
		}
	}

	configJSON, err := json.MarshalIndent(c, "", "    ")
	if err != nil {
		log.Fatal(err)
	}

	if err := ioutil.WriteFile("./final_gv.json", configJSON, 0644); err != nil {
		log.Fatal(err)
	}

	compactConfigJSON, err := json.Marshal(c)
	if err != nil {
		log.Fatal(err)
	}

	if err := ioutil.WriteFile("./final_gv_compact.json", compactConfigJSON, 0644); err != nil {
		log.Fatal(err)
	}
	return c
}

func makeEthGenesis() {
	// Specify the genesis state of Athereum (the built-in instance of the EVM)
	alloc := core.GenesisAlloc{}
	alloc[common.HexToAddress("0100000000000000000000000000000000000000")] = core.GenesisAccount{
		Code: common.Hex2Bytes("730000000000000000000000000000000000000000301460806040526004361061004b5760003560e01c80631e01043914610050578063abb24ba014610092578063b6510bb3146100a9575b600080fd5b61007c6004803603602081101561006657600080fd5b8101908080359060200190929190505050610118565b6040518082815260200191505060405180910390f35b81801561009e57600080fd5b506100a761013b565b005b8180156100b557600080fd5b50610116600480360360808110156100cc57600080fd5b81019080803573ffffffffffffffffffffffffffffffffffffffff16906020019092919080359060200190929190803590602001909291908035906020019092919050505061013e565b005b60003073ffffffffffffffffffffffffffffffffffffffff1682905d9050919050565b5c565b8373ffffffffffffffffffffffffffffffffffffffff1681836108fc8690811502906040516000604051808303818888878c8af69550505050505015801561018a573d6000803e3d6000fd5b505050505056fea2646970667358221220ed2100d6623a884d196eceefabe5e03da4309a2562bb25262f3874f1acb31cd764736f6c634300060a0033"),
	}
	evmArgs := core.Genesis{
		Config: &params.ChainConfig{
			ChainID:             big.NewInt(43110),
			HomesteadBlock:      big.NewInt(0),
			DAOForkBlock:        big.NewInt(0),
			DAOForkSupport:      true,
			EIP150Block:         big.NewInt(0),
			EIP150Hash:          common.HexToHash("0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0"),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(0),
			PetersburgBlock:     big.NewInt(0),
		},
		Nonce:      0,
		Timestamp:  0,
		ExtraData:  []byte{0},
		GasLimit:   100000000,
		Difficulty: big.NewInt(0),
		Mixhash:    common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
		Coinbase:   common.HexToAddress("0x0000000000000000000000000000000000000000"),
		Alloc:      alloc,
		Number:     0,
		GasUsed:    0,
		ParentHash: common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
	}
	evmSS := evm.StaticService{}
	evmReply, err := evmSS.BuildGenesis(nil, &evmArgs)
	if err != nil {
		panic(err)
	}

	if err := ioutil.WriteFile("./eth_genesis.txt", evmReply.Bytes, 0644); err != nil {
		log.Fatal(err)
	}
}

func parseLine(line string) (string, string, genesis.LockedAmount, []genesis.LockedAmount, error) {
	triple := strings.SplitN(line, "', ", 3)
	ethAddrString := triple[0][2:]
	avaxAddrString := triple[1][1:]

	unlockScheduleString := triple[2][2 : len(triple[2])-3]
	unlockScheduleStringArray := strings.Split(unlockScheduleString, "), (")
	unlockSchedule := make([]genesis.LockedAmount, 0, len(unlockScheduleStringArray))
	for _, periodString := range unlockScheduleStringArray {
		periodStringArray := strings.Split(periodString, ", ")
		amount, ok := new(big.Float).SetString(periodStringArray[0])
		if !ok {
			return "", "", genesis.LockedAmount{}, nil, fmt.Errorf("invalid float: %s", periodStringArray[0])
		}
		locktime, err := strconv.ParseUint(periodStringArray[1], 10, 64)
		if err != nil {
			return "", "", genesis.LockedAmount{}, nil, err
		}

		rawAmount, precision := new(big.Float).Mul(amount, new(big.Float).SetUint64(units.Avax)).Uint64()
		if precision != big.Exact {
			return "", "", genesis.LockedAmount{}, nil, fmt.Errorf("non-specific amount provided: %s", periodStringArray[0])
		}
		unlockSchedule = append(unlockSchedule, genesis.LockedAmount{
			Amount:   rawAmount,
			Locktime: locktime,
		})
	}
	initialAllocation := unlockSchedule[0]
	filteredAllocations := []genesis.LockedAmount{}
	for _, allocation := range unlockSchedule[1:] {
		if allocation.Amount > 0 {
			filteredAllocations = append(filteredAllocations, allocation)
		}
	}
	return ethAddrString, avaxAddrString, initialAllocation, filteredAllocations, nil
}
