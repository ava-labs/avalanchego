// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hashing

//go:generate go tool mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/hasher.go -mock_names=Hasher=Hasher . Hasher
