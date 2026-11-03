package auth

import "testing"

func TestArgon2idHasher(t *testing.T) {
	hasher := NewArgon2idHasher()
	encoded, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	match, err := hasher.Verify(encoded, "correct horse battery staple")
	if err != nil || !match {
		t.Fatalf("Verify(correct) = %v, %v; want true, nil", match, err)
	}
	match, err = hasher.Verify(encoded, "wrong password")
	if err != nil || match {
		t.Fatalf("Verify(wrong) = %v, %v; want false, nil", match, err)
	}
}

func TestArgon2idHasherRejectsMalformedHashes(t *testing.T) {
	_, err := NewArgon2idHasher().Verify("not-an-argon-hash", "password")
	if err == nil {
		t.Fatal("Verify() error = nil, want malformed hash error")
	}
}
