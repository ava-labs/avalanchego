// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"bytes"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/math"
)

var _ utils.Sortable[*Warp] = (*Warp)(nil)

type WarpSet struct {
	// Validators slice in canonical ordering of the validators that has public key
	Validators []*Warp
	// The total weight of all the validators, including the ones that doesn't have a public key
	TotalWeight uint64
}

type Warp struct {
	PublicKey      *bls.PublicKey
	PublicKeyBytes []byte
	Weight         uint64
	NodeIDs        []ids.NodeID
}

func (w *Warp) Compare(o *Warp) int {
	return bytes.Compare(w.PublicKeyBytes, o.PublicKeyBytes)
}

// FlattenValidatorSet converts the provided vdrSet into a canonical ordering.
func FlattenValidatorSet(vdrSet map[ids.NodeID]*GetValidatorOutput) (WarpSet, error) {
	var (
		vdrs        = make(map[string]*Warp, len(vdrSet))
		totalWeight uint64
		err         error
	)
	for _, vdr := range vdrSet {
		totalWeight, err = math.Add(totalWeight, vdr.Weight)
		if err != nil {
			return WarpSet{}, err
		}

		if vdr.PublicKey == nil {
			continue
		}

		pkBytes := bls.PublicKeyToUncompressedBytes(vdr.PublicKey)
		uniqueVdr, ok := vdrs[string(pkBytes)]
		if !ok {
			uniqueVdr = &Warp{
				PublicKey:      vdr.PublicKey,
				PublicKeyBytes: pkBytes,
			}
			vdrs[string(pkBytes)] = uniqueVdr
		}

		uniqueVdr.Weight += vdr.Weight // Impossible to overflow here
		uniqueVdr.NodeIDs = append(uniqueVdr.NodeIDs, vdr.NodeID)
	}

	// Sort validators by public key
	vdrList := maps.Values(vdrs)
	utils.Sort(vdrList)
	return WarpSet{Validators: vdrList, TotalWeight: totalWeight}, nil
}
