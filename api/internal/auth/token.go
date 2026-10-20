package auth

import (
	"errors"
	"fmt"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// MinSecretBytes is the minimum HS256 signing secret length (256 bits).
const MinSecretBytes = 32

// tokenIssuer is the fixed `iss` claim: tokens are minted and verified by the same
// API, so this only guards against accepting some other HS256 token by accident.
const tokenIssuer = "lookupkb"

// ErrInvalidToken covers every verification failure (bad signature, wrong
// algorithm/issuer, expired, missing claims). Callers map it to a bare 401.
var ErrInvalidToken = errors.New("invalid token")

// Claims is the verified content of an access token.
type Claims struct {
	// UserID is the internal users.id (the `sub` claim).
	UserID string
	Email  string
	Expiry time.Time
}

// tokenClaims is the wire shape: registered claims plus the email.
type tokenClaims struct {
	jwt.Claims
	Email string `json:"email"`
}

// Tokens issues and verifies the API's HS256 access tokens.
type Tokens struct {
	key    []byte
	ttl    time.Duration
	signer jose.Signer
	now    func() time.Time
}

// NewTokens builds a token issuer/verifier from the shared secret. The secret
// must be at least MinSecretBytes long; ttl bounds each token's lifetime.
func NewTokens(secret string, ttl time.Duration) (*Tokens, error) {
	if len(secret) < MinSecretBytes {
		return nil, fmt.Errorf("auth secret must be at least %d bytes", MinSecretBytes)
	}
	if ttl <= 0 {
		return nil, errors.New("auth token ttl must be positive")
	}
	key := []byte(secret)
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.HS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		return nil, fmt.Errorf("build token signer: %w", err)
	}
	return &Tokens{key: key, ttl: ttl, signer: signer, now: time.Now}, nil
}

// Issue mints a token for the user, returning it and its expiry.
func (t *Tokens) Issue(userID, email string) (string, time.Time, error) {
	now := t.now()
	exp := now.Add(t.ttl)
	raw, err := jwt.Signed(t.signer).Claims(tokenClaims{
		Claims: jwt.Claims{
			Issuer:   tokenIssuer,
			Subject:  userID,
			IssuedAt: jwt.NewNumericDate(now),
			Expiry:   jwt.NewNumericDate(exp),
		},
		Email: email,
	}).Serialize()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return raw, exp, nil
}

// Verify checks the signature (HS256 only — no alg confusion), issuer and expiry
// and returns the claims. Every failure is ErrInvalidToken wrapped with a reason
// that is safe to log (it never contains the token).
func (t *Tokens) Verify(raw string) (Claims, error) {
	tok, err := jwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.HS256})
	if err != nil {
		return Claims{}, fmt.Errorf("%w: parse: %v", ErrInvalidToken, err)
	}
	var c tokenClaims
	if err := tok.Claims(t.key, &c); err != nil {
		return Claims{}, fmt.Errorf("%w: signature: %v", ErrInvalidToken, err)
	}
	// go-jose treats a missing exp as "never expires"; ours always carry one.
	if c.Expiry == nil || c.Subject == "" {
		return Claims{}, fmt.Errorf("%w: missing exp or sub", ErrInvalidToken)
	}
	if err := c.ValidateWithLeeway(jwt.Expected{Issuer: tokenIssuer, Time: t.now()}, 0); err != nil {
		return Claims{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	return Claims{UserID: c.Subject, Email: c.Email, Expiry: c.Expiry.Time()}, nil
}
