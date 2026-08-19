package crypto

import (
	"bytes"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	e, err := NewEncryptor(key)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("hello drkate")
	enc, err := e.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.Decrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain, out) {
		t.Fatalf("round trip failed")
	}
}
