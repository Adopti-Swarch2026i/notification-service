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

	api.GET("/notifications", func(c *gin.Context) {
		userID := c.Query("userId")
		if userID == "" {
			c.JSON(400, gin.H{"error": "userId query param is required"})
			return
		}
		status := c.Query("status") // optional

		// simple hardcoded pagination for the prototype
		limit := 50
		offset := 0

		notifications, err := repo.FindByUserID(c.Request.Context(), userID, status, limit, offset)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{"data": notifications})
	})

	return r
}
