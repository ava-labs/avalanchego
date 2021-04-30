package avm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/pubsub"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

type parser struct {
	tx *Tx
}

func NewPubSubParser(tx *Tx) pubsub.Parser {
	return &parser{tx: tx}
}

// Apply the filter on the addresses.
func (p *parser) Filter(param *pubsub.FilterParam) *ids.ID {
	for _, utxo := range p.tx.UTXOs() {
		if addresses, ok := utxo.Out.(avax.Addressable); ok {
			for _, address := range addresses.Addresses() {
				if param.CheckAddress(address) {
					txID := p.tx.ID()
					return &txID
				}
			}
		}
	}
	return nil
}
