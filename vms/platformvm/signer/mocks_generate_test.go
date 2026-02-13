// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

//go:generate go tool mockgen -package=${GOPACKAGE}mock -source=signer.go -destination=${GOPACKAGE}mock/signer.go -mock_names=Signer=Signer
