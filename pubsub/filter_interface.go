// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

type Parser interface {
	// expected a txID or nil if filter doesn't match
	Filter(*FilterParam) interface{}
}
