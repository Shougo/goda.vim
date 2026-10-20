// Package msgpack is a small, dependency-free MessagePack codec: just enough
// for Neovim's msgpack-rpc. Values are represented as plain Go values
// (nil, bool, int64, uint64, float64, string, []any, map[string]any, Ext).
//
// Typed data is converted by the caller (core goes through JSON). That costs a
// copy but keeps this package tiny; a reflect-based codec can replace it later
// if profiling says so. json.Number is accepted by Encode so that integers
// survive that conversion.
package msgpack

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
)

const (
	maxLen   = 1 << 28 // refuse absurd lengths from a misbehaving peer
	maxDepth = 512
)

// Ext is a MessagePack extension value (Neovim uses these for Buffer, Window
// and Tabpage handles).
type Ext struct {
	Type int8
	Data []byte
}

// Encode encodes v. Supported: nil, bool, all int/uint sizes, float32/64,
// json.Number, string, []byte (bin), []string, []any, map[string]any.
func Encode(v any) ([]byte, error) { return appendValue(nil, v) }

func appendValue(b []byte, v any) ([]byte, error) {
	switch x := v.(type) {
	case nil:
		return append(b, 0xc0), nil
	case bool:
		if x {
			return append(b, 0xc3), nil
		}
		return append(b, 0xc2), nil
	case int:
		return appendInt(b, int64(x)), nil
	case int8:
		return appendInt(b, int64(x)), nil
	case int16:
		return appendInt(b, int64(x)), nil
	case int32:
		return appendInt(b, int64(x)), nil
	case int64:
		return appendInt(b, x), nil
	case uint:
		return appendUint(b, uint64(x)), nil
	case uint8:
		return appendUint(b, uint64(x)), nil
	case uint16:
		return appendUint(b, uint64(x)), nil
	case uint32:
		return appendUint(b, uint64(x)), nil
	case uint64:
		return appendUint(b, x), nil
	case float32:
		b = append(b, 0xca)
		return binary.BigEndian.AppendUint32(b, math.Float32bits(x)), nil
	case float64:
		b = append(b, 0xcb)
		return binary.BigEndian.AppendUint64(b, math.Float64bits(x)), nil
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return appendInt(b, i), nil
		}
		f, err := x.Float64()
		if err != nil {
			return nil, fmt.Errorf("msgpack: bad number %q", x)
		}
		return appendValue(b, f)
	case string:
		return appendStr(b, x), nil
	case []byte:
		return appendBin(b, x), nil
	case []string:
		b = appendHeader(b, len(x), 0x90, 0xdc, 0xdd)
		for _, s := range x {
			b = appendStr(b, s)
		}
		return b, nil
	case []any:
		b = appendHeader(b, len(x), 0x90, 0xdc, 0xdd)
		for _, e := range x {
			var err error
			if b, err = appendValue(b, e); err != nil {
				return nil, err
			}
		}
		return b, nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys) // deterministic output
		b = appendHeader(b, len(x), 0x80, 0xde, 0xdf)
		for _, k := range keys {
			b = appendStr(b, k)
			var err error
			if b, err = appendValue(b, x[k]); err != nil {
				return nil, err
			}
		}
		return b, nil
	default:
		return nil, fmt.Errorf("msgpack: unsupported type %T", v)
	}
}

func appendInt(b []byte, n int64) []byte {
	switch {
	case n >= 0:
		return appendUint(b, uint64(n))
	case n >= -32:
		return append(b, byte(n))
	case n >= math.MinInt8:
		return append(b, 0xd0, byte(n))
	case n >= math.MinInt16:
		return binary.BigEndian.AppendUint16(append(b, 0xd1), uint16(n))
	case n >= math.MinInt32:
		return binary.BigEndian.AppendUint32(append(b, 0xd2), uint32(n))
	default:
		return binary.BigEndian.AppendUint64(append(b, 0xd3), uint64(n))
	}
}

func appendUint(b []byte, n uint64) []byte {
	switch {
	case n <= 127:
		return append(b, byte(n))
	case n <= math.MaxUint8:
		return append(b, 0xcc, byte(n))
	case n <= math.MaxUint16:
		return binary.BigEndian.AppendUint16(append(b, 0xcd), uint16(n))
	case n <= math.MaxUint32:
		return binary.BigEndian.AppendUint32(append(b, 0xce), uint32(n))
	default:
		return binary.BigEndian.AppendUint64(append(b, 0xcf), n)
	}
}

func appendStr(b []byte, s string) []byte {
	n := len(s)
	switch {
	case n < 32:
		b = append(b, 0xa0|byte(n))
	case n <= math.MaxUint8:
		b = append(b, 0xd9, byte(n))
	case n <= math.MaxUint16:
		b = binary.BigEndian.AppendUint16(append(b, 0xda), uint16(n))
	default:
		b = binary.BigEndian.AppendUint32(append(b, 0xdb), uint32(n))
	}
	return append(b, s...)
}

func appendBin(b []byte, s []byte) []byte {
	n := len(s)
	switch {
	case n <= math.MaxUint8:
		b = append(b, 0xc4, byte(n))
	case n <= math.MaxUint16:
		b = binary.BigEndian.AppendUint16(append(b, 0xc5), uint16(n))
	default:
		b = binary.BigEndian.AppendUint32(append(b, 0xc6), uint32(n))
	}
	return append(b, s...)
}

// appendHeader writes an array or map header; fix is the fixarray/fixmap base
// (0x90 / 0x80), c16 and c32 the 16- and 32-bit length type bytes.
func appendHeader(b []byte, n int, fix, c16, c32 byte) []byte {
	switch {
	case n < 16:
		return append(b, fix|byte(n))
	case n <= math.MaxUint16:
		return binary.BigEndian.AppendUint16(append(b, c16), uint16(n))
	default:
		return binary.BigEndian.AppendUint32(append(b, c32), uint32(n))
	}
}

// Decoder reads consecutive MessagePack values from a stream.
type Decoder struct {
	r     *bufio.Reader
	depth int
}

func NewDecoder(r io.Reader) *Decoder {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}
	return &Decoder{r: br}
}

// Decode reads one value. It returns io.EOF only at a clean message boundary.
func (d *Decoder) Decode() (any, error) {
	b, err := d.r.ReadByte()
	if err != nil {
		return nil, err
	}
	v, err := d.decode(b)
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return v, err
}

func (d *Decoder) value() (any, error) {
	b, err := d.r.ReadByte()
	if err != nil {
		return nil, err
	}
	return d.decode(b)
}

func (d *Decoder) decode(b byte) (any, error) {
	switch {
	case b <= 0x7f:
		return int64(b), nil
	case b >= 0xe0:
		return int64(int8(b)), nil
	case b&0xe0 == 0xa0:
		return d.str(int(b & 0x1f))
	case b&0xf0 == 0x90:
		return d.array(int(b & 0x0f))
	case b&0xf0 == 0x80:
		return d.mapN(int(b & 0x0f))
	}
	switch b {
	case 0xc0:
		return nil, nil
	case 0xc2:
		return false, nil
	case 0xc3:
		return true, nil
	case 0xc4, 0xc5, 0xc6: // bin: decoded as string, like Neovim's own clients
		n, err := d.length(1 << (b - 0xc4))
		if err != nil {
			return nil, err
		}
		return d.str(n)
	case 0xc7, 0xc8, 0xc9:
		n, err := d.length(1 << (b - 0xc7))
		if err != nil {
			return nil, err
		}
		return d.ext(n)
	case 0xca:
		buf, err := d.readN(4)
		if err != nil {
			return nil, err
		}
		return float64(math.Float32frombits(binary.BigEndian.Uint32(buf))), nil
	case 0xcb:
		buf, err := d.readN(8)
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(binary.BigEndian.Uint64(buf)), nil
	case 0xcc, 0xcd, 0xce, 0xcf:
		u, err := d.uintN(1 << (b - 0xcc))
		if err != nil {
			return nil, err
		}
		if u > math.MaxInt64 {
			return u, nil
		}
		return int64(u), nil
	case 0xd0, 0xd1, 0xd2, 0xd3:
		size := 1 << (b - 0xd0)
		u, err := d.uintN(size)
		if err != nil {
			return nil, err
		}
		shift := uint(64 - 8*size)
		return int64(u<<shift) >> shift, nil // sign-extend
	case 0xd4, 0xd5, 0xd6, 0xd7, 0xd8:
		return d.ext(1 << (b - 0xd4))
	case 0xd9, 0xda, 0xdb:
		n, err := d.length(1 << (b - 0xd9))
		if err != nil {
			return nil, err
		}
		return d.str(n)
	case 0xdc, 0xdd:
		n, err := d.length(2 << (b - 0xdc))
		if err != nil {
			return nil, err
		}
		return d.array(n)
	case 0xde, 0xdf:
		n, err := d.length(2 << (b - 0xde))
		if err != nil {
			return nil, err
		}
		return d.mapN(n)
	}
	return nil, fmt.Errorf("msgpack: invalid type byte 0x%02x", b)
}

func (d *Decoder) readN(n int) ([]byte, error) {
	if n < 0 || n > maxLen {
		return nil, fmt.Errorf("msgpack: length %d exceeds limit", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(d.r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func (d *Decoder) uintN(size int) (uint64, error) {
	buf, err := d.readN(size)
	if err != nil {
		return 0, err
	}
	var u uint64
	for _, c := range buf {
		u = u<<8 | uint64(c)
	}
	return u, nil
}

func (d *Decoder) length(size int) (int, error) {
	u, err := d.uintN(size)
	if err != nil {
		return 0, err
	}
	if u > maxLen {
		return 0, fmt.Errorf("msgpack: length %d exceeds limit", u)
	}
	return int(u), nil
}

func (d *Decoder) str(n int) (any, error) {
	buf, err := d.readN(n)
	if err != nil {
		return nil, err
	}
	return string(buf), nil
}

func (d *Decoder) ext(n int) (any, error) {
	t, err := d.r.ReadByte()
	if err != nil {
		return nil, err
	}
	data, err := d.readN(n)
	if err != nil {
		return nil, err
	}
	return Ext{Type: int8(t), Data: data}, nil
}

func (d *Decoder) array(n int) (any, error) {
	if d.depth++; d.depth > maxDepth {
		return nil, fmt.Errorf("msgpack: nesting too deep")
	}
	defer func() { d.depth-- }()
	out := make([]any, 0, min(n, 1024))
	for range n {
		v, err := d.value()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (d *Decoder) mapN(n int) (any, error) {
	if d.depth++; d.depth > maxDepth {
		return nil, fmt.Errorf("msgpack: nesting too deep")
	}
	defer func() { d.depth-- }()
	out := make(map[string]any, min(n, 1024))
	for range n {
		k, err := d.value()
		if err != nil {
			return nil, err
		}
		v, err := d.value()
		if err != nil {
			return nil, err
		}
		ks, ok := k.(string)
		if !ok {
			ks = fmt.Sprint(k)
		}
		out[ks] = v
	}
	return out, nil
}
