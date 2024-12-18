package fee

import "github.com/ava-labs/avalanchego/vms/platformvm/txs"

var _ Calculator = &SimpleCalculator{}

type SimpleCalculator struct {
	txFee uint64
}

func NewSimpleCalculator(fee uint64) *SimpleCalculator {
	return &SimpleCalculator{
		txFee: fee,
	}
}

func (c *SimpleCalculator) CalculateFee(txs.UnsignedTx) (uint64, error) {
	return c.txFee, nil
}
