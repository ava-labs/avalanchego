// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"bytes"
	"encoding/json"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/math"

	avajson "github.com/ava-labs/avalanchego/utils/json"
)

var _ utils.Sortable[*Warp] = (*Warp)(nil)

type WarpSet struct {
	// Slice, in canonical ordering, of the validators that have a public key.
	Validators []*Warp
	// The total weight of all the validators, including the ones that don't
	// have a public key.
	TotalWeight uint64
}

type jsonWarpSet struct {
	Validators  []*Warp        `json:"validators"`
	TotalWeight avajson.Uint64 `json:"totalWeight"`
}

func (w WarpSet) MarshalJSON() ([]byte, error) {
	return json.Marshal(jsonWarpSet{
		Validators:  w.Validators,
		TotalWeight: avajson.Uint64(w.TotalWeight),
	})
}

func (w *WarpSet) UnmarshalJSON(b []byte) error {
	var j jsonWarpSet
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	w.TotalWeight = uint64(j.TotalWeight)
	w.Validators = j.Validators
	return nil
}

type Warp struct {
	PublicKey *bls.PublicKey
	// PublicKeyBytes is expected to be in the uncompressed form.
	PublicKeyBytes []byte
	Weight         uint64
	NodeIDs        []ids.NodeID
}

func (w *Warp) Compare(o *Warp) int {
	return bytes.Compare(w.PublicKeyBytes, o.PublicKeyBytes)
}

type jsonWarp struct {
	PublicKey string         `json:"publicKey"`
	Weight    avajson.Uint64 `json:"weight"`
	NodeIDs   []ids.NodeID   `json:"nodeIDs"`
}

func (w Warp) MarshalJSON() ([]byte, error) {
	pkBytes := bls.PublicKeyToCompressedBytes(w.PublicKey)
	pk, err := formatting.Encode(formatting.HexNC, pkBytes)
	if err != nil {
		return nil, err
	}
	return json.Marshal(jsonWarp{
		PublicKey: pk,
		Weight:    avajson.Uint64(w.Weight),
		NodeIDs:   w.NodeIDs,
	})
}

func (w *Warp) UnmarshalJSON(b []byte) error {
	var j jsonWarp
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}

	pkBytes, err := formatting.Decode(formatting.HexNC, j.PublicKey)
	if err != nil {
		return err
	}
	pk, err := bls.PublicKeyFromCompressedBytes(pkBytes)
	if err != nil {
		return err
	}
	*w = Warp{
		PublicKey:      pk,
		PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk),
		Weight:         uint64(j.Weight),
		NodeIDs:        j.NodeIDs,
	}
	return nil
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
