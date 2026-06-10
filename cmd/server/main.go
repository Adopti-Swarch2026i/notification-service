package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/adopti/notification-service/internal/config"
	"github.com/adopti/notification-service/internal/discovery"
	"github.com/adopti/notification-service/internal/handlers"
	msg "github.com/adopti/notification-service/internal/messaging"
	"github.com/adopti/notification-service/internal/repository"
	"github.com/adopti/notification-service/internal/server"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"firebase.google.com/go/v4/messaging"
	"go.uber.org/zap"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	var logger *zap.Logger
	if cfg.LogLevel == "debug" {
		logger, _ = zap.NewDevelopment()
	} else {
		logger, _ = zap.NewProduction()
	}
	defer logger.Sync()

	repoCtx, repoCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer repoCancel()
	postgresRepo, err := repository.NewPostgresRepo(repoCtx, cfg.PostgresDSN, cfg.PostgresReplicaDSN)
	if err != nil {
		logger.Fatal("Failed to initialize postgres repository", zap.String("error", config.SanitizeError(err)))
	}

	consumerCtx, consumerCancel := context.WithCancel(context.Background())
	consumer, err := msg.NewConsumer(cfg.RabbitMQURL, logger)
	if err != nil {
		logger.Warn("RabbitMQ unavailable at startup, will keep trying", zap.String("error", config.SanitizeError(err)))
	}

	app, err := firebase.NewApp(context.Background(), nil)
	if err != nil {
		logger.Fatal("Failed to initialize firebase app globally", zap.Error(err))
	}
	var authClient *auth.Client
	authClient, err = app.Auth(context.Background())
	if err != nil {
		logger.Fatal("Failed to allocate firebase Auth client", zap.Error(err))
	}
	var msgClient *messaging.Client
	msgClient, err = app.Messaging(context.Background())
	if err != nil {
		logger.Warn("Failed to allocate firebase Messaging client (push degraded)", zap.Error(err))
	}

	pushHandler := handlers.NewPushHandler(msgClient, postgresRepo, logger)
	emailHandler := handlers.NewEmailHandler(cfg, authClient, postgresRepo, logger)
	dispatcher := msg.NewDispatcher(logger, emailHandler, pushHandler)

	go func() {
		if err := consumer.Start(consumerCtx, dispatcher.Dispatch); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("Consumer stopped", zap.Error(err))
		}
	}()

	router := server.NewRouter(cfg.LogLevel, postgresRepo, pushHandler, authClient)

	// ── TLS configuration (mTLS) ─────────────────────────
	caPEM, err := os.ReadFile(cfg.TLSCAPath)
	if err != nil {
		logger.Fatal("Failed to read CA certificate", zap.Error(err))
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		logger.Fatal("Failed to append CA certificate to pool")
	}

	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ClientCAs:  caPool,
		ClientAuth: tls.RequireAndVerifyClientCert,
	}

	srv := &http.Server{
		Addr:      ":" + cfg.Port,
		Handler:   router,
		TLSConfig: tlsCfg,
	}

	go func() {
		logger.Info("Starting HTTPS server", zap.String("port", cfg.Port))
		if err := srv.ListenAndServeTLS(cfg.TLSCertPath, cfg.TLSKeyPath); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("ListenAndServeTLS failed", zap.Error(err))
		}
	}()

	discovery.Register(logger)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("Shutting down server...")

	discovery.Deregister(logger)

	consumerCancel()
	consumer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server exiting")
}
