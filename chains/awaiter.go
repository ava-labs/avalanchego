// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/math"
)

type awaiter struct {
	vdrs      validators.Set
	reqWeight uint64
	weight    uint64
	ctx       *snow.Context
	eng       common.Engine
}

func (a *awaiter) Connected(vdrID ids.ShortID) bool {
	vdr, ok := a.vdrs.Get(vdrID)
	if !ok {
		return false
	}
	weight, err := math.Add64(vdr.Weight(), a.weight)
	a.weight = weight
	if err == nil && a.weight < a.reqWeight {
		return false
	}

	go func() {
		a.ctx.Lock.Lock()
		defer a.ctx.Lock.Unlock()
		a.eng.Startup()
	}()
	return true
}

func (a *awaiter) Disconnected(vdrID ids.ShortID) bool {
	if vdr, ok := a.vdrs.Get(vdrID); ok {
		a.weight, _ = math.Sub64(vdr.Weight(), a.weight)
	}
	return false
}
