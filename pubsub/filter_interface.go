// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import "github.com/ava-labs/avalanchego/ids"

type Parser interface {
	// expected a txID or nil if filter doesn't match
	Filter(*FilterParam) *ids.ID
}
