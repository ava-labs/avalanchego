// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

//go:generate go run go.uber.org/mock/mockgen@v0.4 -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/linearizable_vm.go -mock_names=LinearizableVM=LinearizableVM . LinearizableVM
