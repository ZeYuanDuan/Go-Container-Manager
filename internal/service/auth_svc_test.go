package service_test

import (
	"context"
	"testing"

	"github.com/alsonduan/go-container-manager/internal/domain"
	"github.com/alsonduan/go-container-manager/internal/service"
	apperr "github.com/alsonduan/go-container-manager/pkg/errors"
	appjwt "github.com/alsonduan/go-container-manager/pkg/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

const testJWTSecret = "test-secret-key-at-least-32-characters-long"

// --- mock ---

type mockUserRepo struct {
	user *domain.User
	err  error
}

func (m *mockUserRepo) FindByUsername(_ context.Context, _ string) (*domain.User, error) {
	return m.user, m.err
}

// --- helpers ---

func hashPassword(t *testing.T, password string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	return string(h)
}

// --- tests ---

func TestLogin_UserNotFound_Unauthorized(t *testing.T) {
	svc := service.NewAuthService(&mockUserRepo{err: domain.ErrNotFound}, testJWTSecret)
	_, _, err := svc.Login(context.Background(), "nouser", "pass")
	assert.ErrorIs(t, err, apperr.ErrUnauthorized)
}

func TestLogin_WrongPassword_Unauthorized(t *testing.T) {
	user := &domain.User{ID: "uid-1", Username: "user1", PasswordHash: hashPassword(t, "correctpass")}
	svc := service.NewAuthService(&mockUserRepo{user: user}, testJWTSecret)
	_, _, err := svc.Login(context.Background(), "user1", "wrongpass")
	assert.ErrorIs(t, err, apperr.ErrUnauthorized)
}

func TestLogin_HappyPath_ReturnsTokenAndExpiry(t *testing.T) {
	user := &domain.User{ID: "uid-1", Username: "user1", PasswordHash: hashPassword(t, "password123")}
	svc := service.NewAuthService(&mockUserRepo{user: user}, testJWTSecret)

	token, expiresIn, err := svc.Login(context.Background(), "user1", "password123")
	require.NoError(t, err)
	assert.Equal(t, 3600, expiresIn)
	assert.NotEmpty(t, token)

	// Validate the token contains correct userID
	uid, err := appjwt.ValidateToken(token, testJWTSecret)
	require.NoError(t, err)
	assert.Equal(t, "uid-1", uid)
}
