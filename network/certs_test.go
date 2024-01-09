// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"crypto/tls"
	"sync"
	"testing"

	_ "embed"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/staking"
)

var (
	//go:embed test_cert_1.crt
	testCertBytes1 []byte
	//go:embed test_key_1.key
	testKeyBytes1 []byte
	//go:embed test_cert_2.crt
	testCertBytes2 []byte
	//go:embed test_key_2.key
	testKeyBytes2 []byte
	//go:embed test_cert_3.crt
	testCertBytes3 []byte
	//go:embed test_key_3.key
	testKeyBytes3 []byte

	certLock   sync.Mutex
	tlsCerts   []*tls.Certificate
	tlsConfigs []*tls.Config
)

func init() {
	cert1, err := staking.LoadTLSCertFromBytes(testKeyBytes1, testCertBytes1)
	if err != nil {
		panic(err)
	}
	cert2, err := staking.LoadTLSCertFromBytes(testKeyBytes2, testCertBytes2)
	if err != nil {
		panic(err)
	}
	cert3, err := staking.LoadTLSCertFromBytes(testKeyBytes3, testCertBytes3)
	if err != nil {
		panic(err)
	}
	tlsCerts = []*tls.Certificate{
		cert1, cert2, cert3,
	}
}

func getTLS(t *testing.T, index int) (ids.NodeID, *tls.Certificate, *tls.Config) {
	certLock.Lock()
	defer certLock.Unlock()

	for len(tlsCerts) <= index {
		cert, err := staking.NewTLSCert()
		require.NoError(t, err)
		tlsCerts = append(tlsCerts, cert)
	}
	for len(tlsConfigs) <= index {
		cert := tlsCerts[len(tlsConfigs)]
		tlsConfig := peer.TLSConfig(*cert, nil)
		tlsConfigs = append(tlsConfigs, tlsConfig)
	}

	tlsCert := tlsCerts[index]
	cert := staking.CertificateFromX509(tlsCert.Leaf)
	nodeID := ids.NodeIDFromCert(cert)
	return nodeID, tlsCert, tlsConfigs[index]
}
