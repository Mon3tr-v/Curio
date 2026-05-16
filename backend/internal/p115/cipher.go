package p115

import (
	"encoding/base64"
	"errors"
	"math/big"
)

var (
	p115RSAModulus = mustBigInt("8686980c0f5a24c4b9d43020cd2c22703ff3f450756529058b1cf88f09b8602136477198a6e2683149659bd122c33592fdb5ad47944ad1ea4d36c6b172aad6338c3bb6ac6227502d010993ac967d1aef00f0c8e038de2e4d3bc2ec368af2e9f10a6f1eda4f7262f136420c07c331b871bf139f74f3010e3c4fe57df3afb71683")
	p115RSAExp     = big.NewInt(0x10001)
	p115RSAKey     = []byte{0x8d, 0xa5, 0xa5, 0x8d}
	p115RSAKeyLong = []byte{0x78, 0x06, 0xad, 0x4c, 0x33, 0x86, 0x5d, 0x18, 0x4c, 0x01, 0x3f, 0x46}
	p115GKTS       = []byte{
		0xf0, 0xe5, 0x69, 0xae, 0xbf, 0xdc, 0xbf, 0x8a, 0x1a, 0x45, 0xe8, 0xbe, 0x7d, 0xa6, 0x73, 0xb8,
		0xde, 0x8f, 0xe7, 0xc4, 0x45, 0xda, 0x86, 0xc4, 0x9b, 0x64, 0x8b, 0x14, 0x6a, 0xb4, 0xf1, 0xaa,
		0x38, 0x01, 0x35, 0x9e, 0x26, 0x69, 0x2c, 0x86, 0x00, 0x6b, 0x4f, 0xa5, 0x36, 0x34, 0x62, 0xa6,
		0x2a, 0x96, 0x68, 0x18, 0xf2, 0x4a, 0xfd, 0xbd, 0x6b, 0x97, 0x8f, 0x4d, 0x8f, 0x89, 0x13, 0xb7,
		0x6c, 0x8e, 0x93, 0xed, 0x0e, 0x0d, 0x48, 0x3e, 0xd7, 0x2f, 0x88, 0xd8, 0xfe, 0xfe, 0x7e, 0x86,
		0x50, 0x95, 0x4f, 0xd1, 0xeb, 0x83, 0x26, 0x34, 0xdb, 0x66, 0x7b, 0x9c, 0x7e, 0x9d, 0x7a, 0x81,
		0x32, 0xea, 0xb6, 0x33, 0xde, 0x3a, 0xa9, 0x59, 0x34, 0x66, 0x3b, 0xaa, 0xba, 0x81, 0x60, 0x48,
		0xb9, 0xd5, 0x81, 0x9c, 0xf8, 0x6c, 0x84, 0x77, 0xff, 0x54, 0x78, 0x26, 0x5f, 0xbe, 0xe8, 0x1e,
		0x36, 0x9f, 0x34, 0x80, 0x5c, 0x45, 0x2c, 0x9b, 0x76, 0xd5, 0x1b, 0x8f, 0xcc, 0xc3, 0xb8, 0xf5,
	}
)

func p115RSAEncrypt(data []byte) string {
	randKey := make([]byte, 16)
	tmp := reverseBytes(xor115(data, p115RSAKey))
	plain := append(randKey, xor115(tmp, p115RSAKeyLong)...)
	return base64.StdEncoding.EncodeToString(p115RSAApplyPKCS1(plain))
}

func p115RSADecrypt(value string) ([]byte, error) {
	cipherData, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	data, err := p115RSAUnwrapPKCS1(cipherData)
	if err != nil {
		return nil, err
	}
	if len(data) < 16 {
		return nil, errors.New("115 加密响应无效")
	}
	keyLong := p115RSAGenKey(data[:16], 12)
	tmp := reverseBytes(xor115(data[16:], keyLong))
	return xor115(tmp, p115RSAKey), nil
}

func p115RSAApplyPKCS1(data []byte) []byte {
	out := make([]byte, 0, ((len(data)+116)/117)*128)
	for start := 0; start < len(data); start += 117 {
		end := start + 117
		if end > len(data) {
			end = len(data)
		}
		block := p115PKCS1Pad(data[start:end])
		cipher := new(big.Int).Exp(new(big.Int).SetBytes(block), p115RSAExp, p115RSAModulus).Bytes()
		out = append(out, leftPad(cipher, 128)...)
	}
	return out
}

func p115RSAUnwrapPKCS1(cipherData []byte) ([]byte, error) {
	if len(cipherData)%128 != 0 {
		return nil, errors.New("115 加密响应长度无效")
	}
	out := make([]byte, 0, len(cipherData))
	for start := 0; start < len(cipherData); start += 128 {
		block := new(big.Int).Exp(new(big.Int).SetBytes(cipherData[start:start+128]), p115RSAExp, p115RSAModulus).Bytes()
		sep := -1
		for i, b := range block {
			if b == 0 {
				sep = i
				break
			}
		}
		if sep < 0 || sep+1 > len(block) {
			return nil, errors.New("115 加密响应填充无效")
		}
		out = append(out, block[sep+1:]...)
	}
	return out, nil
}

func p115PKCS1Pad(message []byte) []byte {
	block := make([]byte, 0, 128)
	block = append(block, 0)
	for i := 0; i < 126-len(message); i++ {
		block = append(block, 0x02)
	}
	block = append(block, 0)
	block = append(block, message...)
	return block
}

func p115RSAGenKey(randKey []byte, size int) []byte {
	key := make([]byte, size)
	length := size * (size - 1)
	index := 0
	for i := 0; i < size; i++ {
		x := (int(randKey[i]) + int(p115GKTS[index])) & 0xff
		key[i] = p115GKTS[length] ^ byte(x)
		length -= size
		index += size
	}
	return key
}

func xor115(src, key []byte) []byte {
	if len(key) == 0 {
		return append([]byte(nil), src...)
	}
	out := make([]byte, len(src))
	start := len(src) & 3
	for i := 0; i < start; i++ {
		out[i] = src[i] ^ key[i]
	}
	for offset := start; offset < len(src); offset += len(key) {
		for i := 0; i < len(key) && offset+i < len(src); i++ {
			out[offset+i] = src[offset+i] ^ key[i]
		}
	}
	return out
}

func reverseBytes(src []byte) []byte {
	out := make([]byte, len(src))
	for i := range src {
		out[i] = src[len(src)-1-i]
	}
	return out
}

func leftPad(src []byte, size int) []byte {
	if len(src) >= size {
		return src
	}
	out := make([]byte, size)
	copy(out[size-len(src):], src)
	return out
}

func mustBigInt(hexValue string) *big.Int {
	value, ok := new(big.Int).SetString(hexValue, 16)
	if !ok {
		panic("invalid 115 rsa modulus")
	}
	return value
}
