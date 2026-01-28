package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/shatteredsilicon/mt-agent/pkg/logger"
	"github.com/shatteredsilicon/mt-agent/pkg/tuner"
	"gopkg.in/ini.v1"
)

var DefaultTuningFile string

var (
	logLevel  logger.AllowedLevel
	logFormat logger.AllowedFormat
)

var (
	cfg          = new(config)
	setByUserMap = make(map[string]bool)
)

func setByUserFlagAction() func(ctx *kingpin.ParseContext) error {
	executed := false

	return func(pc *kingpin.ParseContext) error {
		if executed {
			return nil
		}

		for _, elem := range pc.Elements {
			if elem.Clause == nil {
				continue
			}

			flagClause, ok := elem.Clause.(*kingpin.FlagClause)
			if !ok || flagClause == nil {
				continue
			}

			setByUserMap[flagClause.Model().Name] = true
		}

		executed = true
		return nil
	}
}

var (
	configPath = kingpin.Flag(
		"config",
		"Path of config file",
	).Default("/opt/ss/ssm-client/mt-agent.conf").String()

	tuneInterval = kingpin.Flag(
		"tune.interval",
		"Tuning interval",
	).Duration()
	tuneChangeGap = kingpin.Flag(
		"tune.change_gap",
		"gap limit between config changes",
	).Duration()

	mysqlDSN = kingpin.Flag(
		"mysql.dsn",
		"DSN to use for connecting to MySQL",
	).String()
	mysqlTuningfile = kingpin.Flag(
		"mysql.tuning_file",
		"Path of MySQL config file for tuned settings",
	).Default(DefaultTuningFile).String()

	serverURL = kingpin.Flag(
		"server.url",
		"SSM server url",
	).String()
	serverUsername = kingpin.Flag(
		"server.username",
		"SSM server username",
	).String()
	serverPassword = kingpin.Flag(
		"server.password",
		"SSM server password",
	).String()
)

func init() {
	kingpin.Flag(
		"log.level",
		"Log level for logging",
	).HintOptions(logger.AllowedLevels...).SetValue(&logLevel)
	kingpin.Flag(
		"log.format",
		"Log format for logging",
	).HintOptions(logger.AllowedFormats...).SetValue(&logFormat)

	kingpin.HelpFlag.Short('h')
	kingpin.CommandLine.PreAction(setByUserFlagAction())
	kingpin.Parse()
}

func main() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	reconnectSigChan := make(chan os.Signal, 1)
	signal.Notify(reconnectSigChan, syscall.SIGHUP) // kill -HUP PID

	intSigChan := make(chan os.Signal, 1)
	signal.Notify(intSigChan, syscall.SIGINT) // CTRL-C

	if err := ini.MapTo(cfg, *configPath); err != nil {
		log.Fatalf("failed to load config file %s: %s", *configPath, err.Error())
	}

	// override flag value with config value
	// if it's not set
	overrideFlags()

	sURL, err := url.Parse(*serverURL)
	if err != nil {
		log.Fatalf("failed to parse server url '%s': %s", *serverURL, err.Error())
	}
	if sURL.User == nil && (len(*serverUsername) > 0 || len(*serverPassword) > 0) {
		sURL.User = url.UserPassword(*serverUsername, *serverPassword)
	}

	logger := logger.NewLogger(&logLevel, &logFormat)
	tuner := tuner.NewTuner(*tuneInterval, *tuneChangeGap, logger, sURL, *mysqlDSN, *mysqlTuningfile)

	go func() {
		defer func() {

		}()

		tuner.Start(context.Background())
	}()

LOOP:
	for {
		select {
		case <-sigChan:
			break LOOP
		case <-intSigChan:
			break LOOP
		case <-reconnectSigChan:
		}
	}
}

type config struct {
	Tune   tuneConfig   `ini:"tune"`
	Log    logConfig    `ini:"log"`
	MySQL  mysqlConfig  `ini:"mysql"`
	Server serverConfig `ini:"server"`
}

type tuneConfig struct {
	Interval  time.Duration `ini:"interval"`
	ChangeGap time.Duration `ini:"change_gap"`
}

type mysqlConfig struct {
	DSN        string  `ini:"dsn"`
	TuningFile *string `ini:"tuning_file"`
}

type logConfig struct {
	Level  string `ini:"level"`
	Format string `ini:"format"`
}

type serverConfig struct {
	URL      string `ini:"url"`
	Username string `ini:"username"`
	Password string `ini:"password"`
}

func configVisit(visitFn func(string, string, reflect.Value)) {
	type item struct {
		value   reflect.Value
		section string
	}

	items := []item{
		{
			value:   reflect.ValueOf(cfg).Elem(),
			section: "",
		},
	}
	for i := 0; i < len(items); i++ {
		for j := 0; j < items[i].value.Type().NumField(); j++ {
			fieldValue := items[i].value.Field(j)
			fieldType := items[i].value.Type().Field(j)
			section := items[i].section
			key := strings.SplitN(fieldType.Tag.Get("ini"), ",", 2)[0]

			if fieldValue.Kind() == reflect.Struct {
				if fieldValue.CanAddr() {
					if section == "" {
						section = key
					} else if section != key {
						section = fmt.Sprintf("%s.%s", section, key)
					}

					items = append(items, item{
						value:   fieldValue.Addr().Elem(),
						section: section,
					})
				}
				continue
			} else if fieldValue.Kind() == reflect.Ptr && fieldValue.Type().Elem().Kind() == reflect.String && fieldValue.IsNil() {
				continue
			}

			visitFn(section, key, fieldValue)
		}
	}
}

func overrideFlags() {
	configVisit(func(section, key string, fieldValue reflect.Value) {
		flagKey := fmt.Sprintf("%s.%s", section, key)
		if section == "" {
			flagKey = key
		}

		setByUser := setByUserMap[flagKey]
		kingpinF := kingpin.CommandLine.GetFlag(flagKey)
		if setByUser || kingpinF == nil {
			return
		}

		var values []reflect.Value
		if fieldValue.Kind() == reflect.Slice {
			for i := 0; i < fieldValue.Len(); i++ {
				values = append(values, fieldValue.Index(i))
			}
		} else {
			values = []reflect.Value{fieldValue}
		}

		for i := range values {
			switch values[i].Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Float32, reflect.Int64:
				kingpinF.Model().Value.Set(strconv.FormatInt(values[i].Int(), 10))
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				kingpinF.Model().Value.Set(strconv.FormatUint(values[i].Uint(), 10))
			case reflect.Bool:
				kingpinF.Model().Value.Set(strconv.FormatBool(values[i].Bool()))
			case reflect.Ptr:
				if !values[i].IsNil() {
					if values[i].Elem().Kind() == reflect.Bool {
						kingpinF.Model().Value.Set(strconv.FormatBool(values[i].Elem().Bool()))
					} else {
						kingpinF.Model().Value.Set(values[i].Elem().String())
					}
				}
			default:
				kingpinF.Model().Value.Set(values[i].String())
			}
		}
	})
}
