package avm

import (
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
func (p *parser) Filter(param *pubsub.FilterParam) *pubsub.FilterResponse {
	for _, utxo := range p.tx.UTXOs() {
		if addresses, ok := utxo.Out.(avax.Addressable); ok {
			for _, address := range addresses.Addresses() {
				if param.CheckAddress(address) {
					return &pubsub.FilterResponse{TxID: p.tx.ID(), AddressID: pubsub.ByteToID(address)}
				}
			}
		}
	}
	return nil
}
