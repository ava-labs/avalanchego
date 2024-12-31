// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

//go:generate go run go.uber.org/mock/mockgen@v0.4 -package=${GOPACKAGE}mock -source=router.go -destination=${GOPACKAGE}mock/router.go -mock_names=Router=Router -exclude_interfaces=InternalHandler
