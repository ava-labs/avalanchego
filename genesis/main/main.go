// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"os"
	"strconv"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/units"
)

type lockedAmount struct {
	amount   uint64
	locktime uint64
}

type allocation struct {
	ethAddr        ids.ShortID
	avaxAddr       ids.ShortID
	initialAmount  uint64
	unlockSchedule []lockedAmount
}

// Config contains the genesis addresses used to construct a genesis
type Config struct {
	NetworkID uint32

	Allocations []allocation

	StartTime             uint64
	InitialStakeAmount    uint64
	InitialStakeDuration  uint64
	InitialStakeAddresses []ids.ShortID

	CChainGenesis []byte

	Message string
}

func main() {
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
			fmt.Println()
			fmt.Println(line)
			fmt.Println(err)
			fmt.Println()
		}
		// fmt.Println(ethAddr, avaxAddr, initialUnlock, unlockSchedule)
		_, _, _, _ = ethAddr, avaxAddr, initialUnlock, unlockSchedule
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
}

func parseLine(line string) (ids.ShortID, ids.ShortID, lockedAmount, []lockedAmount, error) {
	triple := strings.SplitN(line, "', ", 3)
	ethAddrString := triple[0][4:]
	ethAddrBytes, err := hex.DecodeString(ethAddrString)
	if err != nil {
		return ids.ShortID{}, ids.ShortID{}, lockedAmount{}, nil, err
	}
	ethAddr, err := ids.ToShortID(ethAddrBytes)
	if err != nil {
		return ids.ShortID{}, ids.ShortID{}, lockedAmount{}, nil, err
	}

	avaxAddrString := triple[1][1:]
	_, _, avaxAddrBytes, err := formatting.ParseAddress(avaxAddrString)
	if err != nil {
		return ids.ShortID{}, ids.ShortID{}, lockedAmount{}, nil, err
	}
	avaxAddr, err := ids.ToShortID(avaxAddrBytes)
	if err != nil {
		return ids.ShortID{}, ids.ShortID{}, lockedAmount{}, nil, err
	}

	unlockScheduleString := triple[2][2 : len(triple[2])-3]
	unlockScheduleStringArray := strings.Split(unlockScheduleString, "), (")
	unlockSchedule := make([]lockedAmount, len(unlockScheduleStringArray))
	for i, periodString := range unlockScheduleStringArray {
		periodStringArray := strings.Split(periodString, ", ")
		amount, ok := new(big.Float).SetString(periodStringArray[0])
		if !ok {
			return ids.ShortID{}, ids.ShortID{}, lockedAmount{}, nil, fmt.Errorf("invalid float: %s", periodStringArray[0])
		}
		locktime, err := strconv.ParseUint(periodStringArray[1], 10, 64)
		if err != nil {
			return ids.ShortID{}, ids.ShortID{}, lockedAmount{}, nil, err
		}

		rawAmount, precision := new(big.Float).Mul(amount, new(big.Float).SetUint64(units.Avax)).Uint64()
		if precision != big.Exact {
			return ids.ShortID{}, ids.ShortID{}, lockedAmount{}, nil, fmt.Errorf("non-specific amount provided: %s", periodStringArray[0])
		}
		unlockSchedule[i] = lockedAmount{
			amount:   rawAmount,
			locktime: locktime,
		}
	}
	return ethAddr, avaxAddr, unlockSchedule[0], unlockSchedule[1:], nil
}
