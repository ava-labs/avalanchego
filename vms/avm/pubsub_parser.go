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
func (p *parser) Filter(connections []pubsub.FilterInterface) ([]pubsub.FilterInterface, interface{}) {
	var resp []pubsub.FilterInterface
	respm := make(map[pubsub.FilterInterface]struct{})
	for _, utxo := range p.tx.UTXOs() {
		if addresses, ok := utxo.Out.(avax.Addressable); ok {
			for _, address := range addresses.Addresses() {
				for _, c := range connections {
					if _, ok := respm[c]; ok {
						continue
					}
					sid, err := ids.ToShortID(address)
					if err != nil {
						// return an error?
						continue
					}
					if c.CheckAddress(sid) {
						respm[c] = struct{}{}
						resp = append(resp, c)
					}
				}
			}
		}
	}
	txID := p.tx.ID()
	return resp, &txID
}
