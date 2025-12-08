# 技术栈与开源库选型（Go）

本文档说明项目使用的主要技术栈与 Go 开源库选型，以高 star、成熟稳定为优先。

## 一、核心语言与运行环境

- **语言**：Go 1.24.5（具体版本以生产环境为准）。
- **运行环境**：Linux（推荐使用容器化运行于 Kubernetes）。
- **协议**：WebSocket / gRPC / HTTP。

## 二、网络与 RPC 层（以长连接为主）

整体不以传统 HTTP/Web 框架为核心，而是围绕长连接和有状态服务设计，HTTP 仅作为少量控制面/回调的辅助协议。

### 1. gRPC（含双向流）

- `google.golang.org/grpc`
  - 作用：网关与内部服务之间的 RPC 通信；对外 SDK 可选使用 gRPC。
  - 优点：标准化接口、自动代码生成、内建流式支持，适合实现有状态的长连接服务（如双向流维护玩家会话）。

- `github.com/grpc-ecosystem/grpc-gateway/v2`
  - 作用：将 gRPC 接口映射为 REST/JSON HTTP 接口，方便管理后台或三方系统调用。

### 2. WebSocket（主要玩家通道）

- `nhooyr.io/websocket` 或 `github.com/gorilla/websocket`
  - 作用：实现游戏客户端长连接通道。
  - 选择策略：
    - `gorilla/websocket`：生态成熟，示例丰富；
    - `nhooyr/websocket`：现代 API，轻量，支持 context 更好。

### 3. HTTP 支撑能力（仅控制面）

- 标准库 `net/http`
  - 作用：仅用于抖音回调 Webhook、健康检查、少量运维控制接口。
  - 说明：业务核心链路（玩家交互、游戏对局）全部基于 WebSocket/gRPC 长连接和有状态服务，不依赖通用 HTTP/Web 框架。

## 三、存储层

### 1. PostgreSQL

- 驱动：`github.com/jackc/pgx/v5`
  - 作用：连接 PostgreSQL，执行 SQL，支持连接池、批量操作。

- Query Builder：
  - `github.com/Masterminds/squirrel`
    - 优点：类型更明确，适合偏 SQL 风格的开发方式，结合 `pgx` 使用可兼顾性能与可维护性。

### 2. Redis

- `github.com/redis/go-redis/v9`
  - 作用：缓存、匹配队列（List/ZSet）、房间状态部分数据、排行榜等。
  - 支持连接池、集群、哨兵。

## 四、配置、日志与观测性

### 1. 配置管理

- `github.com/spf13/viper`
  - 作用：加载配置文件（YAML/JSON/TOML 等），支持环境变量覆盖和多环境配置。

### 2. 日志

- `go.uber.org/zap`
  - 作用：提供高性能结构化日志。
  - 使用方式：封装为统一的 `logger`，在各服务中注入使用。

- `github.com/natefinch/lumberjack`
  - 作用：为 zap 提供日志文件滚动（按大小/日期拆分）、保留历史文件数量控制。
  - 使用方式：将 `lumberjack.Logger` 作为 zap 的 `WriteSyncer`，实现本地文件日志的自动切割与清理。

### 3. 指标与监控

- `github.com/prometheus/client_golang`
  - 作用：采集和暴露 Prometheus 指标（QPS、延迟、错误率、连接数、房间数等）。

### 4. 链路追踪

- `go.opentelemetry.io/otel`
  - 作用：OpenTelemetry 标准实现，支持多种后端（Jaeger、Tempo 等）。
  - 用途：跟踪跨服务调用链，定位性能瓶颈和错误来源。

## 五、并发控制与容错

### 1. 限流

- `golang.org/x/time/rate`
  - 作用：在网关层、接口层实现请求级限流，保护后端服务。

### 2. 熔断

- `github.com/sony/gobreaker`
  - 作用：在调用外部服务或关键内部服务时增加熔断保护，避免级联故障。

### 3. 协程池（可选）

- `github.com/panjf2000/ants`
  - 作用：在需要大量并发任务且需控制 goroutine 数量的场景使用（例如异步日志处理、批量任务）。

## 六、工具与通用库

### 1. 依赖注入（可选）

- `go.uber.org/fx` 或 `github.com/google/wire`
  - 作用：管理服务之间依赖，避免复杂手动初始化。
  - 可按团队习惯选择是否引入。

### 2. 校验

- `github.com/go-playground/validator/v10`
  - 作用：对请求数据结构进行字段校验（必填、范围、格式等）。

### 3. 序列化

- `github.com/bytedance/sonic`
  - 作用：作为统一的 JSON 编解码库使用，在网关编解码、服务间 JSON 交互等场景中提供高性能序列化能力。

### 4. 消息队列

- `github.com/segmentio/kafka-go`
  - 作用：作为主选消息队列客户端，用于游戏事件广播、异步任务处理、日志/埋点 pipeline 等，需要高吞吐和可持久化的场景。

## 七、抖音 Server SDK

- 使用官方抖音开放平台 Go Server SDK（按官网文档引入）。
  - 作用：封装签名验证、请求加解密、回调处理等。
  - 建议在 `internal/douyin` 中进行二次封装，统一对内暴露接口。

## 八、基础设施与环境依赖

- Kubernetes（或等价容器编排）：
  - 用于部署各服务实例，实现弹性扩缩容和服务发现。
- 反向代理 / 负载均衡：
  - 使用云厂商 SLB/ELB 或 Nginx/Envoy 等，将流量分发到网关节点。
- CI/CD 工具：
  - GitHub Actions、GitLab CI、Jenkins 等，完成自动构建和部署。

以上技术栈在业界较为主流，社区活跃、资料丰富，适合作为从 0 到 1 搭建分布式游戏服务器的基础。
