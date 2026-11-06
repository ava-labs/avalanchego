// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package random_network

import (
	"context"
	"crypto/rand"
	"encoding/asn1"
	"errors"
	"fmt"
)

var errTxVerification = errors.New("tx verification failed")

type txID [32]byte

type TX struct {
	ID                     txID
	shouldFailVerification bool
}

func (t *TX) String() string {
	return fmt.Sprintf("%x", t.ID[:])
}

type asn1TX struct {
	ID                     []byte
	ShouldFailVerification bool
}

func (aTX asn1TX) toTX() *TX {
	var idArr txID
	copy(idArr[:], aTX.ID)
	return &TX{ID: idArr, shouldFailVerification: aTX.ShouldFailVerification}
}

func CreateNewTX() *TX {
	id := make([]byte, 32)
	_, err := rand.Read(id)
	if err != nil {
		panic(err)
	}

	var idArr txID
	copy(idArr[:], id)

	return &TX{ID: idArr}
}

func (t *TX) SetShouldFailVerification() {
	t.shouldFailVerification = true
}

func (t *TX) Bytes() ([]byte, error) {
	return asn1.Marshal(asn1TX{ID: t.ID[:], ShouldFailVerification: t.shouldFailVerification})
}

func TxFromBytes(b []byte) (*TX, error) {
	var asn1TX asn1TX
	_, err := asn1.Unmarshal(b, &asn1TX)
	if err != nil {
		return nil, err
	}

	return asn1TX.toTX(), nil
}

func (t *TX) Verify(ctx context.Context) error {
	// TBD
	// Can set artificial failure here for testing or longer verification times
	if t.shouldFailVerification {
		return errTxVerification
	}
	return nil
}
