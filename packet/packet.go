// Package packet implements Bilibili live message framing and compression.
package packet

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/andybalholm/brotli"
)

const (
	Plain = iota
	Popularity
	Zlib
	Brotli
)

const (
	HandShake = iota
	HandShakeResponse
	HeartBeat
	HeartBeatResponse
	_
	Notification
	_
	RoomEnter
	RoomEnterResponse
)

const (
	HeaderLength               = 16
	DefaultMaxDecompressedSize = 16 << 20
	DefaultMaxDepth            = 8
	DefaultMaxPackets          = 65536
)

var (
	ErrInvalidPacket       = errors.New("packet: invalid packet")
	ErrUnsupportedProtocol = errors.New("packet: unsupported protocol")
	ErrDecompressionLimit  = errors.New("packet: decompressed data exceeds limit")
	ErrNestingLimit        = errors.New("packet: compression nesting exceeds limit")
	ErrPacketLimit         = errors.New("packet: packet count exceeds limit")
)

type Packet struct {
	PacketLength    int
	HeaderLength    int
	ProtocolVersion uint16
	Operation       uint32
	SequenceID      int
	Body            []byte
}

func NewPacket(protocolVersion uint16, operation uint32, body []byte) Packet {
	return Packet{
		PacketLength:    HeaderLength + len(body),
		HeaderLength:    HeaderLength,
		ProtocolVersion: protocolVersion,
		Operation:       operation,
		SequenceID:      1,
		Body:            body,
	}
}

// NewPlainPacket constructs an uncompressed control packet. It retains the
// historical protocol version 1; use NewPacket(Plain, ...) for JSON messages.
func NewPlainPacket(operation int, body []byte) Packet {
	return NewPacket(Popularity, uint32(operation), body)
}

// Decode validates exactly one packet. Body shares the input buffer; callers
// that reuse or change data must copy Body first. Extended headers are accepted.
func Decode(data []byte) (Packet, error) {
	if len(data) < HeaderLength {
		return Packet{}, fmt.Errorf("%w: header is truncated (%d bytes)", ErrInvalidPacket, len(data))
	}
	packetLength := binary.BigEndian.Uint32(data[:4])
	if uint64(packetLength) != uint64(len(data)) {
		return Packet{}, fmt.Errorf("%w: declared length %d differs from available %d", ErrInvalidPacket, packetLength, len(data))
	}
	headerLength := int(binary.BigEndian.Uint16(data[4:6]))
	if headerLength < HeaderLength || headerLength > len(data) {
		return Packet{}, fmt.Errorf("%w: invalid header length %d", ErrInvalidPacket, headerLength)
	}
	return Packet{
		PacketLength:    len(data),
		HeaderLength:    headerLength,
		ProtocolVersion: binary.BigEndian.Uint16(data[6:8]),
		Operation:       binary.BigEndian.Uint32(data[8:12]),
		SequenceID:      int(binary.BigEndian.Uint32(data[12:16])),
		Body:            data[headerLength:],
	}, nil
}

// Decoder bounds the work required to expand untrusted compressed frames.
// Zero values use the defaults. MaxDecompressedSize is a cumulative byte budget
// across all compressed packets, including nested ones, in a single call.
// MaxPackets counts both compressed envelopes and uncompressed packets.
// A Decoder can be shared between goroutines if its configuration is not changed.
type Decoder struct {
	MaxDecompressedSize int64
	MaxDepth            int
	MaxPackets          int
}

type decodeState struct {
	remainingBytes   int64
	remainingPackets int
	maxDepth         int
}

func (d Decoder) state() (decodeState, error) {
	if d.MaxDecompressedSize < 0 || d.MaxDepth < 0 || d.MaxPackets < 0 {
		return decodeState{}, errors.New("packet: decoder limits must not be negative")
	}
	if d.MaxDecompressedSize == 0 {
		d.MaxDecompressedSize = DefaultMaxDecompressedSize
	}
	if d.MaxDepth == 0 {
		d.MaxDepth = DefaultMaxDepth
	}
	if d.MaxPackets == 0 {
		d.MaxPackets = DefaultMaxPackets
	}
	return decodeState{d.MaxDecompressedSize, d.MaxPackets, d.MaxDepth}, nil
}

// DecodeFrame decodes all packets in a WebSocket message, recursively expanding
// zlib and Brotli envelopes. An error discards the entire frame, never returning
// partial results. Uncompressed packet bodies share data's backing buffer.
func DecodeFrame(data []byte) ([]Packet, error) {
	return (Decoder{}).DecodeFrame(data)
}

func (d Decoder) DecodeFrame(data []byte) ([]Packet, error) {
	state, err := d.state()
	if err != nil {
		return nil, err
	}
	var packets []Packet
	if err := state.decode(data, 0, &packets); err != nil {
		return nil, err
	}
	return packets, nil
}

func (s *decodeState) decode(data []byte, depth int, packets *[]Packet) error {
	for offset := 0; offset < len(data); {
		remaining := data[offset:]
		if len(remaining) < HeaderLength {
			return fmt.Errorf("%w: truncated header at offset %d", ErrInvalidPacket, offset)
		}
		length := binary.BigEndian.Uint32(remaining[:4])
		if length < HeaderLength || uint64(length) > uint64(len(remaining)) {
			return fmt.Errorf("%w: invalid length %d at offset %d with %d bytes remaining", ErrInvalidPacket, length, offset, len(remaining))
		}
		p, err := Decode(remaining[:int(length)])
		if err != nil {
			return fmt.Errorf("packet at offset %d: %w", offset, err)
		}
		if err := s.expand(p, depth, packets); err != nil {
			return fmt.Errorf("packet at offset %d: %w", offset, err)
		}
		offset += int(length)
	}
	return nil
}

func (s *decodeState) expand(p Packet, depth int, packets *[]Packet) error {
	if s.remainingPackets == 0 {
		return ErrPacketLimit
	}
	s.remainingPackets--
	switch p.ProtocolVersion {
	case Plain, Popularity:
		*packets = append(*packets, p)
		return nil
	case Zlib, Brotli:
		if depth >= s.maxDepth {
			return ErrNestingLimit
		}
		body, err := decompress(p.ProtocolVersion, p.Body, s.remainingBytes)
		if err != nil {
			return err
		}
		s.remainingBytes -= int64(len(body))
		return s.decode(body, depth+1, packets)
	default:
		return fmt.Errorf("%w: %d", ErrUnsupportedProtocol, p.ProtocolVersion)
	}
}

func decompress(protocol uint16, body []byte, limit int64) ([]byte, error) {
	var reader io.Reader
	if protocol == Zlib {
		zr, err := zlib.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("packet: zlib reader: %w", err)
		}
		defer zr.Close()
		reader = zr
	} else {
		reader = brotli.NewReader(bytes.NewReader(body))
	}
	// Read at most the remaining budget, then probe for one extra byte. This
	// avoids overflowing limit+1 when the caller chooses MaxInt64 as a limit.
	decoded, err := io.ReadAll(io.LimitReader(reader, limit))
	if err != nil {
		return nil, fmt.Errorf("packet: decompress protocol %d: %w", protocol, err)
	}
	var extra [1]byte
	if n, err := io.ReadFull(reader, extra[:]); n != 0 {
		return nil, ErrDecompressionLimit
	} else if err != nil && err != io.EOF {
		return nil, fmt.Errorf("packet: decompress protocol %d: %w", protocol, err)
	}
	return decoded, nil
}

// ParseWithError expands one already decoded packet using the default limits.
func (p Packet) ParseWithError() ([]Packet, error) {
	state, _ := (Decoder{}).state()
	var packets []Packet
	if err := state.expand(p, 0, &packets); err != nil {
		return nil, err
	}
	return packets, nil
}

// NewPacketFromBytes is the legacy decoder. It returns a zero Packet on invalid
// input. New callers should use Decode to receive the validation error.
func NewPacketFromBytes(data []byte) Packet {
	p, _ := Decode(data)
	return p
}

// Parse is the legacy expansion API. It returns nil on invalid data; use
// ParseWithError or DecodeFrame to receive the error.
func (p Packet) Parse() []Packet {
	packets, _ := p.ParseWithError()
	return packets
}

func (p *Packet) Unmarshal(v any) error {
	return json.Unmarshal(p.Body, v)
}

// MarshalBinary serializes a packet using the standard 16-byte header and
// sequence ID 1, matching the service's client packet format.
func (p Packet) MarshalBinary() ([]byte, error) {
	if uint64(len(p.Body)) > uint64(^uint32(0))-HeaderLength {
		return nil, fmt.Errorf("%w: body too large", ErrInvalidPacket)
	}
	raw := make([]byte, HeaderLength+len(p.Body))
	binary.BigEndian.PutUint32(raw[:4], uint32(len(raw)))
	binary.BigEndian.PutUint16(raw[4:6], HeaderLength)
	binary.BigEndian.PutUint16(raw[6:8], p.ProtocolVersion)
	binary.BigEndian.PutUint32(raw[8:12], p.Operation)
	binary.BigEndian.PutUint32(raw[12:16], 1)
	copy(raw[HeaderLength:], p.Body)
	return raw, nil
}

// Build serializes the packet, returning nil if it exceeds the wire size limit.
func (p *Packet) Build() []byte {
	raw, _ := p.MarshalBinary()
	return raw
}

// DecodePacket is an alias of NewPacketFromBytes retained for compatibility.
func DecodePacket(data []byte) Packet {
	return NewPacketFromBytes(data)
}

func EncodePacket(packet Packet) []byte {
	return packet.Build()
}

// Slice splits an uncompressed sequence without expanding compression. It
// returns nil on invalid data. Use DecodeFrame for error reporting and expansion.
func Slice(data []byte) []Packet {
	var packets []Packet
	for len(data) > 0 {
		if len(data) < HeaderLength || len(packets) >= DefaultMaxPackets {
			return nil
		}
		length := binary.BigEndian.Uint32(data[:4])
		if length < HeaderLength || uint64(length) > uint64(len(data)) {
			return nil
		}
		p, err := Decode(data[:int(length)])
		if err != nil {
			return nil
		}
		packets = append(packets, p)
		data = data[int(length):]
	}
	return packets
}
