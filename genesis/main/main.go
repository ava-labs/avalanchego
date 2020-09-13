// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"math/big"
	"os"
	"strconv"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm"
	"github.com/ava-labs/go-ethereum/common"

	safemath "github.com/ava-labs/avalanche-go/utils/math"
)

// LockedAmount ...
type LockedAmount struct {
	Amount   uint64 `json:"amount"`
	Locktime uint64 `json:"locktime"`
}

// Allocation ...
type Allocation struct {
	ETHAddr        ids.ShortID    `json:"ethAddr"`
	AVAXAddr       ids.ShortID    `json:"avaxAddr"`
	InitialAmount  uint64         `json:"initialAmount"`
	UnlockSchedule []LockedAmount `json:"unlockSchedule"`
}

// Config contains the genesis addresses used to construct a genesis
type Config struct {
	NetworkID uint32 `json:"networkID"`

	Allocations []Allocation `json:"allocations"`

	StartTime             uint64        `json:"startTime"`
	InitialStakeAmount    uint64        `json:"initialStakeAmount"`
	InitialStakeDuration  uint64        `json:"initialStakeDuration"`
	InitialStakeAddresses []ids.ShortID `json:"initialStakeAddresses"`
	InitialStakeNodeIDs   []ids.ShortID `json:"initialStakeNodeIDs"`

	CChainGenesis []byte `json:"cChainGenesis"`

	Message string `json:"message"`
}

// InitialSupply ...
func (c *Config) InitialSupply() (uint64, error) {
	initialSupply := uint64(0)
	for _, allocation := range c.Allocations {
		newInitialSupply, err := safemath.Add64(initialSupply, allocation.InitialAmount)
		if err != nil {
			return 0, err
		}
		for _, unlock := range allocation.UnlockSchedule {
			newInitialSupply, err = safemath.Add64(newInitialSupply, unlock.Amount)
			if err != nil {
				return 0, err
			}
		}
		initialSupply = newInitialSupply
	}
	return initialSupply, nil
}

func main() {
	file, err := os.Open("./final_gv.out")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	c := &Config{
		NetworkID:            constants.MainnetID,
		StartTime:            math.MaxUint64,
		InitialStakeAmount:   100 * units.MegaAvax, // TODO: Needs to be set correctly
		InitialStakeDuration: math.MaxUint64,
		Message:              "Behind the Vast Market Rally: A Tumbling Dollar.",
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		ethAddr, avaxAddr, initialUnlock, unlockSchedule, err := parseLine(line)
		if err != nil {
			log.Fatal(line, err)
		}
		c.Allocations = append(c.Allocations, Allocation{
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

	configString, err := json.MarshalIndent(c, "", "    ")
	if err != nil {
		log.Fatal(err)
	}

	if err := ioutil.WriteFile("./final_gv.json", configString, 0644); err != nil {
		log.Fatal(err)
	}

	initialSupply, err := c.InitialSupply()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(360*units.MegaAvax - initialSupply)
}

func makeEthGenesis() {
	// Specify the genesis state of Athereum (the built-in instance of the EVM)
	alloc := core.GenesisAlloc{}
	alloc[common.HexToAddress("0100000000000000000000000000000000000000")] = core.GenesisAccount{
		Code: common.Hex2Bytes("0x730000000000000000000000000000000000000000301460806040526004361061004b5760003560e01c80631e01043914610050578063abb24ba014610092578063b6510bb3146100a9575b600080fd5b61007c6004803603602081101561006657600080fd5b8101908080359060200190929190505050610118565b6040518082815260200191505060405180910390f35b81801561009e57600080fd5b506100a761013b565b005b8180156100b557600080fd5b50610116600480360360808110156100cc57600080fd5b81019080803573ffffffffffffffffffffffffffffffffffffffff16906020019092919080359060200190929190803590602001909291908035906020019092919050505061013e565b005b60003073ffffffffffffffffffffffffffffffffffffffff1682905d9050919050565b5c565b8373ffffffffffffffffffffffffffffffffffffffff1681836108fc8690811502906040516000604051808303818888878c8af69550505050505015801561018a573d6000803e3d6000fd5b505050505056fea2646970667358221220ed2100d6623a884d196eceefabe5e03da4309a2562bb25262f3874f1acb31cd764736f6c634300060a0033"),
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

	if err := ioutil.WriteFile("./eth_genesis.txt", []byte(hex.Dump(evmReply.Bytes)), 0644); err != nil {
		log.Fatal(err)
	}
}

func parseLine(line string) (ids.ShortID, ids.ShortID, LockedAmount, []LockedAmount, error) {
	triple := strings.SplitN(line, "', ", 3)
	ethAddrString := triple[0][4:]
	ethAddrBytes, err := hex.DecodeString(ethAddrString)
	if err != nil {
		return ids.ShortID{}, ids.ShortID{}, LockedAmount{}, nil, err
	}
	ethAddr, err := ids.ToShortID(ethAddrBytes)
	if err != nil {
		return ids.ShortID{}, ids.ShortID{}, LockedAmount{}, nil, err
	}

	avaxAddrString := triple[1][1:]
	_, _, avaxAddrBytes, err := formatting.ParseAddress(avaxAddrString)
	if err != nil {
		return ids.ShortID{}, ids.ShortID{}, LockedAmount{}, nil, err
	}
	avaxAddr, err := ids.ToShortID(avaxAddrBytes)
	if err != nil {
		return ids.ShortID{}, ids.ShortID{}, LockedAmount{}, nil, err
	}

	unlockScheduleString := triple[2][2 : len(triple[2])-3]
	unlockScheduleStringArray := strings.Split(unlockScheduleString, "), (")
	unlockSchedule := make([]LockedAmount, len(unlockScheduleStringArray))
	for i, periodString := range unlockScheduleStringArray {
		periodStringArray := strings.Split(periodString, ", ")
		amount, ok := new(big.Float).SetString(periodStringArray[0])
		if !ok {
			return ids.ShortID{}, ids.ShortID{}, LockedAmount{}, nil, fmt.Errorf("invalid float: %s", periodStringArray[0])
		}
		locktime, err := strconv.ParseUint(periodStringArray[1], 10, 64)
		if err != nil {
			return ids.ShortID{}, ids.ShortID{}, LockedAmount{}, nil, err
		}

		rawAmount, precision := new(big.Float).Mul(amount, new(big.Float).SetUint64(units.Avax)).Uint64()
		if precision != big.Exact {
			return ids.ShortID{}, ids.ShortID{}, LockedAmount{}, nil, fmt.Errorf("non-specific amount provided: %s", periodStringArray[0])
		}
		unlockSchedule[i] = LockedAmount{
			Amount:   rawAmount,
			Locktime: locktime,
		}
	}
	return ethAddr, avaxAddr, unlockSchedule[0], unlockSchedule[1:], nil
}
