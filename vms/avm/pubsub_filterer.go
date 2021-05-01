package avm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/pubsub"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

type filterer struct {
	tx *Tx
}

func NewPubSubFilterer(tx *Tx) pubsub.Filterer {
	return &filterer{tx: tx}
}

// Apply the filter on the addresses.
func (f *filterer) Filter(filters []pubsub.FilterInterface) ([]bool, interface{}) {
	resp := make([]bool, len(filters))
	for _, utxo := range f.tx.UTXOs() {
		addressable, ok := utxo.Out.(avax.Addressable)
		if !ok {
			continue
		}

		for _, address := range addressable.Addresses() {
			for i, c := range filters {
				if resp[i] {
					continue
				}

				sid, err := ids.ToShortID(address)
				if err != nil {
					// return an error?
					continue
				}
				resp[i] = c.CheckAddress(sid)
			}
		}
	}
	return resp, f.tx.ID()
}
