package server

import (
	"context"
	"strconv"
	"strings"
	"time"

	"firebase.google.com/go/v4/auth"
	"github.com/adopti/notification-service/internal/repository"
	"github.com/gin-gonic/gin"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

type PushRoutable interface {
	RegisterDeviceToken(c *gin.Context)
}

func NewRouter(
	logLevel string,
	repo repository.NotificationRepository,
	pushHandler PushRoutable,
	authClient *auth.Client,
) *gin.Engine {
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
		// Registrar device-token requiere identidad: protegemos con el mismo
		// middleware que /notifications.
		api.POST("/device-tokens",
			requireFirebaseAuth(authClient),
			pushHandler.RegisterDeviceToken,
		)
	}

	api.GET("/notifications",
		requireFirebaseAuth(authClient),
		func(c *gin.Context) {
			tokenUID, _ := c.Get(ctxKeyUID)
			uidFromToken, _ := tokenUID.(string)

			// Si pasan ?userId, debe coincidir con el uid del token.
			// Si no lo pasan, usamos el uid autenticado.
			userID := c.Query("userId")
			if userID == "" {
				userID = uidFromToken
			} else if userID != uidFromToken {
				c.JSON(403, gin.H{"error": "cannot read notifications of another user"})
				return
			}

			status := c.Query("status")
			limit := parsePositiveInt(c.Query("limit"), defaultLimit)
			if limit > maxLimit {
				limit = maxLimit
			}
			offset := parsePositiveInt(c.Query("offset"), 0)

			notifications, err := repo.FindByUserID(c.Request.Context(), userID, status, limit, offset)
			if err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}

			c.JSON(200, gin.H{
				"data":   notifications,
				"limit":  limit,
				"offset": offset,
			})
		},
	)

	return r
}

const ctxKeyUID = "uid"

// requireFirebaseAuth valida el header `Authorization: Bearer <idToken>`.
// Si authClient es nil (no debería ocurrir tras el fail-fast del boot)
// rechazamos el request con 503: sin autenticación no servimos endpoints
// protegidos.
func requireFirebaseAuth(authClient *auth.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		if authClient == nil {
			c.AbortWithStatusJSON(503, gin.H{"error": "auth service unavailable"})
			return
		}
		raw := c.GetHeader("Authorization")
		if raw == "" {
			c.AbortWithStatusJSON(401, gin.H{"error": "missing Authorization header"})
			return
		}
		const prefix = "Bearer "
		if !strings.HasPrefix(raw, prefix) {
			c.AbortWithStatusJSON(401, gin.H{"error": "invalid Authorization scheme"})
			return
		}
		idToken := strings.TrimSpace(raw[len(prefix):])
		if idToken == "" {
			c.AbortWithStatusJSON(401, gin.H{"error": "empty bearer token"})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		token, err := authClient.VerifyIDToken(ctx, idToken)
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "invalid or expired token"})
			return
		}
		c.Set(ctxKeyUID, token.UID)
		c.Next()
	}
}

func parsePositiveInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return fallback
	}
	return v
}
