// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import (
	"encoding/json"
	"io"

	"github.com/ava-labs/avalanchego/utils/formatting"
)

// CommandMessage command message
type CommandMessage struct {
	Command string `json:"command"`
	// Addresses array of avax addresses i.e. fuji123....
	Addresses []string `json:"addresses,omitempty"`
	// FilterMax size of bloom filter
	FilterMax uint64 `json:"filterMax,omitempty"`
	// FilterError expected error rate of filter
	FilterError float64 `json:"filterError,omitempty"`
	// subscription to this kind of messages
	EventType EventType `json:"eventType"`
	// Unsubscribe unsubscribe channel remove address or reset filter
	Unsubscribe bool `json:"unsubscribe"`
	// AddressIds array of addresses, kept as a [][]byte for use in the bloom filter
	addressIds [][]byte `json:"-"`
}

func NewCommandMessage(r io.Reader) (*CommandMessage, error) {
	c := CommandMessage{}
	err := c.Load(r)
	if err != nil {
		return nil, err
	}
	err = c.ParseAddresses()
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *CommandMessage) IsNewFilter() bool {
	return c.FilterMax > 0 && c.FilterError > 0
}

func (c *CommandMessage) FilterOrDefault() {
	if c.IsNewFilter() {
		return
	}
	c.FilterMax = DefaultFilterMax
	c.FilterError = DefaultFilterError
}

// ParseAddresses converts the bech32 addresses to their byte equiv ids.ShortID.
func (c *CommandMessage) ParseAddresses() error {
	if c.addressIds == nil {
		c.addressIds = make([][]byte, 0, len(c.Addresses))
	}
	for _, astr := range c.Addresses {
		_, _, abytes, err := formatting.ParseAddress(astr)
		if err != nil {
			return err
		}
		c.addressIds = append(c.addressIds, abytes)
	}
	return nil
}

func (c *CommandMessage) Load(r io.Reader) error {
	return json.NewDecoder(r).Decode(c)
}
