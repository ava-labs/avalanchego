// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

const (
	typeLabel      = "type"
	opLabel        = "op"
	directionLabel = "direction"

	compressionLabel   = "compression"
	decompressionLabel = "decompression"
)

var (
	_ fmt.Stringer = (*InboundMessage)(nil)

	metricLabels = []string{typeLabel, opLabel, directionLabel}

	errUnknownCompressionType = errors.New("message is compressed with an unknown compression type")
)

type InboundMessage struct {
	NodeID                ids.NodeID
	Op                    Op
	Message               fmt.Stringer
	Expiration            time.Time
	BytesSavedCompression int
	onFinishedHandling    func()
}

func (m *InboundMessage) OnFinishedHandling() {
	if m.onFinishedHandling != nil {
		m.onFinishedHandling()
	}
}

func (m *InboundMessage) String() string {
	return fmt.Sprintf("%s Op: %s Message: %s",
		m.NodeID, m.Op, m.Message)
}

// OutboundMessage represents a set of fields for an outbound message that can
// be sent over the wire
type OutboundMessage struct {
	// BypassThrottling is true if we should send this message, regardless of
	// any outbound message throttling
	BypassThrottling bool
	Op               Op
	Bytes            []byte
	// BytesSavedCompression stores the amount of bytes that this message saved
	// due to being compressed
	BytesSavedCompression int
}

// TODO: add other compression algorithms with extended interface
type msgBuilder struct {
	zstdCompressor compression.Compressor
	count          *prometheus.CounterVec // type + op + direction
	duration       *prometheus.GaugeVec   // type + op + direction

	maxMessageTimeout time.Duration
}

func newMsgBuilder(
	metrics prometheus.Registerer,
	maxMessageTimeout time.Duration,
) (*msgBuilder, error) {
	zstdCompressor, err := compression.NewZstdCompressor(constants.DefaultMaxMessageSize)
	if err != nil {
		return nil, err
	}

	mb := &msgBuilder{
		zstdCompressor: zstdCompressor,
		count: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "codec_compressed_count",
				Help: "number of compressed messages",
			},
			metricLabels,
		),
		duration: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "codec_compressed_duration",
				Help: "time spent handling compressed messages",
			},
			metricLabels,
		),

		maxMessageTimeout: maxMessageTimeout,
	}
	return mb, errors.Join(
		metrics.Register(mb.count),
		metrics.Register(mb.duration),
	)
}

func (mb *msgBuilder) marshal(
	uncompressedMsg *p2p.Message,
	compressionType compression.Type,
) ([]byte, int, Op, error) {
	uncompressedMsgBytes, err := proto.Marshal(uncompressedMsg)
	if err != nil {
		return nil, 0, 0, err
	}

	op, err := ToOp(uncompressedMsg)
	if err != nil {
		return nil, 0, 0, err
	}

	// If compression is enabled, we marshal twice:
	// 1. the original message
	// 2. the message with compressed bytes
	//
	// This recursive packing allows us to avoid an extra compression on/off
	// field in the message.
	var (
		startTime     = time.Now()
		compressedMsg p2p.Message
	)
	switch compressionType {
	case compression.TypeNone:
		return uncompressedMsgBytes, 0, op, nil
	case compression.TypeZstd:
		compressedBytes, err := mb.zstdCompressor.Compress(uncompressedMsgBytes)
		if err != nil {
			return nil, 0, 0, err
		}
		compressedMsg = p2p.Message{
			Message: &p2p.Message_CompressedZstd{
				CompressedZstd: compressedBytes,
			},
		}
	default:
		return nil, 0, 0, errUnknownCompressionType
	}

	compressedMsgBytes, err := proto.Marshal(&compressedMsg)
	if err != nil {
		return nil, 0, 0, err
	}
	compressTook := time.Since(startTime)

	labels := prometheus.Labels{
		typeLabel:      compressionType.String(),
		opLabel:        op.String(),
		directionLabel: compressionLabel,
	}
	mb.count.With(labels).Inc()
	mb.duration.With(labels).Add(float64(compressTook))

	bytesSaved := len(uncompressedMsgBytes) - len(compressedMsgBytes)
	return compressedMsgBytes, bytesSaved, op, nil
}

func (mb *msgBuilder) unmarshal(b []byte) (*p2p.Message, int, Op, error) {
	m := new(p2p.Message)
	if err := proto.Unmarshal(b, m); err != nil {
		return nil, 0, 0, err
	}

	// Figure out what compression type, if any, was used to compress the message.
	var (
		compressor      compression.Compressor
		compressedBytes []byte
		zstdCompressed  = m.GetCompressedZstd()
	)
	switch {
	case len(zstdCompressed) > 0:
		compressor = mb.zstdCompressor
		compressedBytes = zstdCompressed
	default:
		// The message wasn't compressed
		op, err := ToOp(m)
		return m, 0, op, err
	}

	startTime := time.Now()

	decompressed, err := compressor.Decompress(compressedBytes)
	if err != nil {
		return nil, 0, 0, err
	}
	bytesSavedCompression := len(decompressed) - len(compressedBytes)

	if err := proto.Unmarshal(decompressed, m); err != nil {
		return nil, 0, 0, err
	}
	decompressTook := time.Since(startTime)

	// Record decompression time metric
	op, err := ToOp(m)
	if err != nil {
		return nil, 0, 0, err
	}

	labels := prometheus.Labels{
		typeLabel:      compression.TypeZstd.String(),
		opLabel:        op.String(),
		directionLabel: decompressionLabel,
	}
	mb.count.With(labels).Inc()
	mb.duration.With(labels).Add(float64(decompressTook))

	return m, bytesSavedCompression, op, nil
}

func (mb *msgBuilder) createOutbound(m *p2p.Message, compressionType compression.Type, bypassThrottling bool) (*OutboundMessage, error) {
	b, saved, op, err := mb.marshal(m, compressionType)
	if err != nil {
		return nil, err
	}

	return &OutboundMessage{
		BypassThrottling:      bypassThrottling,
		Op:                    op,
		Bytes:                 b,
		BytesSavedCompression: saved,
	}, nil
}

func (mb *msgBuilder) parseInbound(
	bytes []byte,
	nodeID ids.NodeID,
	onFinishedHandling func(),
) (*InboundMessage, error) {
	m, bytesSavedCompression, op, err := mb.unmarshal(bytes)
	if err != nil {
		return nil, err
	}

	msg, err := Unwrap(m)
	if err != nil {
		return nil, err
	}

	expiration := mockable.MaxTime
	if deadline, ok := GetDeadline(msg); ok {
		deadline = min(deadline, mb.maxMessageTimeout)
		expiration = time.Now().Add(deadline)
	}

	return &InboundMessage{
		NodeID:                nodeID,
		Op:                    op,
		Message:               msg,
		Expiration:            expiration,
		onFinishedHandling:    onFinishedHandling,
		BytesSavedCompression: bytesSavedCompression,
	}, nil
}
