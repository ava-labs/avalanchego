// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
)

var (
	_ InboundMessage  = &inboundMessageWithPacker{}
	_ InboundMessage  = &inboundMessageWithProto{}
	_ OutboundMessage = &outboundMessageWithPacker{}
	_ OutboundMessage = &outboundMessageWithProto{}

	errUnknownMessageTypeForOp = errors.New("unknown message type for Op")
	errUnexpectedCompressedOp  = errors.New("unexpected compressed Op")

	errInvalidIPAddrLen = errors.New("invalid IP address field length (expected 16-byte)")
	errInvalidCert      = errors.New("invalid TLS certificate field")
)

// InboundMessage represents a set of fields for an inbound message that can be serialized into a byte stream
type InboundMessage interface {
	fmt.Stringer

	BytesSavedCompression() int
	Op() Op
	Get(Field) (interface{}, error)
	NodeID() ids.NodeID
	ExpirationTime() time.Time
	OnFinishedHandling()
}

type inboundMessage struct {
	op                    Op
	bytesSavedCompression int
	nodeID                ids.NodeID
	expirationTime        time.Time
	onFinishedHandling    func()
}

// Op returns the value of the specified operation in this message
func (inMsg *inboundMessage) Op() Op { return inMsg.op }

// BytesSavedCompression returns the number of bytes this message saved due to
// compression. That is, the number of bytes we did not receive over the
// network due to the message being compressed. 0 for messages that were not
// compressed.
func (inMsg *inboundMessage) BytesSavedCompression() int {
	return inMsg.bytesSavedCompression
}

// NodeID returns the node that the msg was sent by.
func (inMsg *inboundMessage) NodeID() ids.NodeID { return inMsg.nodeID }

// ExpirationTime returns the time this message doesn't need to be responded to.
// A zero time means message does not expire.
func (inMsg *inboundMessage) ExpirationTime() time.Time { return inMsg.expirationTime }

// OnFinishedHandling is the function to be called once inboundMessage is
// complete.
func (inMsg *inboundMessage) OnFinishedHandling() {
	if inMsg.onFinishedHandling != nil {
		inMsg.onFinishedHandling()
	}
}

type inboundMessageWithPacker struct {
	inboundMessage

	fields map[Field]interface{}
}

// Field returns the value of the specified field in this message
func (inMsg *inboundMessageWithPacker) Get(field Field) (interface{}, error) {
	value, ok := inMsg.fields[field]
	if !ok {
		return nil, fmt.Errorf("%w: %s", errMissingField, field)
	}
	return value, nil
}

func (inMsg *inboundMessageWithPacker) String() string {
	sb := strings.Builder{}
	sb.WriteString(fmt.Sprintf("(Op: %s, NodeID: %s", inMsg.op, inMsg.nodeID))
	if requestIDIntf, exists := inMsg.fields[RequestID]; exists {
		sb.WriteString(fmt.Sprintf(", RequestID: %d", requestIDIntf.(uint32)))
	}
	if !inMsg.expirationTime.IsZero() {
		sb.WriteString(fmt.Sprintf(", Deadline: %d", inMsg.expirationTime.Unix()))
	}
	switch inMsg.op {
	case GetAccepted, Accepted, Chits, AcceptedFrontier:
		sb.WriteString(fmt.Sprintf(", NumContainerIDs: %d)", len(inMsg.fields[ContainerIDs].([][]byte))))
	case Get, GetAncestors, PullQuery:
		sb.WriteString(fmt.Sprintf(", ContainerID: 0x%x)", inMsg.fields[ContainerID].([]byte)))
	case Ancestors:
		sb.WriteString(fmt.Sprintf(", NumContainers: %d)", len(inMsg.fields[MultiContainerBytes].([][]byte))))
	case Notify:
		sb.WriteString(fmt.Sprintf(", Notification: %d)", inMsg.fields[VMMessage].(uint32)))
	case AppRequest, AppResponse, AppGossip:
		sb.WriteString(fmt.Sprintf(", len(AppMsg): %d)", len(inMsg.fields[AppBytes].([]byte))))
	default:
		sb.WriteString(")")
	}

	return sb.String()
}

type inboundMessageWithProto struct {
	inboundMessage

	msg *p2ppb.Message
}

func (inMsg *inboundMessageWithProto) String() string {
	return inMsg.msg.String()
}

func (inMsg *inboundMessageWithProto) Get(field Field) (interface{}, error) {
	return getField(inMsg.msg, field)
}

// TODO: once protobuf-based p2p messaging is fully activated,
// move the semantic checks out of this package
func getField(m *p2ppb.Message, field Field) (interface{}, error) {
	switch m.GetMessage().(type) {
	case *p2ppb.Message_Pong:
		msg := m.GetPong()
		if field == Uptime {
			// the original packer-based pong base uses uint8
			return uint8(msg.UptimePct), nil
		}

	case *p2ppb.Message_Version:
		msg := m.GetVersion()
		switch field {
		case NetworkID:
			return msg.NetworkId, nil
		case MyTime:
			return msg.MyTime, nil
		case IP:
			// "net.IP" type in Golang is 16-byte
			// regardless of whether it's IPV4 or 6 (see net.IPv6len)
			// however, proto message does not enforce the length
			// so we need to verify here
			// TODO: once we complete the migration
			// move this semantic verification outside of this package
			if len(msg.IpAddr) != net.IPv6len {
				return nil, fmt.Errorf(
					"%w: invalid IP address length %d in version message",
					errInvalidIPAddrLen,
					len(msg.IpAddr),
				)
			}
			return ips.IPPort{
				IP:   net.IP(msg.IpAddr),
				Port: uint16(msg.IpPort),
			}, nil
		case VersionStr:
			return msg.MyVersion, nil
		case VersionTime:
			return msg.MyVersionTime, nil
		case SigBytes:
			return msg.Sig, nil
		case TrackedSubnets:
			return msg.TrackedSubnets, nil
		}

	case *p2ppb.Message_PeerList:
		msg := m.GetPeerList()
		if field == Peers {
			peers := make([]ips.ClaimedIPPort, len(msg.GetClaimedIpPorts()))
			for i, p := range msg.GetClaimedIpPorts() {
				tlsCert, err := x509.ParseCertificate(p.X509Certificate)
				if err != nil {
					// this certificate is different than the certificate received
					// during the TLS handshake (and so this error can occur)
					return nil, fmt.Errorf(
						"%w: failed to parse peer certificate for peer_list message (%v)",
						errInvalidCert,
						err,
					)
				}
				// TODO: once we complete the migration
				// move this semantic verification outside of this package
				if len(p.IpAddr) != net.IPv6len {
					return nil, fmt.Errorf(
						"%w: invalid IP address length %d in peer_list message",
						errInvalidIPAddrLen,
						len(p.IpAddr),
					)
				}
				peers[i] = ips.ClaimedIPPort{
					Cert: tlsCert,
					IPPort: ips.IPPort{
						IP:   net.IP(p.IpAddr),
						Port: uint16(p.IpPort),
					},
					Timestamp: p.Timestamp,
					Signature: p.Signature,
				}
			}
			return peers, nil
		}

	case *p2ppb.Message_GetStateSummaryFrontier:
		msg := m.GetGetStateSummaryFrontier()
		switch field {
		case ChainID:
			return msg.ChainId, nil
		case RequestID:
			return msg.RequestId, nil
		case Deadline:
			return msg.Deadline, nil
		}

	case *p2ppb.Message_StateSummaryFrontier_:
		msg := m.GetStateSummaryFrontier_()
		switch field {
		case ChainID:
			return msg.ChainId, nil
		case RequestID:
			return msg.RequestId, nil
		case SummaryBytes:
			return msg.Summary, nil
		}

	case *p2ppb.Message_GetAcceptedStateSummary:
		msg := m.GetGetAcceptedStateSummary()
		switch field {
		case ChainID:
			return msg.ChainId, nil
		case RequestID:
			return msg.RequestId, nil
		case Deadline:
			return msg.Deadline, nil
		case SummaryHeights:
			return msg.Heights, nil
		}

	case *p2ppb.Message_AcceptedStateSummary_:
		msg := m.GetAcceptedStateSummary_()
		switch field {
		case ChainID:
			return msg.ChainId, nil
		case RequestID:
			return msg.RequestId, nil
		case SummaryIDs:
			return msg.SummaryIds, nil
		}

	case *p2ppb.Message_GetAcceptedFrontier:
		msg := m.GetGetAcceptedFrontier()
		switch field {
		case ChainID:
			return msg.ChainId, nil
		case RequestID:
			return msg.RequestId, nil
		case Deadline:
			return msg.Deadline, nil
		}

	case *p2ppb.Message_AcceptedFrontier_:
		msg := m.GetAcceptedFrontier_()
		switch field {
		case ChainID:
			return msg.ChainId, nil
		case RequestID:
			return msg.RequestId, nil
		case ContainerIDs:
			return msg.ContainerIds, nil
		}

	case *p2ppb.Message_GetAccepted:
		msg := m.GetGetAccepted()
		switch field {
		case ChainID:
			return msg.ChainId, nil
		case RequestID:
			return msg.RequestId, nil
		case Deadline:
			return msg.Deadline, nil
		case ContainerIDs:
			return msg.ContainerIds, nil
		}

	case *p2ppb.Message_Accepted_:
		msg := m.GetAccepted_()
		switch field {
		case ChainID:
			return msg.ChainId, nil
		case RequestID:
			return msg.RequestId, nil
		case ContainerIDs:
			return msg.ContainerIds, nil
		}

	case *p2ppb.Message_GetAncestors:
		msg := m.GetGetAncestors()
		switch field {
		case ChainID:
			return msg.ChainId, nil
		case RequestID:
			return msg.RequestId, nil
		case Deadline:
			return msg.Deadline, nil
		case ContainerID:
			return msg.ContainerId, nil
		}

	case *p2ppb.Message_Ancestors_:
		msg := m.GetAncestors_()
		switch field {
		case ChainID:
			return msg.ChainId, nil
		case RequestID:
			return msg.RequestId, nil
		case MultiContainerBytes:
			return msg.Containers, nil
		}

	case *p2ppb.Message_Get:
		msg := m.GetGet()
		switch field {
		case ChainID:
			return msg.ChainId, nil
		case RequestID:
			return msg.RequestId, nil
		case Deadline:
			return msg.Deadline, nil
		case ContainerID:
			return msg.ContainerId, nil
		}

	case *p2ppb.Message_Put:
		msg := m.GetPut()
		switch field {
		case ChainID:
			return msg.ChainId, nil
		case RequestID:
			return msg.RequestId, nil
		case ContainerBytes:
			return msg.Container, nil
		}

	case *p2ppb.Message_PushQuery:
		msg := m.GetPushQuery()
		switch field {
		case ChainID:
			return msg.ChainId, nil
		case RequestID:
			return msg.RequestId, nil
		case Deadline:
			return msg.Deadline, nil
		case ContainerBytes:
			return msg.Container, nil
		}

	case *p2ppb.Message_PullQuery:
		msg := m.GetPullQuery()
		switch field {
		case ChainID:
			return msg.ChainId, nil
		case RequestID:
			return msg.RequestId, nil
		case Deadline:
			return msg.Deadline, nil
		case ContainerID:
			return msg.ContainerId, nil
		}

	case *p2ppb.Message_Chits:
		msg := m.GetChits()
		switch field {
		case ChainID:
			return msg.ChainId, nil
		case RequestID:
			return msg.RequestId, nil
		case ContainerIDs:
			return msg.ContainerIds, nil
		}

	case *p2ppb.Message_AppRequest:
		msg := m.GetAppRequest()
		switch field {
		case ChainID:
			return msg.ChainId, nil
		case RequestID:
			return msg.RequestId, nil
		case Deadline:
			return msg.Deadline, nil
		case AppBytes:
			return msg.AppBytes, nil
		}

	case *p2ppb.Message_AppResponse:
		msg := m.GetAppResponse()
		switch field {
		case ChainID:
			return msg.ChainId, nil
		case RequestID:
			return msg.RequestId, nil
		case AppBytes:
			return msg.AppBytes, nil
		}

	case *p2ppb.Message_AppGossip:
		msg := m.GetAppGossip()
		switch field {
		case ChainID:
			return msg.ChainId, nil
		case AppBytes:
			return msg.AppBytes, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", errMissingField, field)
}

// OutboundMessage represents a set of fields for an outbound message that can
// be serialized into a byte stream
type OutboundMessage interface {
	BytesSavedCompression() int
	Bytes() []byte
	Op() Op
	BypassThrottling() bool

	AddRef()
	DecRef()

	IsProto() bool
}

type outboundMessage struct {
	op                    Op
	bytes                 []byte
	bytesSavedCompression int
	bypassThrottling      bool
}

// Op returns the value of the specified operation in this message
func (outMsg *outboundMessage) Op() Op { return outMsg.op }

// Bytes returns this message in bytes
func (outMsg *outboundMessage) Bytes() []byte { return outMsg.bytes }

// BytesSavedCompression returns the number of bytes this message saved due to
// compression. That is, the number of bytes we did not send over the
// network due to the message being compressed. 0 for messages that were not
// compressed.
func (outMsg *outboundMessage) BytesSavedCompression() int {
	return outMsg.bytesSavedCompression
}

// BypassThrottling when attempting to send this message
func (outMsg *outboundMessage) BypassThrottling() bool { return outMsg.bypassThrottling }

type outboundMessageWithPacker struct {
	outboundMessage

	refLock sync.Mutex
	refs    int
	c       *codec
}

func (outMsg *outboundMessageWithPacker) AddRef() {
	outMsg.refLock.Lock()
	defer outMsg.refLock.Unlock()

	outMsg.refs++
}

// Once the reference count of this message goes to 0, the byte slice should not
// be inspected.
func (outMsg *outboundMessageWithPacker) DecRef() {
	outMsg.refLock.Lock()
	defer outMsg.refLock.Unlock()

	outMsg.refs--
	if outMsg.refs == 0 {
		outMsg.c.byteSlicePool.Put(outMsg.bytes)
	}
}

func (outMsg *outboundMessageWithPacker) IsProto() bool { return false }

type outboundMessageWithProto struct {
	outboundMessage

	msg *p2ppb.Message
}

func (outMsg *outboundMessageWithProto) AddRef()       {}
func (outMsg *outboundMessageWithProto) DecRef()       {}
func (outMsg *outboundMessageWithProto) IsProto() bool { return true }

// TODO: add other compression algorithms with extended interface
type msgBuilderProtobuf struct {
	gzipCompressor compression.Compressor
	clock          mockable.Clock

	compressTimeMetrics   map[Op]metric.Averager
	decompressTimeMetrics map[Op]metric.Averager

	maxMessageTimeout time.Duration
}

// NOTE: the metrics registration paths are the same as "NewCodecWithMemoryPool"!
// To avoid conflicts, use the different namespace if created at the same time.
func newMsgBuilderProtobuf(namespace string, metrics prometheus.Registerer, maxMessageSize int64, maxMessageTimeout time.Duration) (*msgBuilderProtobuf, error) {
	cpr, err := compression.NewGzipCompressor(maxMessageSize)
	if err != nil {
		return nil, err
	}

	mb := &msgBuilderProtobuf{
		gzipCompressor: cpr,

		compressTimeMetrics:   make(map[Op]metric.Averager, len(ExternalOps)),
		decompressTimeMetrics: make(map[Op]metric.Averager, len(ExternalOps)),

		maxMessageTimeout: maxMessageTimeout,
	}

	errs := wrappers.Errs{}
	for _, op := range ExternalOps {
		if !op.Compressible() {
			continue
		}

		mb.compressTimeMetrics[op] = metric.NewAveragerWithErrs(
			namespace,
			fmt.Sprintf("%s_compress_time", op),
			fmt.Sprintf("time (in ns) to compress %s messages", op),
			metrics,
			&errs,
		)
		mb.decompressTimeMetrics[op] = metric.NewAveragerWithErrs(
			namespace,
			fmt.Sprintf("%s_decompress_time", op),
			fmt.Sprintf("time (in ns) to decompress %s messages", op),
			metrics,
			&errs,
		)
	}
	return mb, errs.Err
}

// NOTE THAT the passed message must be verified beforehand.
// NOTE THAT the passed message will be modified if compression is enabled.
// TODO: find a way to not in-place modify the message
func (mb *msgBuilderProtobuf) marshal(m *p2ppb.Message, gzipCompress bool) ([]byte, int, time.Duration, error) {
	uncompressedMsgBytes, err := proto.Marshal(m)
	if err != nil {
		return nil, 0, 0, err
	}

	if !gzipCompress {
		return uncompressedMsgBytes, 0, 0, nil
	}

	// If compression is enabled, we marshal twice:
	// 1. the original message
	// 2. the message with compressed bytes
	//
	// This recursive packing allows us to avoid an extra compression on/off
	// field in the message.
	startTime := time.Now()
	compressedBytes, err := mb.gzipCompressor.Compress(uncompressedMsgBytes)
	if err != nil {
		return nil, 0, 0, err
	}
	compressTook := time.Since(startTime)

	// Original message can be discarded for the compressed message.
	m.Message = &p2ppb.Message_CompressedGzip{
		CompressedGzip: compressedBytes,
	}
	compressedMsgBytes, err := proto.Marshal(m)
	if err != nil {
		return nil, 0, 0, err
	}

	bytesSaved := len(uncompressedMsgBytes) - len(compressedMsgBytes)
	return compressedMsgBytes, bytesSaved, compressTook, nil
}

func (mb *msgBuilderProtobuf) unmarshal(b []byte) (Op, *p2ppb.Message, bool, int, time.Duration, error) {
	m := new(p2ppb.Message)
	if err := proto.Unmarshal(b, m); err != nil {
		return 0, nil, false, 0, 0, err
	}

	compressed := m.GetCompressedGzip()
	if len(compressed) == 0 {
		// The message wasn't compressed
		op, err := msgToOp(m)
		return op, m, false, 0, 0, err
	}

	startTime := time.Now()
	decompressed, err := mb.gzipCompressor.Decompress(compressed)
	if err != nil {
		return 0, nil, true, 0, 0, err
	}
	decompressTook := time.Since(startTime)

	if err := proto.Unmarshal(decompressed, m); err != nil {
		return 0, nil, true, 0, 0, err
	}

	op, err := msgToOp(m)
	if err != nil {
		return 0, nil, true, 0, 0, err
	}
	if !op.Compressible() {
		return 0, nil, true, 0, 0, errUnexpectedCompressedOp
	}

	bytesSavedCompression := len(decompressed) - len(compressed)
	return op, m, true, bytesSavedCompression, decompressTook, nil
}

func msgToOp(m *p2ppb.Message) (Op, error) {
	switch m.GetMessage().(type) {
	case *p2ppb.Message_Ping:
		return Ping, nil
	case *p2ppb.Message_Pong:
		return Pong, nil
	case *p2ppb.Message_Version:
		return Version, nil
	case *p2ppb.Message_PeerList:
		return PeerList, nil
	case *p2ppb.Message_GetStateSummaryFrontier:
		return GetStateSummaryFrontier, nil
	case *p2ppb.Message_StateSummaryFrontier_:
		return StateSummaryFrontier, nil
	case *p2ppb.Message_GetAcceptedStateSummary:
		return GetAcceptedStateSummary, nil
	case *p2ppb.Message_AcceptedStateSummary_:
		return AcceptedStateSummary, nil
	case *p2ppb.Message_GetAcceptedFrontier:
		return GetAcceptedFrontier, nil
	case *p2ppb.Message_AcceptedFrontier_:
		return AcceptedFrontier, nil
	case *p2ppb.Message_GetAccepted:
		return GetAccepted, nil
	case *p2ppb.Message_Accepted_:
		return Accepted, nil
	case *p2ppb.Message_GetAncestors:
		return GetAncestors, nil
	case *p2ppb.Message_Ancestors_:
		return Ancestors, nil
	case *p2ppb.Message_Get:
		return Get, nil
	case *p2ppb.Message_Put:
		return Put, nil
	case *p2ppb.Message_PushQuery:
		return PushQuery, nil
	case *p2ppb.Message_PullQuery:
		return PullQuery, nil
	case *p2ppb.Message_Chits:
		return Chits, nil
	case *p2ppb.Message_AppRequest:
		return AppRequest, nil
	case *p2ppb.Message_AppResponse:
		return AppResponse, nil
	case *p2ppb.Message_AppGossip:
		return AppGossip, nil
	default:
		return 0, fmt.Errorf("%w: unknown message %T", errUnknownMessageTypeForOp, m.GetMessage())
	}
}

// NOTE THAT the passed message will be updated if compression is enabled.
// TODO: find a way to not in-place modify the message
func (mb *msgBuilderProtobuf) createOutbound(op Op, msg *p2ppb.Message, gzipCompress bool, bypassThrottling bool) (*outboundMessageWithProto, error) {
	b, saved, compressTook, err := mb.marshal(msg, gzipCompress)
	if err != nil {
		return nil, err
	}
	if gzipCompress {
		mb.compressTimeMetrics[op].Observe(float64(compressTook))
	}

	return &outboundMessageWithProto{
		outboundMessage: outboundMessage{
			op:                    op,
			bytes:                 b,
			bytesSavedCompression: saved,
			bypassThrottling:      bypassThrottling,
		},
		msg: msg,
	}, nil
}

func (mb *msgBuilderProtobuf) parseInbound(bytes []byte, nodeID ids.NodeID, onFinishedHandling func()) (*inboundMessageWithProto, error) {
	op, m, wasCompressed, bytesSavedCompression, decompressTook, err := mb.unmarshal(bytes)
	if err != nil {
		return nil, err
	}
	if wasCompressed {
		mb.decompressTimeMetrics[op].Observe(float64(decompressTook))
	}

	var expirationTime time.Time
	if deadline, err := getField(m, Deadline); err == nil {
		deadlineDuration := time.Duration(deadline.(uint64))
		if deadlineDuration > mb.maxMessageTimeout {
			deadlineDuration = mb.maxMessageTimeout
		}
		expirationTime = mb.clock.Time().Add(deadlineDuration)
	}

	return &inboundMessageWithProto{
		inboundMessage: inboundMessage{
			op:                    op,
			bytesSavedCompression: bytesSavedCompression,
			nodeID:                nodeID,
			expirationTime:        expirationTime,
			onFinishedHandling:    onFinishedHandling,
		},
		msg: m,
	}, nil
}
