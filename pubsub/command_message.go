// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import (
	"github.com/ava-labs/avalanchego/utils/formatting"
)

// NewBloom command for a new bloom filter
type NewBloom struct {
	// FilterMax size of bloom filter
	FilterMax uint64 `json:"filterMax"`
	// FilterError expected error rate of filter
	FilterError float64 `json:"filterError"`
}

// NewSet command for a new map set
type NewSet struct {
}

// AddAddresses command to add addresses
type AddAddresses struct {
	// Addresses bech 32 addresses toa add
	Addresses []string `json:"addresses"`
	// addressIds array of addresses, kept as a [][]byte for use in the bloom filter
	addressIds [][]byte
}

// Command execution command
type Command struct {
	NewBloom     *NewBloom     `json:"newBloom,omitempty"`
	NewSet       *NewSet       `json:"newSet,omitempty"`
	AddAddresses *AddAddresses `json:"addAddresses,omitempty"`
}

func (c *Command) String() string {
	switch {
	case c.NewBloom != nil:
		return "newBloom"
	case c.NewSet != nil:
		return "newSet"
	case c.AddAddresses != nil:
		return "addAddresses"
	}
	return "unknown"
}

func (c *NewBloom) IsNewFilter() bool {
	return c.FilterMax > 0 && c.FilterError > 0
}

func (c *NewBloom) FilterOrDefault() {
	if c.IsNewFilter() {
		return
	}
	c.FilterMax = DefaultFilterMax
	c.FilterError = DefaultFilterError
}

// ParseAddresses converts the bech32 addresses to their byte equiv ids.ShortID.
func (c *AddAddresses) ParseAddresses() error {
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
