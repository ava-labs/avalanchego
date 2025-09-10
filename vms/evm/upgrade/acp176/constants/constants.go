package constants

const (
	MinTargetPerSecond  = 1_000_000                                 // P
	TargetConversion    = maxTargetChangeRate * MaxTargetExcessDiff // D
	MaxTargetExcessDiff = 1 << 15                                   // Q

	TargetToMax     = 2 // multiplier to convert from target per second to max per second
	MinMaxPerSecond = MinTargetPerSecond * TargetToMax

	MaxTimeToDouble       = 60 * 60 * 24 // 1 day
	MaxTimeToFillCapacity = 60 * 10      // 10 minutes

	maxTargetChangeRate = 1024 // Controls the rate that the target can change per block.
)
