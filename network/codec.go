// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	errMissingField      = errors.New("message missing field")
	errBadOp             = errors.New("input field has invalid operation")
	errCompressNeedsFlag = errors.New("compressed message requires isCompressed flag")
)

// codec defines the serialization and deserialization of network messages.
// It's safe for multiple goroutines to call Pack and Parse concurrently.
type codec struct {
	// [bytesSavedMetrics] must not be nil.
	// Should only be written to on codec creation.
	bytesSavedMetrics     map[Op]prometheus.Histogram
	compressTimeMetrics   map[Op]prometheus.Histogram
	decompressTimeMetrics map[Op]prometheus.Histogram
	// [compressor] must not be nil
	compressor compression.Compressor
}

// If this method returns an error, the returned codec may still be used.
// However, some metrics may not be registered with [metricsRegisterer].
func newCodec(metricsRegisterer prometheus.Registerer) (codec, error) {
	compressor, err := compression.NewGzipCompressor()
	if err != nil {
		return codec{}, fmt.Errorf("couldn't create compressor: %s", err)
	}
	c := codec{
		bytesSavedMetrics:     make(map[Op]prometheus.Histogram, len(ops)),
		compressTimeMetrics:   make(map[Op]prometheus.Histogram, len(ops)),
		decompressTimeMetrics: make(map[Op]prometheus.Histogram, len(ops)),
		compressor:            compressor,
	}
	errs := wrappers.Errs{}
	for _, op := range ops {
		c.bytesSavedMetrics[op] = prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.PlatformName,
			Name:      fmt.Sprintf("%s_bytes_saved", op),
			Help:      fmt.Sprintf("Number of bytes saved (not sent) due to compression of %s messages", op),
		})
		c.compressTimeMetrics[op] = prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.PlatformName,
			Name:      fmt.Sprintf("%s_compress_time", op),
			Help:      fmt.Sprintf("Time (in ns) to compress %s messages", op),
		})
		c.decompressTimeMetrics[op] = prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.PlatformName,
			Name:      fmt.Sprintf("%s_decompress_time", op),
			Help:      fmt.Sprintf("Time (in ns) to decompress %s messages", op),
		})
		errs.Add(metricsRegisterer.Register(c.bytesSavedMetrics[op]))
		errs.Add(metricsRegisterer.Register(c.compressTimeMetrics[op]))
		errs.Add(metricsRegisterer.Register(c.decompressTimeMetrics[op]))
	}
	return c, errs.Err
}

// Pack attempts to pack a map of fields into a message.
// The first byte of the message is the opcode of the message.
// Uses [buffer] to hold the message's byte repr.
// [buffer]'s contents may be overwritten by this method.
// [buffer] may be nil.
// If [includeIsCompressedFlag], include a flag that marks whether the payload
// is compressed or not.
// If [compress] and [includeIsCompressedFlag], compress the payload.
// If [compress] == true, [includeIsCompressedFlag] must be true
// TODO remove [includeIsCompressedFlag] after network upgrade.
func (c codec) Pack(
	buffer []byte,
	op Op,
	fieldValues map[Field]interface{},
	includeIsCompressedFlag bool,
	compress bool,
) (Msg, error) {
	if compress && !includeIsCompressedFlag {
		return nil, errCompressNeedsFlag
	}
	msgFields, ok := Messages[op]
	if !ok {
		return nil, errBadOp
	}

	p := wrappers.Packer{
		MaxSize: math.MaxInt32,
		Bytes:   buffer[:0],
	}
	// Pack the op code (message type)
	p.PackByte(byte(op))

	// Optionally, pack whether the payload is compressed
	if includeIsCompressedFlag {
		p.PackBool(compress)
	}

	// Pack the uncompressed payload
	for _, field := range msgFields {
		data, ok := fieldValues[field]
		if !ok {
			return nil, errMissingField
		}
		field.Packer()(&p, data)
	}
	if p.Err != nil {
		return nil, p.Err
	}
	msg := &msg{
		op:     op,
		fields: fieldValues,
		bytes:  p.Bytes,
	}
	if !compress {
		return msg, nil
	}

	// If [compress], compress the payload (not the op code, not isCompressed).
	// The slice below is guaranteed to be in-bounds because [p.Err] == nil
	// implies that len(msg.bytes) >= 2
	payloadBytes := msg.bytes[wrappers.BoolLen+wrappers.ByteLen:]
	startTime := time.Now()
	compressedPayloadBytes, err := c.compressor.Compress(payloadBytes)
	if err != nil {
		return nil, fmt.Errorf("couldn't compress payload of %s message: %s", op, err)
	}
	c.compressTimeMetrics[op].Observe(float64(time.Since(startTime)))
	bytesSaved := len(payloadBytes) - len(compressedPayloadBytes) // may be negative
	c.bytesSavedMetrics[op].Observe(float64(bytesSaved))
	// Remove the uncompressed payload (keep just the message type and isCompressed)
	msg.bytes = msg.bytes[:wrappers.BoolLen+wrappers.ByteLen]
	// Attach the compressed payload
	msg.bytes = append(msg.bytes, compressedPayloadBytes...)
	return msg, nil
}

// Parse attempts to convert bytes into a message.
// The first byte of the message is the opcode of the message.
// If [parseIsCompressedFlag], try to parse the flag that indicates
// whether the message payload is compressed. Should only be true
// if we expect this peer to send us compressed messages.
// TODO remove [parseIsCompressedFlag] after network upgrade
func (c codec) Parse(bytes []byte, parseIsCompressedFlag bool) (Msg, error) {
	p := wrappers.Packer{Bytes: bytes}

	// Unpack the op code (message type)
	op := Op(p.UnpackByte())

	msgFields, ok := Messages[op]
	if !ok { // Unknown message type
		return nil, errBadOp
	}

	// See if messages of this type may be compressed
	compressed := false
	if parseIsCompressedFlag {
		compressed = p.UnpackBool()
	}
	if p.Err != nil {
		return nil, p.Err
	}

	// If the payload is compressed, decompress it
	if compressed {
		// The slice below is guaranteed to be in-bounds because [p.Err] == nil
		compressedPayloadBytes := p.Bytes[wrappers.ByteLen+wrappers.BoolLen:]
		startTime := time.Now()
		payloadBytes, err := c.compressor.Decompress(compressedPayloadBytes)
		if err != nil {
			return nil, fmt.Errorf("couldn't decompress payload of %s message: %s", op, err)
		}
		c.decompressTimeMetrics[op].Observe(float64(time.Since(startTime)))
		// Replace the compressed payload with the decompressed payload.
		// Remove the compressed payload and isCompressed; keep just the message type
		p.Bytes = p.Bytes[:wrappers.ByteLen]
		// Rewind offset by 1 because we removed the bool flag
		// since the data now is uncompressed
		p.Offset -= wrappers.BoolLen
		// Attach the decompressed payload.
		p.Bytes = append(p.Bytes, payloadBytes...)
	}

	// Parse each field of the payload
	fieldValues := make(map[Field]interface{}, len(msgFields))
	for _, field := range msgFields {
		fieldValues[field] = field.Unpacker()(&p)
	}

	if p.Offset != len(p.Bytes) {
		return nil, fmt.Errorf("expected length %d but got %d", len(p.Bytes), p.Offset)
	}

	return &msg{
		op:     op,
		fields: fieldValues,
		bytes:  p.Bytes,
	}, p.Err
}
