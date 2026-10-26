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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/auth"
	"github.com/xdtqwd/alfa-credit/internal/events"
	"github.com/xdtqwd/alfa-credit/internal/events/kafkabus"
	"github.com/xdtqwd/alfa-credit/internal/httpapi"
	"github.com/xdtqwd/alfa-credit/internal/idempotency"
	"github.com/xdtqwd/alfa-credit/internal/integrations"
	"github.com/xdtqwd/alfa-credit/internal/notify"
	"github.com/xdtqwd/alfa-credit/internal/obs"
	"github.com/xdtqwd/alfa-credit/internal/pipeline"
	"github.com/xdtqwd/alfa-credit/internal/service"
	"github.com/xdtqwd/alfa-credit/internal/storage"
	"github.com/xdtqwd/alfa-credit/internal/storage/pgstore"
	"github.com/xdtqwd/alfa-credit/web"
)

var version = "0.2.0"

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDur(key, def string) (time.Duration, error) {
	d, err := time.ParseDuration(env(key, def))
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}

// envFloat и envInt: пусто — значение по умолчанию, неразборчивое значение — ошибка запуска
// (как у envDur), а не тихая подмена значением по умолчанию. bits — разрядность целевого типа.
func envFloat(key string, def float64) (float64, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return f, nil
}

func envInt(key string, def int64, bits int) (int64, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, bits)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
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

	log := obs.NewLogger(os.Stdout, env("LOG_LEVEL", "info"), env("LOG_FORMAT", "json")).With("service", "credit-api", "version", version)
	slog.SetDefault(log)
	if err := run(log, addr); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, addr string) error {
	// все опечатки в настройках показываем разом, а не по одной за запуск
	var cfgErrs []error
	must := func(err error) { cfgErrs = append(cfgErrs, err) }
	dur := func(k, def string) time.Duration { v, err := envDur(k, def); must(err); return v }
	flt := func(k string, def float64) float64 { v, err := envFloat(k, def); must(err); return v }
	num := func(k string, def int64, bits int) int64 { v, err := envInt(k, def, bits); must(err); return v }
	stepDelay, sessionTTL, reqTimeout := dur("PIPELINE_STEP_DELAY", "3s"), dur("SESSION_TTL", "12h"), dur("REQUEST_TIMEOUT", "10s")
	registryTTL, bureauTTL := dur("CACHE_REGISTRY_TTL", "1h"), dur("CACHE_BUREAU_TTL", "10m")
	rateRPS, rateBurst := flt("RATE_LIMIT_RPS", 20), flt("RATE_LIMIT_BURST", 60)
	kafkaParts, kafkaRepl := num("KAFKA_PARTITIONS", 6, 32), num("KAFKA_REPLICATION", 1, 16)
	err := errors.Join(cfgErrs...)
	if err != nil {
		return err
	}
	role := env("APP_ROLE", "all") // all | api | worker — API и обработчики событий масштабируются отдельно
	runHTTP, runWorkers := role != "worker", role != "api"
	// секреты и cookie нужны только API; проверяем до подключения к базе — неверный конфиг падает сразу
	var sec securityConfig
	if runHTTP {
		if sec, err = loadSecurity(os.Getenv); err != nil {
			return err
		}
		for _, w := range sec.Warnings {
			log.Warn(w)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ---------- внешние сервисы (моки) под кэшем ----------
	registry := integrations.NewCachedRegistry(integrations.MockRegistry{}, registryTTL)
	bureau := integrations.NewCachedBureau(integrations.MockBureau{}, bureauTTL)

	// ---------- хранилище ----------
	var (
		repo      storage.Repository
		users     storage.UserRepository
		sessions  auth.SessionStore
		tx        storage.TxRunner
		idem      idempotency.Store
		pg        *pgstore.Store
		storeKind = "file"
	)
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		pg, err = pgstore.Open(ctx, dsn)
		if err != nil {
			return err
		}
		defer pg.Close()
		if err := pg.Migrate(ctx); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		repo, users, sessions, tx, idem, storeKind = pg.Apps(), pg.Users(), pg.Sessions(sessionTTL), pg, idempotency.PG{DB: pg.DB}, "postgres"
	} else {
		fr, err := storage.NewFileStore(env("DATA_FILE", "./data/applications.json"))
		if err != nil {
			return err
		}
		fu, err := storage.NewUserFileStore(env("USERS_FILE", "./data/users.json"))
		if err != nil {
			return err
		}
		repo, users, sessions, idem = fr, fu, auth.NewSessions(sessionTTL), idempotency.NewMemory(24*time.Hour)
	}

	// ---------- шина событий ----------
	var (
		pub      events.Publisher
		sub      events.Subscriber
		busKind  = "memory"
		ready    = map[string]func(context.Context) error{}
		closeBus = func() {}
	)
	if brokers := strings.TrimSpace(os.Getenv("KAFKA_BROKERS")); brokers != "" {
		kb, err := kafkabus.New(ctx, kafkabus.Config{Brokers: strings.Split(brokers, ","), ClientID: "credit-" + role,
			Partitions: int32(kafkaParts), Replication: int16(kafkaRepl), Log: log})
		if err != nil {
			return err
		}
		pub, sub, busKind, closeBus = kb, kb, "kafka", kb.Close
		ready["kafka"] = kb.Ping
	} else {
		mem := events.NewMemory(log)
		pub, sub = mem, mem
	}
	defer closeBus()
	if pg == nil {
		// без БД нет outbox: события публикуются сразу после изменения заявки
		repo = &storage.Publishing{Repository: repo, Bus: pub}
	}

	var auditSink obs.AuditSink
	if pg != nil {
		auditSink = pg
	}
	auditor := &obs.Auditor{Log: log, Sink: auditSink, Now: time.Now}

	svc := &service.Service{Repo: repo, Users: users, Registry: registry, Now: time.Now, Tx: tx, Audit: auditor}
	authSvc := &service.AuthService{
		Users: users, Registry: registry, ESIA: integrations.MockESIA{}, Now: time.Now, Audit: auditor,
		Sessions: sessions, Limiter: auth.NewLimiter(5, time.Minute),
	}
	if env("SEED_DEMO", "true") == "true" && runHTTP {
		if err := authSvc.SeedDemo(ctx); err != nil {
			return fmt.Errorf("seed demo users: %w", err)
		}
	}

	var wg sync.WaitGroup
	goRun := func(name string, fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
			log.Info("stopped", "component", name)
		}()
	}

	// ---------- outbox relay (БД → шина) ----------
	if pg != nil && runHTTP {
		goRun("outbox-relay", func() { relayLoop(ctx, log, pg, pub) })
		goRun("db-cleanup", func() { cleanupLoop(ctx, log, pg) })
		obs.Default.Gauge("outbox_pending_events", "События в outbox, ещё не отправленные в шину", func() []obs.Sample {
			c, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			n, err := pg.OutboxPending(c)
			if err != nil {
				return nil
			}
			return []obs.Sample{{Value: float64(n)}}
		})
	}

	// ---------- обработчики событий ----------
	if runWorkers {
		var inbox pipeline.Inbox
		if pg != nil {
			inbox = pg
		}
		worker := &pipeline.Worker{
			Repo: repo, Registry: registry, Bureau: bureau, Tx: tx, Inbox: inbox,
			StepDelay: stepDelay, Interval: 5 * time.Second, SweepAfter: stepDelay + 20*time.Second, Log: log, Now: time.Now,
		}
		notifier := &notify.Service{Log: log, Inbox: inbox}
		goRun("pipeline-consumer", func() { consume(ctx, log, sub, pipeline.Consumer, worker.Handle) })
		goRun("notify-consumer", func() { consume(ctx, log, sub, notify.Consumer, notifier.Handle) })
		goRun("pipeline-sweeper", func() { worker.Run(ctx) })
	}

	// ---------- служебный порт: /metrics ----------
	var metricsSrv *http.Server
	if maddr := env("METRICS_ADDR", ":9090"); maddr != "off" {
		mux := http.NewServeMux()
		mux.Handle("GET /metrics", httpapi.MetricsHandler())
		metricsSrv = &http.Server{Addr: maddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("metrics server", "err", err)
			}
		}()
	}

	// ---------- воркер: только health-проверки ----------
	if !runHTTP {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"status":"ok"}`)) })
		mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
			c, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			checks := map[string]func(context.Context) error{"storage": repo.Ping}
			for k, v := range ready {
				checks[k] = v
			}
			for name, fn := range checks {
				if err := fn(c); err != nil {
					http.Error(w, name+": "+err.Error(), http.StatusServiceUnavailable)
					return
				}
			}
			_, _ = w.Write([]byte(`{"status":"ready"}`))
		})
		hs := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("health server", "err", err) // без него Docker HEALTHCHECK пометит воркер нездоровым
			}
		}()
		log.Info("worker started", "storage", storeKind, "bus", busKind, "health", addr)
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(sh)
		if metricsSrv != nil {
			_ = metricsSrv.Shutdown(sh)
		}
		wg.Wait()
		return nil
	}

	// ---------- HTTP API ----------
	api := &httpapi.Server{
		Svc: svc, Auth: authSvc, CookieSecure: sec.CookieSecure, Repo: repo, Log: log,
		Static: web.Static(), Version: version, BankToken: sec.BankToken, DemoESIA: sec.Mode == modeDemo,
		Idem: idem, Limiter: httpapi.NewRateLimiter(rateRPS, rateBurst),
		// для API шина некритична: при недоступной Kafka заявки принимаются, события ждут в outbox
		RequestTimeout: reqTimeout, Degradable: ready,
		Platform: func(ctx context.Context) map[string]any {
			out := map[string]any{"storage": storeKind, "bus": busKind, "role": role}
			if pg != nil {
				if n, err := pg.OutboxPending(ctx); err == nil {
					out["outbox_pending"] = n
				}
			}
			return out
		},
	}
	if pg != nil {
		api.AuditLog = pg.AuditFor
	}
	srv := &http.Server{
		Addr: addr, Handler: api.Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 64 << 10,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", addr, "mode", sec.Mode, "cookie_secure", sec.CookieSecure, "storage", storeKind, "bus", busKind, "role", role, "step_delay", stepDelay.String())
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		stop()
		wg.Wait()
		return err
	}
	log.Info("shutting down: draining HTTP, then consumers")
	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = srv.Shutdown(shCtx)
	if metricsSrv != nil {
		_ = metricsSrv.Shutdown(shCtx)
	}
	stop()
	wg.Wait()
	return err
}

// consume держит подписку и переподключается при ошибках (брокер перезапускается, сеть моргнула).
func consume(ctx context.Context, log *slog.Logger, sub events.Subscriber, group string, h events.Handler) {
	for ctx.Err() == nil {
		if err := sub.Subscribe(ctx, events.TopicApplications, group, h); err != nil && ctx.Err() == nil {
			log.Error("consumer stopped, restarting", "group", group, "err", err)
			select {
			case <-ctx.Done():
			case <-time.After(2 * time.Second):
			}
		}
	}
}

// relayLoop — доставка outbox в шину. При недоступности Kafka события копятся в БД и уходят после
// восстановления; API при этом продолжает работать.
func relayLoop(ctx context.Context, log *slog.Logger, pg *pgstore.Store, pub events.Publisher) {
	backoff := 200 * time.Millisecond
	for ctx.Err() == nil {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		n, err := pg.RelayOutbox(c, pub, 100)
		cancel()
		switch {
		case err != nil && ctx.Err() == nil:
			log.Warn("outbox relay failed, will retry", "err", err, "backoff", backoff.String())
			obs.Default.Inc("outbox_relay_errors_total", "Ошибки доставки outbox в шину")
			backoff = min(backoff*2, 10*time.Second)
		case n > 0:
			obs.Default.Add("outbox_relayed_total", "События, доставленные из outbox в шину", int64(n))
			backoff = 200 * time.Millisecond
			continue // есть ещё — забираем сразу
		default:
			backoff = 200 * time.Millisecond
		}
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
	}
}

func cleanupLoop(ctx context.Context, log *slog.Logger, pg *pgstore.Store) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := pg.Cleanup(ctx); err != nil {
				log.Warn("db cleanup failed", "err", err)
			}
		}
	}
}
