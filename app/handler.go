package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/alsonduan/go-container-manager/internal/domain"
	"github.com/alsonduan/go-container-manager/internal/handler"
	"github.com/alsonduan/go-container-manager/internal/repository/docker"
	pgxrepo "github.com/alsonduan/go-container-manager/internal/repository/postgres"
	"github.com/alsonduan/go-container-manager/internal/service"
	"github.com/alsonduan/go-container-manager/pkg/logger"
)

const (
	defaultJWTSecret    = "test-secret-key-at-least-32-characters-long"
	defaultStorageRoot  = "/tmp/aidms-storage"
	defaultWorkerPoolSize = 4
)

// App holds the HTTP handler and provides a Stop method for graceful shutdown.
type App struct {
	Handler http.Handler
	stop    func()
}

// Stop drains the worker pool and performs cleanup. Call before server shutdown.
func (a *App) Stop() {
	if a.stop != nil {
		a.stop()
	}
}

// NewHandler creates a new App with all dependencies. Requires DATABASE_URL env var.
// Config: JWT_SECRET, STORAGE_ROOT, WORKER_POOL_SIZE (optional).
func NewHandler() *App {
	logger.Init()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}

	// Config from env (Twelve-Factor)
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = defaultJWTSecret
	}
	storageRoot := os.Getenv("STORAGE_ROOT")
	if storageRoot == "" {
		storageRoot = defaultStorageRoot
	}
	storageHostPath := os.Getenv("STORAGE_HOST_PATH") // for Docker bind mount when server runs in container
	workerPoolSize := defaultWorkerPoolSize
	if n := os.Getenv("WORKER_POOL_SIZE"); n != "" {
		if parsed, err := parseInt(n); err == nil && parsed > 0 {
			workerPoolSize = parsed
		}
	}

	// Run migrations on startup
	if err := runMigrations(databaseURL); err != nil {
		slog.Error("migrations failed", "error", err)
		os.Exit(1)
	}

	// PostgreSQL connection pool
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		slog.Error("connect to database failed", "error", err)
		os.Exit(1)
	}

	// PostgreSQL repositories
	userRepo := pgxrepo.NewUserRepository(pool)
	fileRepo := pgxrepo.NewFileRepository(pool)
	containerRepo := pgxrepo.NewContainerRepository(pool)
	jobRepo := pgxrepo.NewJobRepository(pool)
	txManager := pgxrepo.NewTxManager(pool) // For row-level lock in Start/Stop/Delete

	// Container runtime: Docker SDK (real containers)
	runtime, err := docker.NewContainerRuntime()
	if err != nil {
		slog.Error("docker runtime init failed", "error", err)
		os.Exit(1)
	}

	// Environment whitelist
	envMap := map[string]string{
		"python-3.10": "python:3.10-slim",
		"ubuntu-base": "ubuntu:22.04",
	}
	environments := []string{"python-3.10", "ubuntu-base"}

	// Job worker pool
	jobChan := make(chan *domain.Job, 100)

	// Stuck Job Recovery: mark RUNNING jobs as FAILED before accepting requests
	ctx := context.Background()
	_ = jobRepo.MarkRunningAsFailed(ctx, "System interrupted")

	// Re-enqueue PENDING jobs from DB (e.g. after graceful shutdown or restart)
	pendingJobs, _ := jobRepo.FindPendingJobs(ctx)
enqueueLoop:
	for _, j := range pendingJobs {
		select {
		case jobChan <- j:
		default:
			break enqueueLoop // channel full, stop re-enqueue
		}
	}

	worker := service.NewWorker(jobRepo, containerRepo, runtime, envMap, storageRoot, storageHostPath, jobChan, workerPoolSize)

	// Job retention: clean up expired jobs every hour
	jobCleaner := service.NewJobCleaner(jobRepo, 1*time.Hour)

	// Services
	authSvc := service.NewAuthService(userRepo, jwtSecret)
	fileSvc := service.NewFileService(fileRepo, storageRoot, txManager)
	containerSvc := service.NewContainerService(containerRepo, jobRepo, runtime, envMap, jobChan, txManager)
	jobSvc := service.NewJobService(jobRepo)

	router := handler.NewRouter(authSvc, fileSvc, containerSvc, jobSvc, jwtSecret, environments)
	return &App{
		Handler: router,
		stop: func() {
			worker.Stop()
			jobCleaner.Stop()
		},
	}
}

func runMigrations(databaseURL string) error {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("postgres driver: %w", err)
	}

	migrationsPath := findMigrationsPath()
	m, err := migrate.NewWithDatabaseInstance("file://"+migrationsPath, "postgres", driver)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

func parseInt(s string) (int, error) {
	return strconv.Atoi(s)
}

func findMigrationsPath() string {
	if p := os.Getenv("MIGRATIONS_PATH"); p != "" {
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
	}
	// Walk up from cwd to find migrations/ next to go.mod
	wd, err := os.Getwd()
	if err != nil {
		return "migrations"
	}
	for dir := wd; dir != ""; dir = filepath.Dir(dir) {
		p := filepath.Join(dir, "migrations")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		if dir == "/" {
			break
		}
	}
	return "migrations"
}
