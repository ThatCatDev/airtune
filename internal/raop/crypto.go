package raop

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
)

// GenerateAESKey generates a random 16-byte AES key and 16-byte IV
// for use in RAOP audio encryption.
func GenerateAESKey() (key []byte, iv []byte, err error) {
	key = make([]byte, 16)
	if _, err = rand.Read(key); err != nil {
		return nil, nil, fmt.Errorf("generating AES key: %w", err)
	}

	iv = make([]byte, 16)
	if _, err = rand.Read(iv); err != nil {
		return nil, nil, fmt.Errorf("generating AES IV: %w", err)
	}

	return key, iv, nil
}

// EncryptAESKey encrypts the AES key with the AirPlay RSA public key
// using RSA PKCS1v15 (NOT OAEP — AirPlay 1 uses PKCS1v15).
// Returns base64-encoded encrypted key. The IV is sent unencrypted in the SDP.
func EncryptAESKey(key []byte) (string, error) {
	// Parse the PEM-encoded RSA public key
	block, _ := pem.Decode([]byte(AirPlayRSAPublicKeyPEM))
	if block == nil {
		return "", fmt.Errorf("failed to decode AirPlay RSA public key PEM")
	}

	pub, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parsing PKCS1 public key: %w", err)
	}

	// Encrypt the AES key with RSA PKCS1v15
	encKey, err := rsa.EncryptPKCS1v15(rand.Reader, pub, key)
	if err != nil {
		return "", fmt.Errorf("encrypting AES key: %w", err)
	}

	// Base64 encode without padding (AirPlay convention)
	return base64.RawStdEncoding.EncodeToString(encKey), nil
}

// EncryptAudio encrypts audio data using AES-128-CBC for RAOP streaming.
//
// AirPlay does NOT use standard PKCS7 padding. Only complete 16-byte blocks
// are encrypted; any remaining bytes are left unencrypted and appended as-is.
// The IV is reset to the original value for each packet (not chained across packets).
func EncryptAudio(data, key, iv []byte) []byte {
	blockSize := aes.BlockSize // 16
	fullLen := len(data) / blockSize * blockSize

	if fullLen == 0 {
		// No complete blocks to encrypt; return data unchanged.
		out := make([]byte, len(data))
		copy(out, data)
		return out
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		// key is expected to be valid 16 bytes; return data unmodified on error
		out := make([]byte, len(data))
		copy(out, data)
		return out
	}

	out := make([]byte, len(data))

	// Encrypt the complete blocks with CBC using a fresh copy of the IV
	ivCopy := make([]byte, len(iv))
	copy(ivCopy, iv)
	mode := cipher.NewCBCEncrypter(block, ivCopy)
	mode.CryptBlocks(out[:fullLen], data[:fullLen])

	// Append the remaining bytes unencrypted
	if fullLen < len(data) {
		copy(out[fullLen:], data[fullLen:])
	}

	return out
}
