// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"math"
	"time"
)

// reward returns the amount of tokens to reward the staker with
func reward(duration time.Duration, amount uint64, inflationRate float64) uint64 {
	// TODO: Can't use floats here. Need to figure out how to do some integer
	// approximations

	years := duration.Hours() / (365. * 24.)

	// Total value of this transaction
	value := float64(amount) * math.Pow(inflationRate, years)

	// Amount of the reward
	reward := value - float64(amount)

	return uint64(reward)
}
