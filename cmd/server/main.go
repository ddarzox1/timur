// Точка входа Qauipsiz: собирает зависимости и запускает HTTP-сервер.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"qauipsiz/internal/config"
	httpdelivery "qauipsiz/internal/delivery/http"
	"qauipsiz/internal/infra/sms"
	"qauipsiz/internal/infra/storage"
	"qauipsiz/internal/repository/postgres"
	"qauipsiz/internal/usecase/auth"
	"qauipsiz/internal/usecase/checklist"
	"qauipsiz/internal/usecase/dashboard"
	"qauipsiz/internal/usecase/hazard"
	"qauipsiz/internal/usecase/induction"
	"qauipsiz/internal/usecase/people"
	"qauipsiz/internal/usecase/pilot"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		return err
	}
	if cfg.FixedOTP != "" {
		slog.Warn("⚠️ ТЕСТОВЫЙ РЕЖИМ: код входа у всех номеров одинаковый — не используйте в проде", "FIXED_OTP", cfg.FixedOTP)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if cfg.Seed {
		if err := postgres.Seed(ctx, pool); err != nil {
			return err
		}
	}

	h := NewHandler(postgres.New(pool), cfg)
	srv := &http.Server{Addr: cfg.Addr, Handler: h.Routes(),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute, WriteTimeout: 2 * time.Minute, IdleTimeout: 2 * time.Minute}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	slog.Info("Qauipsiz запущен", "addr", cfg.Addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// NewHandler собирает все слои: репозиторий → usecase → HTTP.
func NewHandler(store *postgres.Store, cfg config.Config) *httpdelivery.Handler {
	files := storage.Local{Dir: cfg.UploadsDir}
	sender := sms.Log{}
	hazards := hazard.New(store, store, files)
	return httpdelivery.New(httpdelivery.Services{
		Auth:       auth.New(store, sender, cfg.FixedOTP),
		Hazards:    hazards,
		Inductions: induction.New(store, store, files, sender, cfg.BaseURL),
		Checklists: checklist.New(store, store, files, hazards),
		People:     people.New(store, files),
		Dashboard:  dashboard.New(store),
		Pilot:      pilot.New(store),
	}, files, httpdelivery.Options{CookieSecure: cfg.CookieSecure, PricePerUser: cfg.PricePerUser})
}
