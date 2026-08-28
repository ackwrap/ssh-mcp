package secure

import (
	"bytes"
	"testing"
)

func TestEncryptRoundTrip(t *testing.T) {
	salt := bytes.Repeat([]byte{0x42}, 16)
	key := DeriveKey("master123", salt)
	if len(key) != KeyLength {
		t.Fatalf("key length = %d", len(key))
	}
	token, err := Encrypt(key, []byte("服务器密码: secret123"))
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := Decrypt(key, token)
	if err != nil {
		t.Fatal(err)
	}
	if string(plaintext) != "服务器密码: secret123" {
		t.Fatalf("plaintext = %q", plaintext)
	}
	if !VerifyMaster("master123", salt, MakeMasterHash("master123", salt)) {
		t.Fatal("master hash did not verify")
	}
	if VerifyMaster("wrong", salt, MakeMasterHash("master123", salt)) {
		t.Fatal("wrong password verified")
	}
}

func TestDecryptsPythonFernetToken(t *testing.T) {
	salt := bytes.Repeat([]byte{0x42}, 16)
	const token = "gAAAAABqka3SxwnZaJDDXyf-JZ64vxJDdKRLm1YP-VMFTAikqO9nMSg-RKsisj9OteYxrS8a3OEYzkyhbWVOuQxT5KcozwefEA=="
	plaintext, err := Decrypt(DeriveKey("master123", salt), token)
	if err != nil {
		t.Fatal(err)
	}
	if string(plaintext) != "兼容测试" {
		t.Fatalf("plaintext = %q", plaintext)
	}
}
