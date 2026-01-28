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

const (
	minKeyBufferSize int = 1024 * 1024 // 1 MiB
)

type keyBufferSizeTuner struct{}

func (t *keyBufferSizeTuner) name() string { return "key_buffer_size" }

func init() {
	t := &keyBufferSizeTuner{}
	tuners[t] = struct{}{}
}

func (t *keyBufferSizeTuner) run(ctx context.Context, logger *slog.Logger, db *sql.DB, samples map[string]float64, cfg *ini.File) error {
	if samples == nil {
		return nil
	}

	var advisedValue int
	if v, ok := samples[advisedMetricName(t.name())]; !ok && len(samples) == 0 {
		return nil
	} else if ok {
		advisedValue = int(v)
	}

	if advisedValue < minKeyBufferSize {
		advisedValue = minKeyBufferSize
	}

	memoryTotal, err := util.GetMemoryTotal()
	if err != nil {
		return err
	}

	var maxKeyBufferSize float64
	if err := db.QueryRowContext(ctx, "SELECT ?*0.75 - @@innodb_buffer_pool_size - @@aria_pagecache_buffer_size", memoryTotal).Scan(&maxKeyBufferSize); err != nil {
		return err
	}

	if advisedValue > int(maxKeyBufferSize) {
		advisedValue = int(maxKeyBufferSize)
	}

	var currentValue int
	if err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT @@%s", t.name())).Scan(&currentValue); err != nil {
		return err
	}

	if currentValue == advisedValue {
		logger.Debug("current value is equal to advised value, ignored", "tuner", t.name(), "current value", currentValue, "advised value", advisedValue)
		return nil
	}

	if _, err := db.ExecContext(ctx, fmt.Sprintf("SET GLOBAL %s = ?", t.name()), advisedValue); err != nil {
		return err
	}
	logger.Info("runtime global variable tuned", "variable", t.name(), "old value", currentValue, "new value", advisedValue)
	lastTimeTuned = time.Now()

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
