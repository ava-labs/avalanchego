// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package contract

//go:generate go run go.uber.org/mock/mockgen -package=$GOPACKAGE -source=interfaces.go -destination=mocks.go -exclude_interfaces StatefulPrecompiledContract,StateReader,ConfigurationBlockContext,Configurator
