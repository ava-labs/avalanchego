// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -source=signer.go -destination=${GOPACKAGE}mock/signer.go -mock_names=Signer=Signer
