package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/jackc/pgx/v5"
)

// Personal Access Tokens.
//
// Wire format: lkb_pat_<prefix>_<secret>
//   - prefix: PATPrefixLen random base62 chars — NOT secret; stored in clear,
//     shown in the UI and used as the lookup key.
//   - secret: patSecretLen random base62 chars from crypto/rand (~256 bits).
//
// Only token_hash = hex(SHA-256(full token)) is persisted. A plain unsalted hash
// is deliberate and sufficient here: unlike a password the input is a uniformly
// random ≥256-bit secret, so there is nothing to brute-force or precompute and a
// salt/KDF would add per-request cost without adding security.
const (
	// PATScheme marks a bearer credential as a PAT (vs a session JWT).
	PATScheme = "lkb_pat_"
	// PATPrefixLen is the length of the non-secret, displayed prefix.
	PATPrefixLen = 8
	// patSecretLen base62 chars ≈ 43 × log2(62) ≈ 256 bits of entropy.
	patSecretLen = 43
)

const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// ErrInvalidPAT covers every PAT verification failure (malformed, unknown
// prefix, hash mismatch, revoked, expired). Callers map it to a bare 401.
var ErrInvalidPAT = errors.New("invalid personal access token")

// NewPAT is a freshly minted token. Token is the plaintext and must be shown to
// the user exactly once and never logged or persisted.
type NewPAT struct {
	Token  string
	Prefix string
	Hash   string
}

// GeneratePAT mints a new token from crypto/rand.
func GeneratePAT() (NewPAT, error) {
	prefix, err := randomBase62(PATPrefixLen)
	if err != nil {
		return NewPAT{}, err
	}
	secret, err := randomBase62(patSecretLen)
	if err != nil {
		return NewPAT{}, err
	}
	tok := PATScheme + prefix + "_" + secret
	return NewPAT{Token: tok, Prefix: prefix, Hash: HashPAT(tok)}, nil
}

// HashPAT returns the persisted form of a token: hex SHA-256 of the full token.
func HashPAT(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// IsPAT reports whether a bearer credential claims to be a PAT.
func IsPAT(raw string) bool {
	return strings.HasPrefix(raw, PATScheme)
}

// ParsePAT extracts the lookup prefix from a well-formed token.
func ParsePAT(raw string) (prefix string, ok bool) {
	rest, found := strings.CutPrefix(raw, PATScheme)
	if !found || len(rest) != PATPrefixLen+1+patSecretLen || rest[PATPrefixLen] != '_' {
		return "", false
	}
	prefix = rest[:PATPrefixLen]
	if !isBase62(prefix) || !isBase62(rest[PATPrefixLen+1:]) {
		return "", false
	}
	return prefix, true
}

// lastUsedGranularity bounds how often a token's last_used_at is written.
const lastUsedGranularity = time.Minute

// verifyPAT authenticates a raw PAT against the store and returns the token's
// row (owner included). Every failure is ErrInvalidPAT wrapped with a log-safe
// reason (never the token).
func verifyPAT(ctx context.Context, store Store, raw string, now time.Time) (dbgen.GetActiveAPITokenByPrefixRow, error) {
	var none dbgen.GetActiveAPITokenByPrefixRow
	prefix, ok := ParsePAT(raw)
	if !ok {
		return none, fmt.Errorf("%w: malformed", ErrInvalidPAT)
	}
	row, err := store.GetActiveAPITokenByPrefix(ctx, prefix)
	if errors.Is(err, pgx.ErrNoRows) {
		return none, fmt.Errorf("%w: unknown or inactive prefix", ErrInvalidPAT)
	}
	if err != nil {
		return none, err
	}
	// Constant-time compare so timing reveals nothing about how much of a guessed
	// secret matched.
	if subtle.ConstantTimeCompare([]byte(HashPAT(raw)), []byte(row.TokenHash)) != 1 {
		return none, fmt.Errorf("%w: hash mismatch", ErrInvalidPAT)
	}
	// The query filters on now() in SQL; re-checked against the app clock.
	if row.ExpiresAt.Valid && !now.Before(row.ExpiresAt.Time) {
		return none, fmt.Errorf("%w: expired", ErrInvalidPAT)
	}
	return row, nil
}

func randomBase62(n int) (string, error) {
	out := make([]byte, 0, n)
	buf := make([]byte, n+n/2)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("read random: %w", err)
		}
		for _, b := range buf {
			// Rejection sampling: 248 = 4×62, so b%62 is uniform for b < 248.
			if b >= 248 {
				continue
			}
			out = append(out, base62[b%62])
			if len(out) == n {
				break
			}
		}
	}
	return string(out), nil
}

func isBase62(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return s != ""
}
