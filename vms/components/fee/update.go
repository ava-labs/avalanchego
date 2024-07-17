// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

// var errChildTimeBeforeParent = errors.New("child block time before parent block time")

// // We cap the maximum gas consumed by time with a leaky bucket approach
// // GasCap = min (GasCap + MaxGasPerSecond/LeakGasCoeff*ElapsedTime, MaxGasPerSecond)
// func GasCap(cfg Config, currentGasCapacity Gas, parentBlkTime, childBlkTime time.Time) (Gas, error) {
// 	if childBlkTime.Before(parentBlkTime) {
// 		return 0, fmt.Errorf("%w, parentBlkTim %v, childBlkTime %v", errChildTimeBeforeParent, parentBlkTime, childBlkTime)
// 	}

// 	elapsedTime := uint64(childBlkTime.Unix() - parentBlkTime.Unix())
// 	if elapsedTime > uint64(cfg.LeakGasCoeff) {
// 		return cfg.MaxGasPerSecond, nil
// 	}

// 	return min(cfg.MaxGasPerSecond, currentGasCapacity+cfg.MaxGasPerSecond*Gas(elapsedTime)/cfg.LeakGasCoeff), nil
// }

// func UpdateGasCap(currentGasCap, blkGas Gas) Gas {
// 	nextGasCap := Gas(0)
// 	if currentGasCap > blkGas {
// 		nextGasCap = currentGasCap - blkGas
// 	}
// 	return nextGasCap
// }
