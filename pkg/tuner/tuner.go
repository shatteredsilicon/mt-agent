package tuner

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"gopkg.in/ini.v1"
)

const (
	httpClientTimeout = 10 * time.Second
)

const (
	defaultTuneInterval   = time.Minute
	defaultChangeLimitGap = time.Hour
)

var lastTimeTuned time.Time // as long as we don't do something like unmarshal with it, it's goroutine-safe.

type tuner interface {
	name() string
	run(context.Context, *slog.Logger, *sql.DB, map[string]float64, *ini.File) error
}

var tuners = map[tuner]struct{}{}

type Tuner struct {
	interval   time.Duration
	gap        time.Duration
	server     *url.URL
	httpClient *http.Client
	logger     *slog.Logger
	dsn        string
	tuningFile string
}

func NewTuner(
	interval time.Duration,
	gap time.Duration,
	logger *slog.Logger,
	serverURL *url.URL,
	mysqlDSN string,
	tuningFile string,
) *Tuner {
	if interval < defaultTuneInterval {
		interval = defaultTuneInterval
	}
	if gap < defaultChangeLimitGap {
		gap = defaultChangeLimitGap
	}

	return &Tuner{
		interval: interval,
		gap:      gap,
		server:   serverURL,
		httpClient: &http.Client{
			Timeout: httpClientTimeout,
		},
		logger:     logger,
		dsn:        mysqlDSN,
		tuningFile: tuningFile,
	}
}

func (t *Tuner) Start(ctx context.Context) {
	ticker := time.NewTicker(t.interval)

	for range ticker.C {
		if time.Since(lastTimeTuned) < t.gap {
			t.logger.Debug(fmt.Sprintf("skipped tuning because changes has been updated in the last %s", t.gap.String()))
			continue
		}

		fileInfo, err := os.Stat(t.tuningFile)
		if err != nil && !os.IsNotExist(err) {
			t.logger.Error("failed to check stat of tuning file", "error", err.Error())
			continue
		} else if err == nil && time.Since(fileInfo.ModTime()) < t.gap {
			t.logger.Debug(fmt.Sprintf("skipped tuning because the tuning file has been updated in the last %s", t.gap.String()))
			continue
		}

		samples, err := t.fetchAdvisedMetricSamples(ctx)
		if err != nil {
			t.logger.Error("failed to fetch advised metric samples", "error", err.Error())
			continue
		}
		t.logger.Debug("got advised metric samples from server", "samples", samples)

		iniCfg, err := ini.LooseLoad(t.tuningFile)
		if err != nil {
			t.logger.Error("failed to open tuning file", "path", t.tuningFile, "error", err.Error())
			continue
		}

		db, err := t.openDB()
		if err != nil {
			t.logger.Error("failed to open mysql connection", "dsn", t.dsn, "error", err.Error())
			continue
		}

		prevLastTimeTuned := lastTimeTuned
		wg := &sync.WaitGroup{}
		for tr := range tuners {
			wg.Add(1)
			go func() {
				defer func() {
					wg.Done()
				}()

				if err := tr.run(ctx, t.logger, db, samples, iniCfg); err != nil {
					t.logger.ErrorContext(ctx, "failed to tune", "tuner", tr.name(), "error", err.Error())
					return
				}
			}()
		}
		wg.Wait()
		db.Close()

		if prevLastTimeTuned.Equal(lastTimeTuned) {
			continue
		}

		if err := iniCfg.SaveTo(t.tuningFile); err != nil {
			t.logger.Error("failed to save tuning file", "path", t.tuningFile, "error", err.Error())
		} else {
			t.logger.Info("tuning file saved", "path", t.tuningFile)
		}
	}
}

func (t *Tuner) openDB() (*sql.DB, error) {
	db, err := sql.Open("mysql", t.dsn)
	if err != nil {
		return nil, err
	}

	conns := len(tuners)
	if conns == 0 {
		conns = 1
	}
	if conns > 20 {
		conns = 20
	}

	db.SetConnMaxLifetime(time.Minute * 3)
	db.SetMaxOpenConns(conns)
	db.SetMaxIdleConns(conns)
	return db, nil
}

type advisedMetricSample struct {
	Metric string  `json:"metric"`
	Value  float64 `json:"value"`
}

type advisedMetricSamples struct {
	Samples []advisedMetricSample `json:"samples"`
}

func (t *Tuner) fetchAdvisedMetricSamples(ctx context.Context) (map[string]float64, error) {
	url := *t.server
	url.Path = path.Join(url.Path, "v0", "metric", "advised-metric-samples")
	req, err := http.NewRequestWithContext(ctx, "GET", url.String(), nil)
	if err != nil {
		return nil, err
	}

	t.logger.Debug("fetching advised metric samples from server", "url", url.String())
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var metricSamples advisedMetricSamples
	if err := json.NewDecoder(resp.Body).Decode(&metricSamples); err != nil {
		return nil, err
	}

	samples := make(map[string]float64)
	for _, sample := range metricSamples.Samples {
		samples[strings.SplitN(sample.Metric, "{", 2)[0]] = sample.Value
	}

	return samples, nil
}

func advisedMetricName(tunerName string) string {
	return "ssm_advised_" + tunerName
}
