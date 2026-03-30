package jwt

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "test-secret-key-at-least-32-characters-long"

func TestGenerateAndValidate_RoundTrip(t *testing.T) {
	token, err := GenerateToken("user-123", testSecret, time.Hour)
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	userID, err := ValidateToken(token, testSecret)
	require.NoError(t, err)
	assert.Equal(t, "user-123", userID)
}

func TestValidateToken_WrongSecret(t *testing.T) {
	token, err := GenerateToken("user-123", testSecret, time.Hour)
	require.NoError(t, err)

	_, err = ValidateToken(token, "wrong-secret-key-at-least-32-chars")
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestValidateToken_ExpiredToken(t *testing.T) {
	token, err := GenerateToken("user-123", testSecret, -time.Second)
	require.NoError(t, err)

	_, err = ValidateToken(token, testSecret)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestValidateToken_MalformedToken(t *testing.T) {
	_, err := ValidateToken("not.a.valid.jwt", testSecret)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestValidateToken_EmptyString(t *testing.T) {
	_, err := ValidateToken("", testSecret)
	assert.ErrorIs(t, err, ErrInvalidToken)
}
