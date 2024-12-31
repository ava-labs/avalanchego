// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

//go:generate go run go.uber.org/mock/mockgen@v0.5 -package=${GOPACKAGE}mock -source=external_sender.go -destination=${GOPACKAGE}mock/external_sender.go -mock_names=ExternalSender=ExternalSender
