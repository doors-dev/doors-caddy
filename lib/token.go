package lib

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
)

var ErrorBase64 = errors.New("base64 decode error")
var ErrorCipher = errors.New("cipher decrypt error")
var ErrorParse = errors.New("ip parse error")

var aad = []byte("doors-pod-ip-v1")

func NewTokenCipher(secret string) (TokenCipher, error) {
	decodedKey, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return tokenCipher{}, err
	}
	block, err := aes.NewCipher(decodedKey)
	if err != nil {
		return tokenCipher{}, err
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		panic(fmt.Errorf("unreachable: AES block rejected by GCM: %w", err))
	}
	return tokenCipher{aead}, nil
}

type TokenCipher interface {
	Encode(addr netip.Addr) (token string)
	Decode(token string) (netip.Addr, error)
}

type tokenCipher struct {
	aead cipher.AEAD
}

func (c tokenCipher) Encode(addr netip.Addr) (token string) {
	ciphertext := c.aead.Seal(nil, nil, addr.AsSlice(), aad)
	return base64.RawURLEncoding.EncodeToString(ciphertext)
}

func (c tokenCipher) Decode(token string) (netip.Addr, error) {
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return netip.Addr{}, ErrorsJoin(ErrorBase64, err)
	}
	bytes, err := c.aead.Open(nil, nil, data, aad)
	if err != nil {
		return netip.Addr{}, ErrorsJoin(ErrorCipher, err)
	}
	addr, ok := netip.AddrFromSlice(bytes)
	if !ok {
		return netip.Addr{}, ErrorParse
	}
	return addr, nil
}
