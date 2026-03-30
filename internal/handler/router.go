package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/alsonduan/go-container-manager/internal/handler/middleware"
	"github.com/alsonduan/go-container-manager/internal/service"
)

func NewRouter(
	authSvc *service.AuthService,
	fileSvc *service.FileService,
	containerSvc *service.ContainerService,
	jobSvc *service.JobService,
	jwtSecret string,
	environments []string,
) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.RequestLogger())

	healthH := NewHealthHandler(environments)
	authH := NewAuthHandler(authSvc)
	fileH := NewFileHandler(fileSvc)
	containerH := NewContainerHandler(containerSvc)
	jobH := NewJobHandler(jobSvc)

	api := r.Group("/api/v1")
	api.Use(middleware.MaxBytesDefault())
	{
		// Public routes
		api.GET("/health", healthH.Health)
		api.GET("/environments", healthH.Environments)
		api.POST("/login", authH.Login)

		// Authenticated routes
		auth := api.Group("")
		auth.Use(middleware.AuthMiddleware(jwtSecret))
		{
			auth.POST("/files/upload", fileH.Upload)
			auth.GET("/files", fileH.List)
			auth.DELETE("/files/:file_id", fileH.Delete)

			auth.POST("/containers", containerH.Create)
			auth.GET("/containers", containerH.List)
			auth.GET("/containers/:container_id", containerH.Get)
			auth.POST("/containers/:container_id/start", containerH.Start)
			auth.POST("/containers/:container_id/stop", containerH.Stop)
			auth.DELETE("/containers/:container_id", containerH.Delete)

			auth.GET("/jobs/:job_id", jobH.GetJob)
		}
	}

	return r
}
