// Package main — точка входа AGENT-компонента DSMS.
//
// Агент работает на каждой ноде (mode: global), собирает метрики хоста
// через gopsutil (интервал 3с) и шлёт JSON-батч на panel (/api/v1/ingest).
// При недоступности panel буферизует до 60 сек, потом отбрасывает старые точки.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xakus/DSMS/agent/internal/collector"
	"github.com/xakus/DSMS/agent/internal/config"
	"github.com/xakus/DSMS/agent/internal/sender"
)

func main() {
	// Структурированные JSON-логи в stdout (NFR-8).
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	col := collector.New(cfg)
	snd := sender.New(cfg)
	go snd.Run(ctx) // отправка с буферизацией — в своей горутине

	slog.Info("agent started",
		"node_id", cfg.NodeID, "panel", cfg.PanelURL, "interval", cfg.Interval.String())

	// Основной цикл сбора: тик каждые cfg.Interval (по умолчанию 3с).
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("agent stopped")
			return
		case <-ticker.C:
			snap, err := col.Collect(ctx)
			if err != nil {
				slog.Warn("collect failed", "err", err)
				continue
			}
			snd.Enqueue(snap)
		}
	}
}
