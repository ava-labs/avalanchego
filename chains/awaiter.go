// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/math"
)

type awaitConnected struct {
	connected func()
	vdrs      validators.Set
	reqWeight uint64
	weight    uint64
}

// NewAwaiter returns a new handler that will await for a sufficient number of
// validators to be connected.
func NewAwaiter(vdrs validators.Set, reqWeight uint64, connected func()) validators.Connector {
	return &awaitConnected{
		vdrs:      vdrs,
		reqWeight: reqWeight,
		connected: connected,
	}
}

func (a *awaitConnected) Connected(vdrID ids.ShortID) bool {
	weight, ok := a.vdrs.GetWeight(vdrID)
	if !ok {
		return false
	}
	weight, err := math.Add64(weight, a.weight)
	a.weight = weight
	// If the error is non-nil, then an overflow error has occurred such that
	// the required weight was surpassed. As per network.Handler interface,
	// this handler should be removed and never called again after returning true.
	if err == nil && a.weight < a.reqWeight {
		return false
	}

	go a.connected()
	return true
}

func (a *awaitConnected) Disconnected(vdrID ids.ShortID) bool {
	if weight, ok := a.vdrs.GetWeight(vdrID); ok {
		// TODO: Account for weight changes in a more robust manner.

		// Sub64 should rarely error since only validators that have added their
		// weight can become disconnected. Because it is possible that there are
		// changes to the validators set, we utilize that Sub64 returns 0 on
		// error.
		a.weight, _ = math.Sub64(a.weight, weight)
	}
	return false
}
