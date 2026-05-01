package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/adopti/notification-service/internal/config"
	"github.com/adopti/notification-service/internal/handlers"
	"github.com/adopti/notification-service/internal/messaging"
	"github.com/adopti/notification-service/internal/repository"
	"github.com/adopti/notification-service/internal/server"
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
	postgresRepo, err := repository.NewPostgresRepo(repoCtx, cfg.PostgresDSN)
	if err != nil {
		logger.Fatal("Failed to initialize postgres repository", zap.Error(err))
	}

	consumerCtx, consumerCancel := context.WithCancel(context.Background())
	consumer, err := messaging.NewConsumer(cfg.RabbitMQURL, logger)
	if err != nil {
		logger.Warn("RabbitMQ unavailable at startup, will keep trying", zap.Error(err))
	}
	emailHandler := handlers.NewEmailHandler(cfg, postgresRepo, logger)
	dispatcher := messaging.NewDispatcher(logger, emailHandler)

	go func() {
		if err := consumer.Start(consumerCtx, dispatcher.Dispatch); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("Consumer stopped", zap.Error(err))
		}
	}()

	router := server.NewRouter(cfg.LogLevel, postgresRepo)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	go func() {
		logger.Info("Starting server", zap.String("port", cfg.Port))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("Listen and serve failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("Shutting down server...")

	consumerCancel()
	consumer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server exiting")
}
