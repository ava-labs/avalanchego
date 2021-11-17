// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import "crypto/tls"

func TLSConfig(cert tls.Certificate) *tls.Config {
	// #nosec G402
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAnyClientCert,
		// We do not use the TLS CA functionality to authenticate a
		// hostname. We only require an authenticated channel based on the
		// peer's public key. Therefore, we can safely skip CA verification.
		//
		// During our security audit by Quantstamp, this was investigated
		// and confirmed to be safe and correct.
		InsecureSkipVerify: true,
	}
}
