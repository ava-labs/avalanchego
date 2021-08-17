package platformvm

import (
	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/pubsub"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

var _ pubsub.Filterer = &baseFilterer{}
var _ pubsub.Filterer = &exportFilterer{}

type baseFilterer struct {
	tx *BaseTx
}

func NewPubSubFilterer(tx *BaseTx) pubsub.Filterer {
	return &baseFilterer{tx: tx}
}

// Apply the filter on the addresses.
func (f *baseFilterer) Filter(filters []pubsub.Filter) ([]bool, interface{}) {
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
				resp[i] = c.Check(address)
			}
		}
	}
	return resp, api.JSONTxID{
		TxID: f.tx.ID(),
	}
}

type exportFilterer struct {
	tx *UnsignedExportTx
}

func NewPubSubExportFilterer(tx *UnsignedExportTx) pubsub.Filterer {
	return &exportFilterer{tx: tx}
}

// Apply the filter on the addresses.
func (f *exportFilterer) Filter(filters []pubsub.Filter) ([]bool, interface{}) {
	resp := make([]bool, len(filters))
	f.tx.vm.ctx.Log.Info("")

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
				resp[i] = c.Check(address)
			}
		}
	}
	for _, outputs := range f.tx.ExportedOutputs {
		addressable, ok := outputs.Out.(avax.Addressable)
		if !ok {
			continue
		}

		for _, address := range addressable.Addresses() {
			for i, c := range filters {
				if resp[i] {
					continue
				}
				resp[i] = c.Check(address)
			}
		}
	}
	return resp, api.JSONTxID{
		TxID: f.tx.ID(),
	}
}
