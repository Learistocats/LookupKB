package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPassword_HashAndVerify(t *testing.T) {
	h, err := HashPassword("correct horse battery staple")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(h, "$argon2id$v=19$"))
	require.NotContains(t, h, "correct horse")

	require.True(t, VerifyPassword(h, "correct horse battery staple"))
	require.False(t, VerifyPassword(h, "Correct horse battery staple"))
}

func TestPassword_SaltedHashesDiffer(t *testing.T) {
	a, err := HashPassword("same password here")
	require.NoError(t, err)
	b, err := HashPassword("same password here")
	require.NoError(t, err)
	require.NotEqual(t, a, b)
}

func TestPassword_MalformedHashNeverMatches(t *testing.T) {
	for _, h := range []string{"", "plaintext", "$argon2i$v=19$m=1,t=1,p=1$c2FsdA$a2V5", "$argon2id$v=19$m=x$a$b"} {
		require.False(t, VerifyPassword(h, "anything"), "hash %q", h)
	}
}
