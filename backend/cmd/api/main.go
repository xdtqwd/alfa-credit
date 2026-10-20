package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/httpapi"
	"github.com/xdtqwd/alfa-credit/internal/integrations"
	"github.com/xdtqwd/alfa-credit/internal/pipeline"
	"github.com/xdtqwd/alfa-credit/internal/service"
	"github.com/xdtqwd/alfa-credit/internal/storage"
	"github.com/xdtqwd/alfa-credit/web"
)

var version = "0.1.0"

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	health := flag.Bool("healthcheck", false, "probe /healthz and exit (for Docker HEALTHCHECK)")
	flag.Parse()

	addr := env("HTTP_ADDR", ":8080")
	if *health {
		c := http.Client{Timeout: 2 * time.Second}
		resp, err := c.Get("http://127.0.0.1" + addr + "/healthz")
		if err != nil || resp.StatusCode != 200 {
			os.Exit(1)
		}
		os.Exit(0)
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(log, addr); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, addr string) error {
	stepDelay, err := time.ParseDuration(env("PIPELINE_STEP_DELAY", "3s"))
	if err != nil {
		return fmt.Errorf("PIPELINE_STEP_DELAY: %w", err)
	}

	repo, err := storage.NewFileStore(env("DATA_FILE", "./data/applications.json"))
	if err != nil {
		return err
	}
	registry := integrations.MockRegistry{}
	svc := &service.Service{Repo: repo, Registry: registry, Now: time.Now}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	worker := &pipeline.Worker{
		Repo: repo, Registry: registry, Bureau: integrations.MockBureau{},
		StepDelay: stepDelay, Interval: 500 * time.Millisecond, Log: log, Now: time.Now,
	}
	go worker.Run(ctx)

	srv := &http.Server{
		Addr: addr,
		Handler: (&httpapi.Server{
			Svc: svc, Repo: repo, Log: log, Static: web.Static(), Version: version,
			BankToken: env("BANK_TOKEN", "demo-bank"),
		}).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", addr, "version", version, "step_delay", stepDelay.String())
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		return err
	}
	log.Info("shutting down")
	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shCtx)
}
