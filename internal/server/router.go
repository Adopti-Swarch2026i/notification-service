package server

import (
	"github.com/adopti/notification-service/internal/repository"
	"github.com/gin-gonic/gin"
)

type PushRoutable interface {
	RegisterDeviceToken(c *gin.Context)
}

func NewRouter(logLevel string, repo repository.NotificationRepository, pushHandler PushRoutable) *gin.Engine {
	if logLevel != "debug" {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}

	r := gin.Default()

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "ok",
			"service": "notification-service",
		})
	})

	api := r.Group("/api")
	if pushHandler != nil {
		api.POST("/device-tokens", pushHandler.RegisterDeviceToken)
	}

	return r
}
