// Package neiwangtong contains wire formats and protocols validated against
// native Neiwangtong 3.4.3055 clients.
package neiwangtong

import (
	"encoding/binary"
	"errors"
)

// Key is the 16-byte static key extracted from native Neiwangtong binaries.
var Key = []byte("8asfhj@k7*20hbla")

const (
	xteaRounds = 32
	xteaDelta  = 0x9E3779B9
	xteaSum    = 0xC6EF3720
	blockSize  = 8
)

var errKeyLength = errors.New("neiwangtong: key must be 16 bytes")

// xteaDecryptBlock decrypts an 8-byte block using XTEA with big-endian byte-swapping.
func xteaDecryptBlock(dst, src []byte, k []uint32) {
	v0 := binary.BigEndian.Uint32(src[0:4])
	v1 := binary.BigEndian.Uint32(src[4:8])
	sum := uint32(xteaSum)

	for i := 0; i < xteaRounds; i++ {
		idx1 := (sum >> 11) & 3
		ebx1 := (k[idx1] + sum) ^ (((v0 >> 5) ^ (v0 << 4)) + v0)
		v1 -= ebx1
		sum -= xteaDelta

		idx0 := sum & 3
		ebx0 := (k[idx0] + sum) ^ (((v1 >> 5) ^ (v1 << 4)) + v1)
		v0 -= ebx0
	}

	binary.BigEndian.PutUint32(dst[0:4], v0)
	binary.BigEndian.PutUint32(dst[4:8], v1)
}

// xteaEncryptBlock encrypts an 8-byte block using XTEA with big-endian byte-swapping.
func xteaEncryptBlock(dst, src []byte, k []uint32) {
	v0 := binary.BigEndian.Uint32(src[0:4])
	v1 := binary.BigEndian.Uint32(src[4:8])
	sum := uint32(0)

	for i := 0; i < xteaRounds; i++ {
		idx0 := sum & 3
		ebx0 := (k[idx0] + sum) ^ (((v1 >> 5) ^ (v1 << 4)) + v1)
		v0 += ebx0
		sum += xteaDelta

		idx1 := (sum >> 11) & 3
		ebx1 := (k[idx1] + sum) ^ (((v0 >> 5) ^ (v0 << 4)) + v0)
		v1 += ebx1
	}

	binary.BigEndian.PutUint32(dst[0:4], v0)
	binary.BigEndian.PutUint32(dst[4:8], v1)
}

func parseKey(key []byte) ([]uint32, error) {
	if len(key) != 16 {
		return nil, errKeyLength
	}
	k := make([]uint32, 4)
	k[0] = binary.BigEndian.Uint32(key[0:4])
	k[1] = binary.BigEndian.Uint32(key[4:8])
	k[2] = binary.BigEndian.Uint32(key[8:12])
	k[3] = binary.BigEndian.Uint32(key[12:16])
	return k, nil
}

// Decrypt decrypts ciphertext in-place or into dst using XTEA ECB mode.
func Decrypt(dst, src, key []byte) error {
	k, err := parseKey(key)
	if err != nil {
		return err
	}
	blocks := len(src) / blockSize
	for i := 0; i < blocks; i++ {
		off := i * blockSize
		xteaDecryptBlock(dst[off:off+blockSize], src[off:off+blockSize], k)
	}
	if remainder := len(src) % blockSize; remainder != 0 {
		off := blocks * blockSize
		copy(dst[off:], src[off:off+remainder])
	}
	return nil
}

// Encrypt encrypts plaintext into dst using XTEA ECB mode.
// It pads the input with zeros to the next 8-byte block boundary.
func Encrypt(dst, src, key []byte) ([]byte, error) {
	k, err := parseKey(key)
	if err != nil {
		return nil, err
	}
	pad := (blockSize - (len(src) % blockSize)) % blockSize
	paddedLen := len(src) + pad
	if cap(dst) < paddedLen {
		dst = make([]byte, paddedLen)
	} else {
		dst = dst[:paddedLen]
	}
	copy(dst, src)
	for i := len(src); i < paddedLen; i++ {
		dst[i] = 0
	}
	blocks := paddedLen / blockSize
	for i := 0; i < blocks; i++ {
		off := i * blockSize
		xteaEncryptBlock(dst[off:off+blockSize], dst[off:off+blockSize], k)
	}
	return dst, nil
}
