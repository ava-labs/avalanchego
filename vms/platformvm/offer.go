package platformvm

import (
	"time"

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/utils/hashing"
)

const InterestRateDenominator uint64 = 1_000_000

type LockRuleOffer struct {
	id ids.ID

	InterestRateNominator uint64 `serialize:"true" json:"interestRate"`
	Start                 uint64 `serialize:"true" json:"start"`
	End                   uint64 `serialize:"true" json:"end"`
	MinAmount             uint64 `serialize:"true" json:"minAmount"`
	Duration              uint64 `serialize:"true" json:"duration"`
}

func (o *LockRuleOffer) Initialize() error {
	bytes, err := Codec.Marshal(CodecVersion, &o)
	if err != nil {
		return err
	}
	o.id = hashing.ComputeHash256Array(bytes)
	return nil
}

func (o LockRuleOffer) ID() ids.ID {
	return o.id
}

func (o LockRuleOffer) StartTime() time.Time {
	return time.Unix(int64(o.Start), 0)
}

func (o LockRuleOffer) EndTime() time.Time {
	return time.Unix(int64(o.End), 0)
}

func (o LockRuleOffer) InterestRateFloat64() float64 {
	return float64(o.InterestRateNominator) / float64(InterestRateDenominator)
}
