package packet

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/andybalholm/brotli"
)

func encoded(protocol uint16, operation uint32, body string) []byte {
	p := NewPacket(protocol, operation, []byte(body))
	return p.Build()
}

func compressed(t testing.TB, protocol uint16, body []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if protocol == Zlib {
		writer := zlib.NewWriter(&buffer)
		if _, err := writer.Write(body); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		writer := brotli.NewWriter(&buffer)
		if _, err := writer.Write(body); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	p := NewPacket(protocol, Notification, buffer.Bytes())
	return p.Build()
}

func TestDecode(t *testing.T) {
	raw := encoded(Plain, Notification, `{"cmd":"LIVE"}`)
	p, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.PacketLength != len(raw) || p.HeaderLength != HeaderLength || p.SequenceID != 1 || p.ProtocolVersion != Plain || p.Operation != Notification {
		t.Fatalf("incorrect header: %+v", p)
	}
	if string(p.Body) != `{"cmd":"LIVE"}` {
		t.Fatalf("unexpected body: %s", p.Body)
	}
	if &p.Body[0] != &raw[HeaderLength] {
		t.Fatal("Decode should return a view of the original body")
	}
	if !bytes.Equal(p.Build(), raw) {
		t.Fatal("roundtrip did not preserve packet")
	}
}

func TestDecodeExtendedHeader(t *testing.T) {
	raw := encoded(Plain, Notification, "body")
	raw = append(raw[:HeaderLength], append([]byte{1, 2, 3, 4}, raw[HeaderLength:]...)...)
	binary.BigEndian.PutUint32(raw[:4], uint32(len(raw)))
	binary.BigEndian.PutUint16(raw[4:6], HeaderLength+4)
	binary.BigEndian.PutUint32(raw[12:16], 42)
	p, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.HeaderLength != 20 || p.SequenceID != 42 || string(p.Body) != "body" {
		t.Fatalf("extended header was not honored: %+v", p)
	}
}

func TestDecodeInvalidHeader(t *testing.T) {
	for n := 0; n < HeaderLength; n++ {
		t.Run(fmt.Sprintf("truncated_%d", n), func(t *testing.T) {
			if _, err := Decode(make([]byte, n)); !errors.Is(err, ErrInvalidPacket) {
				t.Fatalf("Decode returned %v", err)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		length uint32
		header uint16
	}{
		{"zero_length", 0, HeaderLength},
		{"short_length", HeaderLength - 1, HeaderLength},
		{"excess_length", 100, HeaderLength},
		{"max_length", ^uint32(0), HeaderLength},
		{"short_header", HeaderLength, HeaderLength - 1},
		{"excess_header", HeaderLength, HeaderLength + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := NewHeartBeatPacket()
			binary.BigEndian.PutUint32(raw[:4], tc.length)
			binary.BigEndian.PutUint16(raw[4:6], tc.header)
			if _, err := Decode(raw); !errors.Is(err, ErrInvalidPacket) {
				t.Fatalf("Decode returned %v", err)
			}
			if packets, err := DecodeFrame(raw); !errors.Is(err, ErrInvalidPacket) || packets != nil {
				t.Fatalf("DecodeFrame returned %v, %v", packets, err)
			}
			if packets := Slice(raw); packets != nil {
				t.Fatalf("Slice returned invalid packets: %+v", packets)
			}
		})
	}
}

func TestDecodeFrameConcatenatedAndNested(t *testing.T) {
	inner := append(encoded(Plain, Notification, "first"), encoded(Popularity, HeartBeatResponse, "second")...)
	nested := compressed(t, Zlib, compressed(t, Brotli, inner))
	raw := append(encoded(Popularity, RoomEnterResponse, "before"), nested...)
	raw = append(raw, encoded(Plain, Notification, "after")...)
	packets, err := DecodeFrame(raw)
	if err != nil {
		t.Fatal(err)
	}
	wantBodies := []string{"before", "first", "second", "after"}
	if len(packets) != len(wantBodies) {
		t.Fatalf("got %d packets, want %d", len(packets), len(wantBodies))
	}
	for i, p := range packets {
		if string(p.Body) != wantBodies[i] {
			t.Fatalf("packet %d body = %q, want %q", i, p.Body, wantBodies[i])
		}
	}
	if packets[2].Operation != HeartBeatResponse {
		t.Fatal("inner packet operation lost")
	}
}

func TestDecodeFrameLimits(t *testing.T) {
	plain := encoded(Plain, Notification, "payload")
	for _, protocol := range []uint16{Zlib, Brotli} {
		t.Run(fmt.Sprintf("protocol_%d", protocol), func(t *testing.T) {
			raw := compressed(t, protocol, plain)
			exact := Decoder{MaxDecompressedSize: int64(len(plain))}
			if packets, err := exact.DecodeFrame(raw); err != nil || len(packets) != 1 {
				t.Fatalf("exact byte limit rejected: %v", err)
			}
			tooSmall := Decoder{MaxDecompressedSize: int64(len(plain) - 1)}
			if _, err := tooSmall.DecodeFrame(raw); !errors.Is(err, ErrDecompressionLimit) {
				t.Fatalf("expected size limit, got %v", err)
			}
			if _, err := exact.DecodeFrame(append(bytes.Clone(raw), raw...)); !errors.Is(err, ErrDecompressionLimit) {
				t.Fatalf("cumulative limit bypassed: %v", err)
			}
		})
	}
	for _, tc := range []struct {
		name    string
		decoder Decoder
		raw     []byte
		want    error
	}{
		{"nesting", Decoder{MaxDepth: 1}, compressed(t, Zlib, compressed(t, Brotli, plain)), ErrNestingLimit},
		{"packet_count", Decoder{MaxPackets: 1}, append(bytes.Clone(plain), plain...), ErrPacketLimit},
		{"envelope_count", Decoder{MaxPackets: 1}, compressed(t, Zlib, plain), ErrPacketLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if packets, err := tc.decoder.DecodeFrame(tc.raw); !errors.Is(err, tc.want) || packets != nil {
				t.Fatalf("got %v, %v; want no packets and %v", packets, err, tc.want)
			}
		})
	}
	if _, err := (Decoder{MaxDepth: -1}).DecodeFrame(plain); err == nil {
		t.Fatal("negative decoder limit accepted")
	}
}

func TestDecodeFrameRejectsCorruption(t *testing.T) {
	plain := encoded(Plain, Notification, "payload")
	badChecksum := compressed(t, Zlib, plain)
	badChecksum[len(badChecksum)-1] ^= 0xff
	truncatedZlib := compressed(t, Zlib, plain)
	truncatedZlib = truncatedZlib[:len(truncatedZlib)-1]
	binary.BigEndian.PutUint32(truncatedZlib[:4], uint32(len(truncatedZlib)))
	for _, raw := range [][]byte{
		badChecksum,
		truncatedZlib,
		encoded(Brotli, Notification, "bad brotli data"),
		encoded(Zlib, Notification, "bad zlib data"),
		compressed(t, Zlib, []byte{0}),
		compressed(t, Brotli, append(bytes.Clone(plain), 1, 2, 3)),
		encoded(99, Notification, "unknown protocol"),
		append(bytes.Clone(plain), 1),
	} {
		if packets, err := DecodeFrame(raw); err == nil || packets != nil {
			t.Fatalf("accepted corrupt frame: %x: %v, %v", raw, packets, err)
		}
	}
	if _, err := DecodeFrame(encoded(99, Notification, "")); !errors.Is(err, ErrUnsupportedProtocol) {
		t.Fatalf("expected unsupported protocol, got %v", err)
	}
}

func TestConstructControlPackets(t *testing.T) {
	heartbeat, err := Decode(NewHeartBeatPacket())
	if err != nil || heartbeat.ProtocolVersion != Popularity || heartbeat.Operation != HeartBeat || len(heartbeat.Body) != 0 {
		t.Fatalf("invalid heartbeat: %+v, %v", heartbeat, err)
	}
	entry, err := Decode(NewEnterPacket(123, "test-buvid", 456, "test-token"))
	if err != nil {
		t.Fatal(err)
	}
	var payload Enter
	if err := json.Unmarshal(entry.Body, &payload); err != nil {
		t.Fatal(err)
	}
	want := Enter{UID: 123, Buvid: "test-buvid", RoomID: 456, ProtoVer: Brotli, Platform: "web", Type: 2, Key: "test-token"}
	if payload != want || entry.ProtocolVersion != Popularity || entry.Operation != RoomEnter {
		t.Fatalf("incorrect entry packet: %+v, %+v", entry, payload)
	}
}

func TestLegacyWrappers(t *testing.T) {
	for _, invalid := range [][]byte{nil, {0}, make([]byte, HeaderLength)} {
		p := DecodePacket(invalid)
		if p.Operation != 0 || p.Body != nil {
			t.Fatalf("invalid input returned a fabricated packet: %+v", p)
		}
		if packets := Slice(invalid); packets != nil {
			t.Fatalf("Slice accepted invalid input: %+v", packets)
		}
	}
	p := NewPacket(Zlib, Notification, []byte("corrupt"))
	if packets := p.Parse(); packets != nil {
		t.Fatalf("Parse accepted corrupt data: %+v", packets)
	}
	plain := encoded(Plain, Notification, "message")
	wrapped := compressed(t, Brotli, plain)
	if packets := Slice(wrapped); len(packets) != 1 || packets[0].ProtocolVersion != Brotli {
		t.Fatalf("Slice should retain compression envelopes: %+v", packets)
	}
	if packets := DecodePacket(wrapped).Parse(); len(packets) != 1 || string(packets[0].Body) != "message" {
		t.Fatalf("legacy Parse failed: %+v", packets)
	}
}

func FuzzDecodeFrame(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0})
	f.Add(make([]byte, HeaderLength))
	plain := encoded(Plain, Notification, `{"cmd":"LIVE"}`)
	f.Add(plain)
	f.Add(append(bytes.Clone(plain), plain...))
	f.Add(compressed(f, Zlib, plain))
	f.Add(compressed(f, Brotli, plain))
	f.Add(compressed(f, Zlib, compressed(f, Brotli, plain)))
	decoder := Decoder{MaxDecompressedSize: 1 << 20, MaxDepth: 4, MaxPackets: 256}
	f.Fuzz(func(t *testing.T, raw []byte) {
		packets, err := decoder.DecodeFrame(raw)
		if err != nil {
			if packets != nil {
				t.Fatal("failed decode returned partial packets")
			}
			return
		}
		for _, p := range packets {
			if p.ProtocolVersion != Plain && p.ProtocolVersion != Popularity {
				t.Fatalf("unexpanded packet: %+v", p)
			}
			if p.HeaderLength < HeaderLength || p.PacketLength != p.HeaderLength+len(p.Body) {
				t.Fatalf("inconsistent decoded packet: %+v", p)
			}
		}
	})
}

func FuzzDecode(f *testing.F) {
	f.Add([]byte{})
	f.Add(NewHeartBeatPacket())
	f.Add(encoded(Plain, Notification, "example"))
	f.Fuzz(func(t *testing.T, raw []byte) {
		p, err := Decode(raw)
		if err != nil {
			return
		}
		if p.PacketLength != len(raw) || p.HeaderLength < HeaderLength || len(p.Body) != len(raw)-p.HeaderLength {
			t.Fatalf("inconsistent decoded packet: %+v", p)
		}
	})
}

func BenchmarkBuild(b *testing.B) {
	p := NewPacket(Plain, Notification, bytes.Repeat([]byte("payload"), 32))
	b.ReportAllocs()
	b.SetBytes(int64(HeaderLength + len(p.Body)))
	for i := 0; i < b.N; i++ {
		_ = p.Build()
	}
}

func BenchmarkDecodeFrame(b *testing.B) {
	plain := encoded(Plain, Notification, `{"cmd":"DANMU_MSG","info":[]}`)
	for _, tc := range []struct {
		name string
		raw  []byte
	}{
		{"single", plain},
		{"batch_100", bytes.Repeat(plain, 100)},
		{"brotli_100", compressed(b, Brotli, bytes.Repeat(plain, 100))},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.raw)))
			for i := 0; i < b.N; i++ {
				if _, err := DecodeFrame(tc.raw); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
