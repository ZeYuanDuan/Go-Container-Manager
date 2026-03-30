package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/alsonduan/go-container-manager/internal/domain"
)

const pgUniqueViolation = "23505"

type ContainerRepository struct {
	pool *pgxpool.Pool
}

func NewContainerRepository(pool *pgxpool.Pool) *ContainerRepository {
	return &ContainerRepository{pool: pool}
}

func (r *ContainerRepository) Save(ctx context.Context, container *domain.Container) error {
	q := getQueryable(ctx, r.pool)
	cmdJSON, err := json.Marshal(container.Command)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx,
		`INSERT INTO containers (id, user_id, name, environment, command, status, mount_path, docker_container_id, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		container.ID, container.UserID, container.Name, container.Environment, cmdJSON, container.Status,
		container.MountPath, nullIfEmpty(container.DockerContainerID), container.CreatedAt, container.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return domain.ErrDuplicate
		}
		return err
	}
	return nil
}

func (r *ContainerRepository) FindByUserID(ctx context.Context, userID string, page, limit int) ([]*domain.Container, int, error) {
	q := getQueryable(ctx, r.pool)

	var total int
	err := q.QueryRow(ctx, `SELECT COUNT(*) FROM containers WHERE user_id = $1`, userID).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	rows, err := q.Query(ctx,
		`SELECT id, user_id, name, environment, command, status, mount_path, docker_container_id, created_at, updated_at
		 FROM containers WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var containers []*domain.Container
	for rows.Next() {
		c, err := scanContainer(rows)
		if err != nil {
			return nil, 0, err
		}
		containers = append(containers, c)
	}
	return containers, total, rows.Err()
}

func (r *ContainerRepository) FindByID(ctx context.Context, containerID string) (*domain.Container, error) {
	q := getQueryable(ctx, r.pool)
	rows, err := q.Query(ctx,
		`SELECT id, user_id, name, environment, command, status, mount_path, docker_container_id, created_at, updated_at
		 FROM containers WHERE id = $1`,
		containerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, domain.ErrNotFound
	}
	c, err := scanContainer(rows)
	if err != nil {
		return nil, err
	}
	return c, rows.Err()
}

func (r *ContainerRepository) FindByIDForUpdate(ctx context.Context, containerID string) (*domain.Container, error) {
	q := getQueryable(ctx, r.pool)
	rows, err := q.Query(ctx,
		`SELECT id, user_id, name, environment, command, status, mount_path, docker_container_id, created_at, updated_at
		 FROM containers WHERE id = $1 FOR UPDATE`,
		containerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, domain.ErrNotFound
	}
	c, err := scanContainer(rows)
	if err != nil {
		return nil, err
	}
	return c, rows.Err()
}

func (r *ContainerRepository) Update(ctx context.Context, container *domain.Container) error {
	q := getQueryable(ctx, r.pool)
	cmdJSON, err := json.Marshal(container.Command)
	if err != nil {
		return err
	}
	ct, err := q.Exec(ctx,
		`UPDATE containers SET name=$2, environment=$3, command=$4, status=$5, mount_path=$6, docker_container_id=$7, updated_at=$8
		 WHERE id = $1`,
		container.ID, container.Name, container.Environment, cmdJSON, container.Status,
		container.MountPath, nullIfEmpty(container.DockerContainerID), container.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *ContainerRepository) Delete(ctx context.Context, containerID string) error {
	q := getQueryable(ctx, r.pool)
	ct, err := q.Exec(ctx, `DELETE FROM containers WHERE id = $1`, containerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *ContainerRepository) FindByUserIDAndName(ctx context.Context, userID, name string) (*domain.Container, error) {
	q := getQueryable(ctx, r.pool)
	rows, err := q.Query(ctx,
		`SELECT id, user_id, name, environment, command, status, mount_path, docker_container_id, created_at, updated_at
		 FROM containers WHERE user_id = $1 AND name = $2`,
		userID, name,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, domain.ErrNotFound
	}
	c, err := scanContainer(rows)
	if err != nil {
		return nil, err
	}
	return c, rows.Err()
}

func scanContainer(rows pgx.Rows) (*domain.Container, error) {
	var c domain.Container
	var cmdJSON []byte
	var dockerID *string
	err := rows.Scan(&c.ID, &c.UserID, &c.Name, &c.Environment, &cmdJSON, &c.Status, &c.MountPath, &dockerID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(cmdJSON, &c.Command); err != nil {
		return nil, err
	}
	if dockerID != nil {
		c.DockerContainerID = *dockerID
	}
	return &c, nil
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
