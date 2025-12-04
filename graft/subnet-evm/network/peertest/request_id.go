// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peertest

const (
	// TestSDKRequestID is the request ID for the SDK request, which is odd-numbered.
	// See peer.IsNetworkRequest for more details.
	TestSDKRequestID uint32 = 1

	// TestPeerRequestID is the request ID for the peer request, which is even-numbered.
	// See peer.IsNetworkRequest for more details.
	TestPeerRequestID uint32 = 0
)
