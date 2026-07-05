// Package main — точка входа PANEL-компонента DSMS.
//
// PANEL живёт на manager-ноде Swarm, отдаёт SPA, REST API и WebSocket,
// принимает метрики от агентов и проксирует Docker Engine API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/docker/docker/api/types/events"

	"github.com/xakus/DSMS/panel/internal/agentclient"
	"github.com/xakus/DSMS/panel/internal/alerts"
	"github.com/xakus/DSMS/panel/internal/api"
	"github.com/xakus/DSMS/panel/internal/auth"
	"github.com/xakus/DSMS/panel/internal/config"
	"github.com/xakus/DSMS/panel/internal/crypto"
	"github.com/xakus/DSMS/panel/internal/dockerapi"
	"github.com/xakus/DSMS/panel/internal/history"
	"github.com/xakus/DSMS/panel/internal/metrics"
	"github.com/xakus/DSMS/panel/internal/store"
	"github.com/xakus/DSMS/panel/internal/streams"
	"github.com/xakus/DSMS/panel/internal/ws"
)

// wsNotifier — доставка алертов в WebSocket topic "alerts" (3.12.2).
// v2 добавит Telegram/email реализации того же интерфейса alerts.Notifier.
type wsNotifier struct {
	hub *ws.Hub
}

// Notify рассылает алерт всем подписчикам topic=alerts.
func (n wsNotifier) Notify(a alerts.Alert) {
	n.hub.Broadcast("alerts", "", map[string]any{
		"topic": "alerts", "id": a.ID, "rule": a.Rule, "severity": a.Severity,
		"state": a.State, "object_type": a.ObjectType, "object_id": a.ObjectID,
		"message": a.Message, "opened_at": a.OpenedAt, "resolved_at": a.ResolvedAt,
	})
}

func main() {
	// Структурированные JSON-логи в stdout (NFR-8).
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	// SQLite-хранилище: пользователи, настройки, audit, агрегаты метрик.
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		slog.Error("store open failed", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	// Docker Engine API через /var/run/docker.sock.
	// WithAPIVersionNegotiation обязателен — иначе риск несовместимости версий (разд. 10 ТЗ).
	docker, err := dockerapi.New()
	if err != nil {
		slog.Error("docker client init failed", "err", err)
		os.Exit(1)
	}
	defer docker.Close()

	// Кольцевой буфер live-метрик: 15 минут при шаге 3с (разд. 2.2 ТЗ).
	buf := metrics.NewClusterBuffer(15*time.Minute, 3*time.Second)

	// WebSocket-hub: рассылка метрик/логов/событий/алертов подписчикам.
	hub := ws.NewHub()

	// Стримы логов (FR-05): запуск по первой подписке, остановка с последней.
	logStreamer := streams.NewLogStreamer(docker, hub)
	hub.OnFirstSub = func(topic, service string, tail int) {
		if topic == "logs" && service != "" {
			logStreamer.Start(service, tail)
		}
	}
	hub.OnLastUnsub = func(topic, service string) {
		if topic == "logs" && service != "" {
			logStreamer.Stop(service)
		}
	}

	// Менеджер сессий и аутентификации (FR-07).
	sessions := auth.NewManager(db, cfg.SessionTTL)

	// Шифрование паролей реестров (FR-13); без ключа фича отключена.
	var box *crypto.Box
	if cfg.EncryptionKey != "" {
		box, err = crypto.NewBox(cfg.EncryptionKey)
		if err != nil {
			slog.Error("encryption key invalid", "err", err)
			os.Exit(1)
		}
	} else {
		slog.Warn("DSMS_ENCRYPTION_KEY not set: registry credentials disabled")
	}

	// Движок алертов (FR-12): доставка в WS topic "alerts".
	engine := alerts.NewEngine(db, wsNotifier{hub}, buf, docker)
	// Порог disk_high из настроек (Settings, экран 12).
	if v, _ := db.GetSetting("alert.disk_pct"); v != "" {
		if pct, err := strconv.ParseFloat(v, 64); err == nil {
			engine.SetDiskPct(pct)
		}
	}

	router := api.NewRouter(api.Deps{
		Cfg:      cfg,
		Store:    db,
		Docker:   docker,
		Buffer:   buf,
		Hub:      hub,
		Sessions: sessions,
		Crypto:   box,
		// Справочник агентов + клиент их API (volumes/df/prune, FR-10/FR-11).
		Agents:      agentclient.NewDirectory(),
		AgentClient: agentclient.New(cfg.AgentToken, ""),
		Alerts:      engine,
	})

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown по SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Периодические проверки алертов: агент молчит, сервис degraded.
	go engine.Run(ctx)

	// Минутные агрегаты метрик + ретенция 7 дн + VACUUM (разд. 2.2, 5 ТЗ).
	go history.New(db, buf).Run(ctx)

	// Лента Docker events → WS "events" (FR-06) + алерт task_failed (FR-12).
	go streams.RunEvents(ctx, docker, hub, func(m events.Message) {
		if m.Type == "container" && m.Action == "die" {
			if code := m.Actor.Attributes["exitCode"]; code != "" && code != "0" {
				name := m.Actor.Attributes["com.docker.swarm.task.name"]
				if name == "" {
					name = m.Actor.Attributes["name"]
				}
				engine.Event("task_failed", "warning", "task", name,
					"task "+name+" exited with code "+code)
			}
		}
	})

	go func() {
		slog.Info("panel listening", "addr", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server failed", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
