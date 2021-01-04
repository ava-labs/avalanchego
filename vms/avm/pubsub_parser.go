package avm

import (
	"github.com/ava-labs/avalanchego/pubsub"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

type parser struct {
	tx *Tx
}

func NewPubSubParser(tx *Tx) pubsub.Parser {
	return &parser{tx: tx}
}

func (p *parser) Filter(param *pubsub.FilterParam) (*pubsub.FilterResponse, error) {
	for _, utxo := range p.tx.UTXOs() {
		switch utxoOut := utxo.Out.(type) {
		case avax.Addressable:
			addresses := utxoOut.Addresses()
			for _, address := range addresses {
				var sid ids.ShortID
				if len(address) != len(sid) {
					continue
				}
				copy(sid[:], address)
				for _, addr := range param.Address {
					if p.compare(addr, sid) {
						return &pubsub.FilterResponse{TxID: p.tx.ID(), Address: sid}, nil
					}
				}
			}
		default:
		}
	}
	return nil, nil
}

func (p *parser) compare(a ids.ShortID, b ids.ShortID) bool {
	for i := 0; i < len(a); i++ {
		if (a[i] & b[i]) != b[i] {
			return false
		}
	}
	return true
}
