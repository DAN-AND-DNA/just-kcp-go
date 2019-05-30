package kcp

import (
	"encoding/binary"
	"time"
)

func ikcpEncode8u(p []byte, c byte) []byte {
	p[0] = c
	return p[1:]
}

func ikcpDecode8u(p []byte, c *byte) []byte {
	*c = p[0]
	return p[1:]
}

func ikcpEncode16u(p []byte, w uint16) []byte {
	binary.LittleEndian.PutUint16(p, w)
	return p[2:]
}

func ikcpDecode16u(p []byte, w *uint16) []byte {
	*w = binary.LittleEndian.Uint16(p)
	return p[2:]
}

func ikcpEncode32u(p []byte, l uint32) []byte {
	binary.LittleEndian.PutUint32(p, l)
	return p[4:]
}

func ikcpDecode32u(p []byte, l *uint32) []byte {
	*l = binary.LittleEndian.Uint32(p)
	return p[4:]
}

func imax(a uint32, b uint32) uint32 {
	if a >= b {
		return a
	}
	return b
}

func imin(a uint32, b uint32) uint32 {
	if a >= b {
		return b
	}
	return a
}

func ibound(lower uint32, middle uint32, upper uint32) uint32 {
	return imin(imax(lower, middle), upper)
}

func itimediff(later uint32, earlier uint32) int {
	return (int)(later - earlier)
}

var runTime time.Time = time.Now()

func currentMs() uint32 {
	return uint32(time.Now().Sub(runTime) / time.Millisecond)
}
