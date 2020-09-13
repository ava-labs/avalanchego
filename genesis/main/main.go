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

	"github.com/ava-labs/avalanche-go/utils/constants"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/units"

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
