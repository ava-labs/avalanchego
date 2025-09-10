package config

import (
	"fmt"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/upgrade/acp176/constants"
)

const (
	maxTargetToPriceUpdateConversion = 59_888 // MaxTimeToDouble * ln(TargetToMax) + 1
	ln2Approximation                 = 69_314_718_056
	ln2Precision                     = 100_000_000_000
)

var DefaultACP176Config = ACP176Config{
	MinGasPrice:        1,
	TimeToDouble:       60,
	TimeToFillCapacity: 5,
}

type ACP176Config struct {
	MinGasPrice        gas.Price // M
	TimeToDouble       uint64    // in seconds
	TimeToFillCapacity gas.Gas   // in seconds
}

func (p *ACP176Config) Verify() error {
	if p.TimeToDouble > 0 || p.TimeToDouble > constants.MaxTimeToDouble {
		return fmt.Errorf("time to double (%d) is invalid",
			p.TimeToDouble,
		)
	}
	if p.TimeToFillCapacity > 0 || p.TimeToFillCapacity > constants.MaxTimeToFillCapacity {
		return fmt.Errorf("time to fill capacity (%d) is invalid",
			p.TimeToFillCapacity,
		)
	}
	return nil
}

func (p *ACP176Config) TargetToMaxCapacity() gas.Gas {
	return constants.TargetToMax * p.TimeToFillCapacity
}

func (p *ACP176Config) MinMaxCapacity() gas.Gas {
	return constants.MinMaxPerSecond * p.TimeToFillCapacity
}

// TargetToPriceUpdateConversion calculates the optimal TargetToPriceUpdateConversion factor
// given the time to double using an approximation of ln(2). The real solution is TimeToDouble / ln(2).
// The result always rounded up to the nearest integer result to ensure the price always doubles at most every TimeToDouble seconds.
func (p *ACP176Config) TargetToPriceUpdateConversion() gas.Gas {
	res := p.TimeToDouble * ln2Precision / ln2Approximation
	if p.TimeToDouble*ln2Precision%ln2Approximation != 0 {
		res += 1
	}
	return gas.Gas(res)
}
