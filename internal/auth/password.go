package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

type PasswordHasher interface {
	Hash(string) (string, error)
	Verify(string, string) (bool, error)
}

type Argon2idHasher struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
}

func NewArgon2idHasher() *Argon2idHasher {
	return &Argon2idHasher{
		memory:      64 * 1024,
		iterations:  3,
		parallelism: 2,
		saltLength:  16,
		keyLength:   32,
	}
}

func (h *Argon2idHasher) Hash(password string) (string, error) {
	salt := make([]byte, h.saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, h.iterations, h.memory, h.parallelism, h.keyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		h.memory,
		h.iterations,
		h.parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func (h *Argon2idHasher) Verify(encodedHash, password string) (bool, error) {
	parameters, salt, expected, err := parseArgon2idHash(encodedHash)
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey([]byte(password), salt, parameters.iterations, parameters.memory, parameters.parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

type argon2Parameters struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
}

func parseArgon2idHash(encodedHash string) (argon2Parameters, []byte, []byte, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return argon2Parameters{}, nil, nil, errors.New("password hash has an invalid format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return argon2Parameters{}, nil, nil, errors.New("password hash has an unsupported Argon2 version")
	}
	var parameters argon2Parameters
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &parameters.memory, &parameters.iterations, &parameters.parallelism); err != nil {
		return argon2Parameters{}, nil, nil, errors.New("password hash has invalid parameters")
	}
	if parameters.memory < 8*1024 || parameters.memory > 256*1024 ||
		parameters.iterations < 1 || parameters.iterations > 10 ||
		parameters.parallelism < 1 || parameters.parallelism > 16 {
		return argon2Parameters{}, nil, nil, errors.New("password hash parameters are outside safe bounds")
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return argon2Parameters{}, nil, nil, errors.New("password hash contains an invalid salt")
	}
	hash, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(hash) < 16 || len(hash) > 64 {
		return argon2Parameters{}, nil, nil, errors.New("password hash contains an invalid digest")
	}
	return parameters, salt, hash, nil
}
