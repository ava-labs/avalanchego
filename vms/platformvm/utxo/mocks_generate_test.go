// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utxo

//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/verifier.go -mock_names=Verifier=Verifier . Verifier
