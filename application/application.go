package application

import (
	"fmt"
	"os"
	"strings"

	zlog "github.com/lk2023060901/danmu-garden-go/pkg/log"
	zviper "github.com/lk2023060901/danmu-garden-go/pkg/util/viper"
)

// Application is the main runtime container for a Zeus service.
// It owns configuration and manages common dependencies.
type Application struct {
	cfg     *zviper.Config
	loggers map[string]*zlog.MLogger
}

// New creates a new Application instance.
func New() *Application {
	return &Application{}
}

// Run is the entry of Zeus application.
// It parses command-line arguments (os.Args) and loads configuration file
// using the following priority:
//   1. Default: ./config.yaml
//   2. Env: ZEUS_CONFIG_FILE_PATH
//   3. CLI: --config <path> or --config=<path>
func (a *Application) Run() error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	a.cfg = cfg

	if err := a.initLogging(); err != nil {
		return err
	}

	return nil
}

// Config returns the loaded configuration, if any.
func (a *Application) Config() *zviper.Config {
	return a.cfg
}

// Logger returns a named logger created from configuration.
// If the name is unknown, it falls back to the global logger.
func (a *Application) Logger(name string) *zlog.MLogger {
	if a.loggers == nil {
		return &zlog.MLogger{Logger: zlog.L()}
	}
	if lg, ok := a.loggers[name]; ok && lg != nil {
		return lg
	}
	return &zlog.MLogger{Logger: zlog.L()}
}

// loadConfig resolves config file path and loads it via viper wrapper.
func (a *Application) loadConfig() (*zviper.Config, error) {
	configPath := "./config.yaml"

	if envPath := os.Getenv("ZEUS_CONFIG_FILE_PATH"); envPath != "" {
		configPath = envPath
	}

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--config" {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("missing value after --config")
			}
			configPath = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(arg, "--config=") {
			val := strings.TrimPrefix(arg, "--config=")
			if val != "" {
				configPath = val
			}
			continue
		}
	}

	cfg := zviper.New()
	if err := cfg.LoadFile(configPath); err != nil {
		return nil, fmt.Errorf("failed to load config file %q: %w", configPath, err)
	}

	return cfg, nil
}

// initLogging initializes global and module-level loggers.
func (a *Application) initLogging() error {
	if err := a.initGlobalLoggerFromEnv(); err != nil {
		return err
	}
	if err := a.initModuleLoggersFromConfig(); err != nil {
		return err
	}
	return nil
}

// initGlobalLoggerFromEnv configures the process-wide logger based on ZEUS_LOG_* env vars.
//
// Priority:
//   - ZEUS_LOG_ENABLE: "1"/"true" to enable outputs; others treated as disabled.
//   - ZEUS_LOG_LEVEL: log level (default "info").
//   - ZEUS_LOG_STDOUT: whether to log to stdout (default false).
//   - ZEUS_LOG_FILE_DIR: log directory.
//   - ZEUS_LOG_FILE: log file name (empty means no file).
//   - ZEUS_LOG_FORMAT: log format ("text" or "json", default "text").
func (a *Application) initGlobalLoggerFromEnv() error {
	enabled := getenvBool("ZEUS_LOG_ENABLE", false)

	cfg := &zlog.Config{
		Level:              getenvDefault("ZEUS_LOG_LEVEL", "info"),
		GrpcLevel:          "",
		Format:             getenvDefault("ZEUS_LOG_FORMAT", "text"),
		DisableTimestamp:   false,
		Stdout:             getenvBool("ZEUS_LOG_STDOUT", false),
		DisableCaller:      false,
		DisableStacktrace:  false,
		DisableErrorVerbose: true,
		File: zlog.FileLogConfig{
			RootPath: getenvDefault("ZEUS_LOG_FILE_DIR", ""),
			Filename: getenvDefault("ZEUS_LOG_FILE", ""),
		},
	}

	// When not enabled, direct all outputs to a discarded sink.
	if !enabled {
		cfg.Stdout = false
		cfg.File.Filename = ""
	}

	logger, props, err := zlog.InitLogger(cfg)
	if err != nil {
		return fmt.Errorf("init global logger from env: %w", err)
	}
	zlog.ReplaceGlobals(logger, props)
	return nil
}

// initModuleLoggersFromConfig creates named loggers from YAML config under "logging" key.
//
// Example:
//   logging:
//     game:
//       level: debug
//       stdout: true
//       file:
//         rootpath: ./logs
//         filename: game.log
func (a *Application) initModuleLoggersFromConfig() error {
	if a.cfg == nil {
		return nil
	}

	// Unmarshal "logging" section into a map[name]Config.
	raw := make(map[string]zlog.Config)
	if err := a.cfg.UnmarshalKey("logging", &raw); err != nil {
		// If the key doesn't exist, UnmarshalKey typically leaves raw empty without error.
		// Any real error should be returned.
		return err
	}
	if len(raw) == 0 {
		return nil
	}

	a.loggers = make(map[string]*zlog.MLogger, len(raw))
	for name, lc := range raw {
		cfgCopy := lc
		logger, _, err := zlog.InitLogger(&cfgCopy)
		if err != nil {
			return fmt.Errorf("init module logger %q: %w", name, err)
		}
		a.loggers[name] = &zlog.MLogger{Logger: logger}
	}

	return nil
}

func getenvDefault(key, def string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	return val
}

func getenvBool(key string, def bool) bool {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	switch strings.ToLower(val) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}
