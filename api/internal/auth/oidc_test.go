package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"
)

const (
	testIssuer   = "https://authentik.example.com/application/o/lookupkb/"
	testAudience = "lookupkb-client-id"
)

// testKey is a small RSA key generated once for the whole test binary (2048-bit
// keygen per test is slow and adds nothing).
var testKey = mustRSAKey()

func mustRSAKey() *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return k
}

// newTestVerifier builds a Verifier backed by a static key set (no network), so
// tests exercise the exact signature/issuer/audience/expiry checks used in prod.
func newTestVerifier(t *testing.T, issuer, audience string, pub crypto.PublicKey) *Verifier {
	t.Helper()
	ks := &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{pub}}
	return VerifierFrom(oidc.NewVerifier(issuer, ks, &oidc.Config{ClientID: audience}))
}

// signToken serializes claims and RS256-signs them with the given key.
func signToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	require.NoError(t, err)

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	require.NoError(t, err)

	jws, err := signer.Sign(payload)
	require.NoError(t, err)
	serialized, err := jws.CompactSerialize()
	require.NoError(t, err)
	return serialized
}

// validClaims is a baseline good token; individual tests mutate a copy.
func validClaims() map[string]any {
	return map[string]any{
		"iss":    testIssuer,
		"aud":    testAudience,
		"sub":    "ak-sub-123",
		"email":  "user@example.com",
		"name":   "Test User",
		"groups": []string{"security", "hr"},
		"exp":    time.Now().Add(time.Hour).Unix(),
		"iat":    time.Now().Add(-time.Minute).Unix(),
	}
}

func TestVerify_ValidTokenReturnsClaims(t *testing.T) {
	v := newTestVerifier(t, testIssuer, testAudience, testKey.Public())
	raw := signToken(t, testKey, validClaims())

	claims, err := v.Verify(context.Background(), raw)
	require.NoError(t, err)
	require.Equal(t, "ak-sub-123", claims.Subject)
	require.Equal(t, "user@example.com", claims.Email)
	require.Equal(t, "Test User", claims.DisplayName())
	require.Equal(t, []string{"security", "hr"}, claims.Groups)
}

func TestVerify_TamperedSignatureRejected(t *testing.T) {
	v := newTestVerifier(t, testIssuer, testAudience, testKey.Public())
	raw := signToken(t, testKey, validClaims())

	// Corrupt the signature: flip the first char of the JWS signature segment,
	// whose bits are always significant (unlike the final char, which only
	// carries a couple of bits and can decode to the same bytes).
	tampered := tamperSignature(t, raw)
	require.NotEqual(t, raw, tampered)

	_, err := v.Verify(context.Background(), tampered)
	require.Error(t, err)
}

func TestVerify_WrongAudienceRejected(t *testing.T) {
	// Verifier expects testAudience; token minted for a different app.
	v := newTestVerifier(t, testIssuer, testAudience, testKey.Public())
	claims := validClaims()
	claims["aud"] = "some-other-app"
	raw := signToken(t, testKey, claims)

	_, err := v.Verify(context.Background(), raw)
	require.Error(t, err, "token for another Authentik app must be rejected (replay defense)")
}

func TestVerify_WrongIssuerRejected(t *testing.T) {
	v := newTestVerifier(t, testIssuer, testAudience, testKey.Public())
	claims := validClaims()
	claims["iss"] = "https://evil.example.com/"
	raw := signToken(t, testKey, claims)

	_, err := v.Verify(context.Background(), raw)
	require.Error(t, err)
}

func TestVerify_ExpiredTokenRejected(t *testing.T) {
	v := newTestVerifier(t, testIssuer, testAudience, testKey.Public())
	claims := validClaims()
	claims["exp"] = time.Now().Add(-time.Hour).Unix()
	raw := signToken(t, testKey, claims)

	_, err := v.Verify(context.Background(), raw)
	require.Error(t, err)
}

func TestVerify_WrongKeyRejected(t *testing.T) {
	// Verifier trusts testKey; token signed by a different key.
	other := mustRSAKey()
	v := newTestVerifier(t, testIssuer, testAudience, testKey.Public())
	raw := signToken(t, other, validClaims())

	_, err := v.Verify(context.Background(), raw)
	require.Error(t, err)
}

func TestVerify_MissingSubRejected(t *testing.T) {
	v := newTestVerifier(t, testIssuer, testAudience, testKey.Public())
	claims := validClaims()
	delete(claims, "sub")
	raw := signToken(t, testKey, claims)

	_, err := v.Verify(context.Background(), raw)
	require.Error(t, err)
}

func TestVerify_NoGroupsYieldsEmptySlice(t *testing.T) {
	v := newTestVerifier(t, testIssuer, testAudience, testKey.Public())
	claims := validClaims()
	delete(claims, "groups")
	raw := signToken(t, testKey, claims)

	got, err := v.Verify(context.Background(), raw)
	require.NoError(t, err)
	require.NotNil(t, got.Groups, "groups must never be nil for downstream ACL code")
	require.Empty(t, got.Groups)
}

func TestVerify_LargeGroupListParsed(t *testing.T) {
	v := newTestVerifier(t, testIssuer, testAudience, testKey.Public())
	groups := make([]string, 5000)
	for i := range groups {
		groups[i] = "group-" + strconv.Itoa(i)
	}
	claims := validClaims()
	claims["groups"] = groups
	raw := signToken(t, testKey, claims)

	got, err := v.Verify(context.Background(), raw)
	require.NoError(t, err)
	require.Len(t, got.Groups, 5000, "an oversized group list must not crash or truncate")
}

// tamperSignature flips the first character of the JWS signature segment to a
// different base64url character, reliably changing the decoded signature bytes.
func tamperSignature(t *testing.T, jwt string) string {
	t.Helper()
	parts := strings.Split(jwt, ".")
	require.Len(t, parts, 3, "expected a compact JWS with three segments")
	sig := parts[2]
	require.NotEmpty(t, sig)
	repl := byte('A')
	if sig[0] == 'A' {
		repl = 'B'
	}
	parts[2] = string(repl) + sig[1:]
	return strings.Join(parts, ".")
}
