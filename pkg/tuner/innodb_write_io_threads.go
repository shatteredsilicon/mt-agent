package tuner

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"runtime"
	"strconv"
	"time"

	"gopkg.in/ini.v1"
)

var (
	minInnoDBWriteIOThreads int = 4
	maxInnoDBWriteIOThreads int = int(math.Max(float64(minInnoDBWriteIOThreads), math.Min(64, float64(runtime.NumCPU()/2))))
)

type innodbWriteIOThreadsTuner struct{}

func (t *innodbWriteIOThreadsTuner) name() string { return "innodb_write_io_threads" }

func init() {
	t := &innodbWriteIOThreadsTuner{}
	tuners[t] = struct{}{}
}

func (t *innodbWriteIOThreadsTuner) run(ctx context.Context, logger *slog.Logger, db *sql.DB, samples map[string]float64, cfg *ini.File) error {
	if samples == nil {
		return nil
	}

	var advisedValue int
	if v, ok := samples[advisedMetricName(t.name())]; !ok {
		return nil
	} else {
		advisedValue = int(v)
	}

	if advisedValue < minInnoDBWriteIOThreads {
		advisedValue = minInnoDBWriteIOThreads
	}

	if advisedValue > maxInnoDBWriteIOThreads {
		advisedValue = maxInnoDBWriteIOThreads
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
