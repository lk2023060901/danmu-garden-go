package application

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	zlog "github.com/lk2023060901/danmu-garden-go/pkg/log"
	zviper "github.com/lk2023060901/danmu-garden-go/pkg/util/viper"
)

// Application 是 Zeus 服务的运行时容器。
// 负责持有配置，并管理通用依赖。
type Application struct {
	cfg           *zviper.Config
	loggers       map[string]*zlog.MLogger
	signalHandler func(kind SignalKind, sig os.Signal)
}

// SignalKind 表示框架抽象出的信号语义。
//
// 用于将底层 os.Signal 映射为更易于业务理解的类别。
type SignalKind int

const (
	SignalUnknown  SignalKind = iota
	SignalShutdown            // 优雅停服（SIGINT/SIGTERM）
	SignalReload              // 配置/日志重载（SIGHUP）
	SignalDiagnose            // 诊断性操作（SIGQUIT）
	SignalUser1               // 预留给用户自定义控制（SIGUSR1）
	SignalUser2               // 预留给用户自定义控制（SIGUSR2）
)

// New 创建一个新的 Application 实例。
func New() *Application {
	return &Application{}
}

// Run 是 Zeus 应用的入口。
// 负责解析命令行参数（os.Args）并加载配置文件，优先级如下：
//   1. 默认：./config.yaml
//   2. 环境变量：ZEUS_CONFIG_FILE_PATH
//   3. 命令行参数：--config <path> 或 --config=<path>
func (a *Application) Run() error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	a.cfg = cfg

	if err := a.initLogging(); err != nil {
		return err
	}

	// 阻塞等待退出信号（例如 Ctrl+C），便于统一处理进程生命周期。
	// 默认行为：直到收到一个“停服类”信号（SIGINT/SIGTERM）才返回；
	// 其余信号会通过应用级信号处理回调（如已注册）交由调用方决定具体行为。
	a.WaitForShutdownSignal()

	return nil
}

// OnSignal 注册应用级信号处理回调。
//
// 回调在 Application 内部捕获到信号后被调用，调用顺序：
//   1. WaitForSignal 将 OS 信号映射为 SignalKind；
//   2. 若已注册 signalHandler，则先调用 handler(kind, sig)；
//   3. 若 kind == SignalShutdown，则 WaitForShutdownSignal 返回，Run 随之返回。
//
// 注意：
//   - 回调在调用 OnSignal 的 goroutine 中执行，通常为 main goroutine；
//   - 回调内的行为由框架使用者自行决定，例如：重载配置、输出诊断信息等。
func (a *Application) OnSignal(handler func(kind SignalKind, sig os.Signal)) {
	a.signalHandler = handler
}

// WaitForShutdownSignal 阻塞直到收到一个“停服类”信号（SIGINT 或 SIGTERM）。
//
// 对于其它信号（SIGHUP/SIGQUIT/SIGUSR1/SIGUSR2），调用方可通过 WaitForSignal
// 获取更细粒度的控制，并决定是否重载配置、做诊断或执行自定义逻辑。
func (a *Application) WaitForShutdownSignal() {
	for {
		_, kind := a.WaitForSignal()
		if a.signalHandler != nil {
			// 交由应用使用者决定不同信号的具体行为。
			a.signalHandler(kind, nil)
		}
		if kind == SignalShutdown {
			return
		}
	}
}

// WaitForSignal 阻塞等待进程级控制信号，并返回其语义类别。
//
// 当前监听的信号包括：
//   - SIGINT  ：映射为 SignalShutdown（通常由 Ctrl+C 触发）；
//   - SIGTERM ：映射为 SignalShutdown（容器/进程管理器正常停服信号）；
//   - SIGHUP  ：映射为 SignalReload（配置/日志重载）；
//   - SIGQUIT ：映射为 SignalDiagnose（诊断性操作，如输出堆栈）；
//   - SIGUSR1 ：映射为 SignalUser1（用户自定义控制）；
//   - SIGUSR2 ：映射为 SignalUser2（用户自定义控制）。
//
// 调用方可根据返回的 kind 决定具体行为。
func (a *Application) WaitForSignal() (os.Signal, SignalKind) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGHUP,
		syscall.SIGQUIT,
		syscall.SIGUSR1,
		syscall.SIGUSR2,
	)

	sig := <-sigCh

	switch sig {
	case syscall.SIGINT, syscall.SIGTERM:
		return sig, SignalShutdown
	case syscall.SIGHUP:
		return sig, SignalReload
	case syscall.SIGQUIT:
		return sig, SignalDiagnose
	case syscall.SIGUSR1:
		return sig, SignalUser1
	case syscall.SIGUSR2:
		return sig, SignalUser2
	default:
		return sig, SignalUnknown
	}
}

// Config 返回已加载的配置。
func (a *Application) Config() *zviper.Config {
	return a.cfg
}

// Logger 根据名称返回配置中创建的日志实例。
// 如果名称未知，则回退到全局日志。
func (a *Application) Logger(name string) *zlog.MLogger {
	if a.loggers == nil {
		return &zlog.MLogger{Logger: zlog.L()}
	}
	if lg, ok := a.loggers[name]; ok && lg != nil {
		return lg
	}
	return &zlog.MLogger{Logger: zlog.L()}
}

// loadConfig 解析配置文件路径，并通过 viper 封装进行加载。
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

// initLogging 初始化全局日志和模块级日志。
func (a *Application) initLogging() error {
	if err := a.initGlobalLoggerFromEnv(); err != nil {
		return err
	}
	if err := a.initModuleLoggersFromConfig(); err != nil {
		return err
	}
	return nil
}

// initGlobalLoggerFromEnv 基于 ZEUS_LOG_* 环境变量配置进程级全局日志。
//
// 优先级和含义：
//   - ZEUS_LOG_ENABLE: "1"/"true" 时开启输出，其他视为关闭。
//   - ZEUS_LOG_LEVEL: 日志级别（默认 "info"）。
//   - ZEUS_LOG_STDOUT: 是否输出到 stdout（默认 false）。
//   - ZEUS_LOG_FILE_DIR: 日志文件目录。
//   - ZEUS_LOG_FILE: 日志文件名（为空表示不写文件）。
//   - ZEUS_LOG_FORMAT: 日志格式（"text" 或 "json"，默认 "text"）。
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

	// 未开启时，关闭所有输出（既不写文件也不打印到控制台）。
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

// initModuleLoggersFromConfig 从 YAML 配置中 "logging" 段创建具名日志实例。
//
// 配置示例：
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

	// 将 "logging" 段反序列化为 map[name]Config。
	raw := make(map[string]zlog.Config)
	if err := a.cfg.UnmarshalKey("logging", &raw); err != nil {
		// 如果 key 不存在，UnmarshalKey 通常会保持 raw 为空且不返回错误。
		// 只有真实错误才需要返回。
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
