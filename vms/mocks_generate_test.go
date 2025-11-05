// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vms

//go:generate go tool -modfile=../tools/go.mod mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/factory.go -mock_names=Factory=Factory . Factory
//go:generate go tool -modfile=../tools/go.mod mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/manager.go -mock_names=Manager=Manager . Manager
