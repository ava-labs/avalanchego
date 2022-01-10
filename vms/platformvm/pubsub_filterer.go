package platformvm

import (
	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/pubsub"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

var _ pubsub.Filterer = &baseFilterer{}

type baseFilterer struct {
	tx avax.AddressableID
}

func NewPubSubFilterer(tx avax.AddressableID) pubsub.Filterer {
	return &baseFilterer{tx: tx}
}

// Filter applies the filters on the addresses.
func (f *baseFilterer) Filter(filters []pubsub.Filter) ([]bool, interface{}) {
	resp := make([]bool, len(filters))
	for _, address := range f.tx.Addresses() {
		for i, c := range filters {
			if resp[i] {
				continue
			}
			resp[i] = c.Check(address)
		}
	}
	return resp, api.JSONTxID{
		TxID: f.tx.ID(),
	}
}
