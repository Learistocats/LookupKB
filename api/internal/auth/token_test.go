package auth

import (
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/require"
)

func TestNewTokens_RejectsShortSecretAndBadTTL(t *testing.T) {
	_, err := NewTokens("too-short", time.Hour)
	require.Error(t, err)
	_, err = NewTokens(testSecret, 0)
	require.Error(t, err)
}

func TestTokens_RoundTrip(t *testing.T) {
	tk := newTestTokens(t)
	raw, exp, err := tk.Issue("7f9c0e8e-0000-4000-8000-000000000001", "a@example.com")
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(time.Hour), exp, 5*time.Second)

	c, err := tk.Verify(raw)
	require.NoError(t, err)
	require.Equal(t, "7f9c0e8e-0000-4000-8000-000000000001", c.UserID)
	require.Equal(t, "a@example.com", c.Email)
}

func TestTokens_ExpiredIsRejected(t *testing.T) {
	tk := newTestTokens(t)
	tk.now = func() time.Time { return time.Now().Add(-2 * time.Hour) }
	raw, _, err := tk.Issue("u", "a@example.com")
	require.NoError(t, err)

	tk.now = time.Now
	_, err = tk.Verify(raw)
	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestTokens_TamperedIsRejected(t *testing.T) {
	tk := newTestTokens(t)
	raw, _, err := tk.Issue("u", "a@example.com")
	require.NoError(t, err)

	_, err = tk.Verify(raw[:len(raw)-2] + "xx")
	require.ErrorIs(t, err, ErrInvalidToken)
}

// A token without exp must not be accepted as "never expires".
func TestTokens_MissingExpiryIsRejected(t *testing.T) {
	tk := newTestTokens(t)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: []byte(testSecret)}, nil)
	require.NoError(t, err)
	raw, err := jwt.Signed(signer).Claims(jwt.Claims{Issuer: tokenIssuer, Subject: "u"}).Serialize()
	require.NoError(t, err)

	_, err = tk.Verify(raw)
	require.ErrorIs(t, err, ErrInvalidToken)
}
