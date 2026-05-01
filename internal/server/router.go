package server

import (
	"github.com/gin-gonic/gin"
)

func NewRouter(logLevel string) *gin.Engine {
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

	return r
}
