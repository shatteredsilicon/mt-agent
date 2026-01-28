package tuner

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"gopkg.in/ini.v1"
)

const (
	minTableDefCache int = 400
	maxTableDefCache int = 256 * 1024 // 256 KiB
)

type tableDefCacheTuner struct{}

func (t *tableDefCacheTuner) name() string { return "table_definition_cache" }

func init() {
	t := &tableDefCacheTuner{}
	tuners[t] = struct{}{}
}

func (t *tableDefCacheTuner) run(ctx context.Context, logger *slog.Logger, db *sql.DB, samples map[string]float64, cfg *ini.File) error {
	if samples == nil {
		return nil
	}

	var advisedValue int
	if v, ok := samples[advisedMetricName(t.name())]; !ok {
		return nil
	} else {
		advisedValue = int(v)
	}

	if advisedValue < minTableDefCache {
		advisedValue = minTableDefCache
	}

	if advisedValue > maxTableDefCache {
		advisedValue = maxTableDefCache
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
