// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timeout

//go:generate go run go.uber.org/mock/mockgen@v0.5 -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/manager.go -mock_names=Manager=Manager . Manager
