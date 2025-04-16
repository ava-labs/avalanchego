// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE} -destination=mocks_test.go . PostForkBlock
//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE} -destination=mock_test.go . SelfSubscriber
