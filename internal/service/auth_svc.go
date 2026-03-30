package service

import (
	"context"
	"time"

	"github.com/alsonduan/go-container-manager/internal/domain"
	apperr "github.com/alsonduan/go-container-manager/pkg/errors"
	appjwt "github.com/alsonduan/go-container-manager/pkg/jwt"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	userRepo  domain.UserRepository
	jwtSecret string
}

func NewAuthService(userRepo domain.UserRepository, jwtSecret string) *AuthService {
	return &AuthService{userRepo: userRepo, jwtSecret: jwtSecret}
}

func (s *AuthService) Login(ctx context.Context, username, password string) (token string, expiresIn int, err error) {
	user, err := s.userRepo.FindByUsername(ctx, username)
	if err != nil {
		return "", 0, apperr.ErrUnauthorized
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", 0, apperr.ErrUnauthorized
	}
	expiresIn = 3600
	token, err = appjwt.GenerateToken(user.ID, s.jwtSecret, time.Duration(expiresIn)*time.Second)
	if err != nil {
		return "", 0, err
	}
	return token, expiresIn, nil
}
