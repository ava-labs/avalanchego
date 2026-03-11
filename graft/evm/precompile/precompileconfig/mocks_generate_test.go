// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompileconfig

//go:generate go tool mockgen -package=$GOPACKAGE -destination=mocks.go . Predicater,Config,ChainConfig,Accepter
