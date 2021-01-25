package pubsub

import "github.com/ava-labs/avalanchego/ids"

type FilterResponse struct {
	Channel   string      `json:"channel"`
	TxID      ids.ID      `json:"txID"`
	Address   string      `json:"address"`
	AddressID ids.ShortID `json:"addressId"`
}

type Parser interface {
	// expected a FilterResponse or nil if filter doesn't match
	Filter(*FilterParam) *FilterResponse
}
