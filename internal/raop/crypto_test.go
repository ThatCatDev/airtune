package raop

import (
	"bytes"
	"testing"
)

func TestGenerateAESKey(t *testing.T) {
	key, iv, err := GenerateAESKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 16 {
		t.Errorf("key length = %d, want 16", len(key))
	}
	if len(iv) != 16 {
		t.Errorf("iv length = %d, want 16", len(iv))
	}
	if bytes.Equal(key, make([]byte, 16)) {
		t.Error("key is all zeros")
	}
	key2, iv2, _ := GenerateAESKey()
	if bytes.Equal(key, key2) {
		t.Error("two generated keys are identical")
	}
	if bytes.Equal(iv, iv2) {
		t.Error("two generated IVs are identical")
	}
}

func TestEncryptAESKey(t *testing.T) {
	key, _, _ := GenerateAESKey()
	encKey, err := EncryptAESKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if encKey == "" {
		t.Error("encrypted key is empty")
	}
}

func TestEncryptAudio(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 16)
	iv := bytes.Repeat([]byte{0x24}, 16)

	data := bytes.Repeat([]byte{0xAB}, 35)
	enc := EncryptAudio(data, key, iv)

	if len(enc) != len(data) {
		t.Errorf("output length = %d, want %d", len(enc), len(data))
	}
	if bytes.Equal(enc[:32], data[:32]) {
		t.Error("encrypted blocks should differ from plaintext")
	}
	if !bytes.Equal(enc[32:], data[32:]) {
		t.Error("remainder bytes should be unencrypted")
	}

	empty := EncryptAudio(nil, key, iv)
	if len(empty) != 0 {
		t.Errorf("empty input produced %d bytes", len(empty))
	}
}
