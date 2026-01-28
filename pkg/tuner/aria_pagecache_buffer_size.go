package tuner

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/shatteredsilicon/mt-agent/pkg/util"
	"gopkg.in/ini.v1"
)

var (
	minAriaPagecacheBufSize int = 1024 * 1024 // 1 MiB
)

type ariaPagecacheBufSizeTuner struct{}

func (t *ariaPagecacheBufSizeTuner) name() string { return "aria_pagecache_buffer_size" }

func init() {
	t := &ariaPagecacheBufSizeTuner{}
	tuners[t] = struct{}{}
}

func (t *ariaPagecacheBufSizeTuner) run(ctx context.Context, logger *slog.Logger, db *sql.DB, samples map[string]float64, cfg *ini.File) error {
	if samples == nil {
		return nil
	}

	var advisedValue int
	if v, ok := samples[advisedMetricName(t.name())]; !ok && len(samples) == 0 {
		return nil
	} else if ok {
		advisedValue = int(v)
	}

	if advisedValue < minAriaPagecacheBufSize {
		advisedValue = minAriaPagecacheBufSize
	}

	memoryTotal, err := util.GetMemoryTotal()
	if err != nil {
		return err
	}

	var maxAriaPagecacheBufSize float64
	if err := db.QueryRowContext(ctx, "SELECT ?*0.75 - @@innodb_buffer_pool_size", memoryTotal).Scan(&maxAriaPagecacheBufSize); err != nil {
		return err
	}

	if advisedValue > int(maxAriaPagecacheBufSize) {
		advisedValue = int(maxAriaPagecacheBufSize)
	}

	var currentValue int
	if err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT @@%s", t.name())).Scan(&currentValue); err != nil {
		return err
	}

	if currentValue == advisedValue {
		logger.Debug("current value is equal to advised value, ignored", "tuner", t.name(), "current value", currentValue, "advised value", advisedValue)
		return nil
	}

	cfgValue := strconv.Itoa(advisedValue)
	if cfg.HasSection("mysqld") && cfg.Section("mysqld").Key(t.name()) != nil && cfg.Section("mysqld").Key(t.name()).String() == cfgValue {
		logger.Debug("advised value has already been set in tuning file", "tuner", t.name(), "value", cfgValue)
		return nil
	}

	cfg.Section("mysqld").Key(t.name()).SetValue(cfgValue)
	logger.Info("config global variable tuned", "variable", t.name(), "old value", currentValue, "new value", advisedValue)
	lastTimeTuned = time.Now()

	return nil
}
