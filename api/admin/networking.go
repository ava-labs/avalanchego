// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package admin

import (
	"sort"

	"github.com/ava-labs/gecko/utils"
)

// Peerable can return a group of peers
type Peerable interface{ IPs() []utils.IPDesc }

// Networking provides helper methods for tracking the current network state
type Networking struct{ peers Peerable }

// Peers returns the current peers
func (n *Networking) Peers() ([]string, error) {
	ipDescs := n.peers.IPs()
	ips := make([]string, len(ipDescs))
	for i, ipDesc := range ipDescs {
		ips[i] = ipDesc.String()
	}
	sort.Strings(ips)
	return ips, nil
}
