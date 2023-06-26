// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	_ InboundMessage  = (*inboundMessage)(nil)
	_ OutboundMessage = (*outboundMessage)(nil)

	errUnknownCompressionType = errors.New("message is compressed with an unknown compression type")
)

// InboundMessage represents a set of fields for an inbound message
type InboundMessage interface {
	fmt.Stringer
	// NodeID returns the ID of the node that sent this message
	NodeID() ids.NodeID
	// Op returns the op that describes this message type
	Op() Op
	// Message returns the message that was sent
	Message() fmt.Stringer
	// Expiration returns the time that the sender will have already timed out
	// this request
	Expiration() time.Time
	// OnFinishedHandling must be called one time when this message has been
	// handled by the message handler
	OnFinishedHandling()
	// BytesSavedCompression returns the number of bytes that this message saved
	// due to being compressed
	BytesSavedCompression() int
}

type inboundMessage struct {
	nodeID                ids.NodeID
	op                    Op
	message               fmt.Stringer
	expiration            time.Time
	onFinishedHandling    func()
	bytesSavedCompression int
}

func (m *inboundMessage) NodeID() ids.NodeID {
	return m.nodeID
}

func (m *inboundMessage) Op() Op {
	return m.op
}

func (m *inboundMessage) Message() fmt.Stringer {
	return m.message
}

func (m *inboundMessage) Expiration() time.Time {
	return m.expiration
}

func (m *inboundMessage) OnFinishedHandling() {
	if m.onFinishedHandling != nil {
		m.onFinishedHandling()
	}
}

func (m *inboundMessage) BytesSavedCompression() int {
	return m.bytesSavedCompression
}

func (m *inboundMessage) String() string {
	return fmt.Sprintf("%s Op: %s Message: %s",
		m.nodeID, m.op, m.message)
}

// OutboundMessage represents a set of fields for an outbound message that can
// be serialized into a byte stream
type OutboundMessage interface {
	// BypassThrottling returns true if we should send this message, regardless
	// of any outbound message throttling
	BypassThrottling() bool
	// Op returns the op that describes this message type
	Op() Op
	// Bytes returns the bytes that will be sent
	Bytes() []byte
	// BytesSavedCompression returns the number of bytes that this message saved
	// due to being compressed
	BytesSavedCompression() int
}

type outboundMessage struct {
	bypassThrottling      bool
	op                    Op
	bytes                 []byte
	bytesSavedCompression int
}

func (m *outboundMessage) BypassThrottling() bool {
	return m.bypassThrottling
}

func (m *outboundMessage) Op() Op {
	return m.op
}

func (m *outboundMessage) Bytes() []byte {
	return m.bytes
}

func (m *outboundMessage) BytesSavedCompression() int {
	return m.bytesSavedCompression
}

// TODO: add other compression algorithms with extended interface
type msgBuilder struct {
	log logging.Logger

	gzipCompressor            compression.Compressor
	gzipCompressTimeMetrics   map[Op]metric.Averager
	gzipDecompressTimeMetrics map[Op]metric.Averager

	zstdCompressor            compression.Compressor
	zstdCompressTimeMetrics   map[Op]metric.Averager
	zstdDecompressTimeMetrics map[Op]metric.Averager

	maxMessageTimeout time.Duration
}

func newMsgBuilder(
	log logging.Logger,
	namespace string,
	metrics prometheus.Registerer,
	maxMessageTimeout time.Duration,
) (*msgBuilder, error) {
	gzipCompressor, err := compression.NewGzipCompressor(constants.DefaultMaxMessageSize)
	if err != nil {
		return nil, err
	}
	zstdCompressor, err := compression.NewZstdCompressor(constants.DefaultMaxMessageSize)
	if err != nil {
		return nil, err
	}

	mb := &msgBuilder{
		log: log,

		gzipCompressor:            gzipCompressor,
		gzipCompressTimeMetrics:   make(map[Op]metric.Averager, len(ExternalOps)),
		gzipDecompressTimeMetrics: make(map[Op]metric.Averager, len(ExternalOps)),

		zstdCompressor:            zstdCompressor,
		zstdCompressTimeMetrics:   make(map[Op]metric.Averager, len(ExternalOps)),
		zstdDecompressTimeMetrics: make(map[Op]metric.Averager, len(ExternalOps)),

		maxMessageTimeout: maxMessageTimeout,
	}

	errs := wrappers.Errs{}
	for _, op := range ExternalOps {
		mb.gzipCompressTimeMetrics[op] = metric.NewAveragerWithErrs(
			namespace,
			fmt.Sprintf("gzip_%s_compress_time", op),
			fmt.Sprintf("time (in ns) to compress %s messages with gzip", op),
			metrics,
			&errs,
		)
		mb.gzipDecompressTimeMetrics[op] = metric.NewAveragerWithErrs(
			namespace,
			fmt.Sprintf("gzip_%s_decompress_time", op),
			fmt.Sprintf("time (in ns) to decompress %s messages with gzip", op),
			metrics,
			&errs,
		)
		mb.zstdCompressTimeMetrics[op] = metric.NewAveragerWithErrs(
			namespace,
			fmt.Sprintf("zstd_%s_compress_time", op),
			fmt.Sprintf("time (in ns) to compress %s messages with zstd", op),
			metrics,
			&errs,
		)
		mb.zstdDecompressTimeMetrics[op] = metric.NewAveragerWithErrs(
			namespace,
			fmt.Sprintf("zstd_%s_decompress_time", op),
			fmt.Sprintf("time (in ns) to decompress %s messages with zstd", op),
			metrics,
			&errs,
		)
	}
	return mb, errs.Err
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
		startTime               = time.Now()
		compressedMsg           p2p.Message
		opToCompressTimeMetrics map[Op]metric.Averager
	)
	switch compressionType {
	case compression.TypeNone:
		return uncompressedMsgBytes, 0, op, nil
	case compression.TypeGzip:
		compressedBytes, err := mb.gzipCompressor.Compress(uncompressedMsgBytes)
		if err != nil {
			return nil, 0, 0, err
		}
		compressedMsg = p2p.Message{
			Message: &p2p.Message_CompressedGzip{
				CompressedGzip: compressedBytes,
			},
		}
		opToCompressTimeMetrics = mb.gzipCompressTimeMetrics
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
		opToCompressTimeMetrics = mb.zstdCompressTimeMetrics
	default:
		return nil, 0, 0, errUnknownCompressionType
	}

	compressedMsgBytes, err := proto.Marshal(&compressedMsg)
	if err != nil {
		return nil, 0, 0, err
	}
	compressTook := time.Since(startTime)

	if compressTimeMetric, ok := opToCompressTimeMetrics[op]; ok {
		compressTimeMetric.Observe(float64(compressTook))
	} else {
		// Should never happen
		mb.log.Warn("no compression metric found for op",
			zap.Stringer("op", op),
			zap.Stringer("compressionType", compressionType),
		)
	}

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
		opToDecompressTimeMetrics map[Op]metric.Averager
		compressor                compression.Compressor
		compressedBytes           []byte
		gzipCompressed            = m.GetCompressedGzip()
		zstdCompressed            = m.GetCompressedZstd()
	)
	switch {
	case len(gzipCompressed) > 0:
		opToDecompressTimeMetrics = mb.gzipDecompressTimeMetrics
		compressor = mb.gzipCompressor
		compressedBytes = gzipCompressed
	case len(zstdCompressed) > 0:
		opToDecompressTimeMetrics = mb.zstdDecompressTimeMetrics
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
	if decompressTimeMetric, ok := opToDecompressTimeMetrics[op]; ok {
		decompressTimeMetric.Observe(float64(decompressTook))
	} else {
		// Should never happen
		mb.log.Warn("no decompression metric found for op",
			zap.Stringer("op", op),
		)
	}

	return m, bytesSavedCompression, op, nil
}

func (mb *msgBuilder) createOutbound(m *p2p.Message, compressionType compression.Type, bypassThrottling bool) (*outboundMessage, error) {
	b, saved, op, err := mb.marshal(m, compressionType)
	if err != nil {
		return nil, err
	}

	return &outboundMessage{
		bypassThrottling:      bypassThrottling,
		op:                    op,
		bytes:                 b,
		bytesSavedCompression: saved,
	}, nil
}

func (mb *msgBuilder) parseInbound(
	bytes []byte,
	nodeID ids.NodeID,
	onFinishedHandling func(),
) (*inboundMessage, error) {
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
		deadline = math.Min(deadline, mb.maxMessageTimeout)
		expiration = time.Now().Add(deadline)
	}

	return &inboundMessage{
		nodeID:                nodeID,
		op:                    op,
		message:               msg,
		expiration:            expiration,
		onFinishedHandling:    onFinishedHandling,
		bytesSavedCompression: bytesSavedCompression,
	}, nil
}
