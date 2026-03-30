package domain

import (
	"context"
	"time"
)

type User struct {
	ID           string
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

type UserRepository interface {
	FindByUsername(ctx context.Context, username string) (*User, error)
}
