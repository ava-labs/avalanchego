// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/outbound_message.go -mock_names=OutboundMessage=OutboundMessage . OutboundMessage
//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/outbound_message_builder.go -mock_names=OutboundMsgBuilder=OutboundMsgBuilder . OutboundMsgBuilder
