package tuner

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"gopkg.in/ini.v1"
)

const (
	minInnoDBRedoLogCap int = 96 * 1024 * 1024        // 96 MiB
	maxInnoDBRedoLogCap int = 16 * 1024 * 1024 * 1024 // 16 GiB
)

type innodbRedoLogCapTuner struct{}

func (t *innodbRedoLogCapTuner) name() string { return "innodb_redo_log_capacity" }

func init() {
	t := &innodbRedoLogCapTuner{}
	tuners[t] = struct{}{}
}

func (t *innodbRedoLogCapTuner) run(ctx context.Context, logger *slog.Logger, db *sql.DB, samples map[string]float64, cfg *ini.File) error {
	if samples == nil {
		return nil
	}

	var advisedValue int
	if v, ok := samples[advisedMetricName(t.name())]; !ok {
		return nil
	} else {
		advisedValue = int(v)
	}

	if advisedValue < minInnoDBRedoLogCap {
		advisedValue = minInnoDBRedoLogCap
	}

	if advisedValue > maxInnoDBRedoLogCap {
		advisedValue = maxInnoDBRedoLogCap
	}

	var version string
	if err := db.QueryRowContext(ctx, "SELECT @@version").Scan(&version); err != nil {
		return err
	}

	variableName := t.name()
	isDynamic := true
	if strings.Contains(strings.ToLower(version), "mariadb") || (len(version) > 0 && version[0] < '8') {
		variableName = "innodb_log_file_size"
		isDynamic = false

		var logFilesInGroupName string
		var logFilesInGroupValue int
		if err := db.QueryRowContext(ctx, "SHOW GLOBAL VARIABLES LIKE 'innodb_log_files_in_group'").Scan(&logFilesInGroupName, &logFilesInGroupValue); err != nil && err != sql.ErrNoRows {
			return err
		}
		if logFilesInGroupValue <= 0 {
			logFilesInGroupValue = 1
		}

		var fv float64
		if err := db.QueryRowContext(ctx, "SELECT ? / ?", advisedValue, logFilesInGroupValue).Scan(&fv); err != nil {
			return err
		} else {
			advisedValue = int(fv)
		}
	}

	var currentValue int
	if err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT @@%s", variableName)).Scan(&currentValue); err != nil {
		return err
	}

	if currentValue == advisedValue {
		logger.Debug("current value is equal to advised value, ignored", "tuner", t.name(), "current value", currentValue, "advised value", advisedValue)
		return nil
	}

	if isDynamic {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("SET GLOBAL %s = ?", variableName), advisedValue); err != nil {
			return err
		}
		logger.Info("runtime global variable tuned", "variable", t.name(), "old value", currentValue, "new value", advisedValue)
		lastTimeTuned = time.Now()
	}

	cfgValue := strconv.Itoa(advisedValue)
	if cfg.HasSection("mysqld") && cfg.Section("mysqld").Key(variableName) != nil && cfg.Section("mysqld").Key(variableName).String() == cfgValue {
		logger.Debug("advised value has already been set in tuning file", "tuner", variableName, "value", cfgValue)
		return nil
	}

	cfg.Section("mysqld").Key(variableName).SetValue(cfgValue)
	logger.Info("config global variable tuned", "variable", variableName, "old value", currentValue, "new value", advisedValue)
	lastTimeTuned = time.Now()

	return nil
}
