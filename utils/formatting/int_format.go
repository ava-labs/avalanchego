// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"fmt"
	"math"
)

func IntFormat(maxValue int) string {
	log := 1
	if maxValue > 0 {
		log = int(math.Ceil(math.Log10(float64(maxValue + 1))))
	}
	return fmt.Sprintf("%%0%dd", log)
}
