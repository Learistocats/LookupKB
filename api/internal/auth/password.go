// Package auth owns LookupKB's local email/password authentication: argon2id
// password hashing, the HS256 access tokens the API signs and verifies itself,
// and the Fiber middleware that projects a verified caller onto the request.
//
// The API stays stateless: it never holds a session. The SvelteKit BFF stores the
// token in its encrypted session cookie and forwards it as a Bearer token.
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

// argon2id parameters (OWASP Password Storage Cheat Sheet minimums: m=19 MiB,
// t=2, p=1). Encoded into every hash so they can be raised later without
// invalidating existing passwords.
const (
	argonMemory  uint32 = 19 * 1024
	argonTime    uint32 = 2
	argonThreads uint8  = 1
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

// errMalformedHash is returned when a stored hash is not a parseable argon2id
// PHC string (e.g. the empty placeholder of a pre-revamp OIDC user).
var errMalformedHash = errors.New("malformed password hash")

// dummyHash is verified against when the login email is unknown, so a miss costs
// the same argon2 work as a wrong password and response timing cannot be used to
// enumerate accounts.
var dummyHash = mustHash("lookupkb-dummy-password")

// HashPassword returns the argon2id PHC-encoded hash of password with a fresh
// random salt: $argon2id$v=19$m=…,t=…,p=…$<salt>$<key>.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches the encoded hash, using a
// constant-time comparison. A malformed hash never matches.
func VerifyPassword(encoded, password string) bool {
	p, salt, key, err := decodeHash(encoded)
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, uint32(len(key))) //nolint:gosec // len(key) is bounded by the decoded hash
	return subtle.ConstantTimeCompare(got, key) == 1
}

// VerifyDummy burns the same work as a real verification. Call it when the user
// lookup misses so "unknown email" and "wrong password" take equal time.
func VerifyDummy(password string) {
	_ = VerifyPassword(dummyHash, password)
}

type argonParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

func decodeHash(encoded string) (argonParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return argonParams{}, nil, nil, errMalformedHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return argonParams{}, nil, nil, errMalformedHash
	}
	var p argonParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil {
		return argonParams{}, nil, nil, errMalformedHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return argonParams{}, nil, nil, errMalformedHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return argonParams{}, nil, nil, errMalformedHash
	}
	return p, salt, key, nil
}

func mustHash(password string) string {
	h, err := HashPassword(password)
	if err != nil {
		panic(err)
	}
	return h
}
