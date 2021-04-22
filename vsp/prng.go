package vsp

import (
	"encoding/binary"
	"io"
	"math/bits"

	"golang.org/x/crypto/chacha20"
)

// RandSource returns cryptographically-secure pseudorandom numbers with uniform
// distribution.
type RandSource struct {
	buf    [8]byte
	cipher *chacha20.Cipher
}

var nonce = make([]byte, chacha20.NonceSize)

// NewRandSource seeds a RandSource from a 32-byte key.
func NewRandSource(seed *[32]byte) *RandSource {
	cipher, _ := chacha20.NewUnauthenticatedCipher(seed[:], nonce)
	return &RandSource{cipher: cipher}
}

// RandSource creates a RandSource with seed randomness read from rand.
func CreateRandSource(rand io.Reader) (*RandSource, error) {
	seed := new([32]byte)
	_, err := io.ReadFull(rand, seed[:])
	if err != nil {
		return nil, err
	}
	return NewRandSource(seed), nil
}

// Uint32 returns a pseudo-random uint32.
func (s *RandSource) Uint32() uint32 {
	b := s.buf[:4]
	for i := range b {
		b[i] = 0
	}
	s.cipher.XORKeyStream(b, b)
	return binary.LittleEndian.Uint32(b)
}

// Uint32n returns a pseudo-random uint32 in range [0,n) without modulo bias.
func (s *RandSource) Uint32n(n uint32) uint32 {
	if n < 2 {
		return 0
	}
	n--
	mask := ^uint32(0) >> bits.LeadingZeros32(n)
	for {
		u := s.Uint32() & mask
		if u <= n {
			return u
		}
	}
}

// Int63 returns a pseudo-random 63-bit positive integer as an int64 without
// modulo bias.
func (s *RandSource) Int63() int64 {
	b := s.buf[:]
	for i := range b {
		b[i] = 0
	}
	s.cipher.XORKeyStream(b, b)
	return int64(binary.LittleEndian.Uint64(b) &^ (1 << 63))
}

// Int63n returns, as an int64, a pseudo-random 63-bit positive integer in [0,n)
// without modulo bias.
// It panics if n <= 0.
func (s *RandSource) Int63n(n int64) int64 {
	if n <= 0 {
		panic("invalid argument to Int63n")
	}
	n--
	mask := int64(^uint64(0) >> bits.LeadingZeros64(uint64(n)))
	for {
		i := s.Int63() & mask
		if i <= n {
			return i
		}
	}
}
