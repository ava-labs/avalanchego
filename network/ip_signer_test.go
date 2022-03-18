// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"crypto"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

func TestIPSigner(t *testing.T) {
	assert := assert.New(t)

	dynIP := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		0,
	)
	clock := mockable.Clock{}
	clock.Set(time.Unix(10, 0))

	tlsCert, err := staking.NewTLSCert()
	assert.NoError(err)

	key := tlsCert.PrivateKey.(crypto.Signer)

	s := newIPSigner(&dynIP, &clock, key)

	signedIP1, err := s.getSignedIP()
	assert.NoError(err)
	assert.EqualValues(dynIP.IP(), signedIP1.IP.IP)
	assert.EqualValues(10, signedIP1.IP.Timestamp)

	clock.Set(time.Unix(11, 0))

	signedIP2, err := s.getSignedIP()
	assert.NoError(err)
	assert.EqualValues(dynIP.IP(), signedIP2.IP.IP)
	assert.EqualValues(10, signedIP2.IP.Timestamp)
	assert.EqualValues(signedIP1.Signature, signedIP2.Signature)

	dynIP.Update(utils.IPDesc{
		IP:   net.IPv6loopback,
		Port: 1,
	})

	signedIP3, err := s.getSignedIP()
	assert.NoError(err)
	assert.EqualValues(dynIP.IP(), signedIP3.IP.IP)
	assert.EqualValues(11, signedIP3.IP.Timestamp)
	assert.NotEqualValues(signedIP2.Signature, signedIP3.Signature)
}
