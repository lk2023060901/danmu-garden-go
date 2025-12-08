# 项目目录结构设计说明

本文档说明服务器项目的目录结构与各目录职责，方便团队协作和后续扩展。

## 一、顶层目录结构

推荐采用单仓多服务的结构：

```text
.
├── cmd/
│   ├── gateway/
│   │   └── main.go
│   ├── game/
│   │   └── main.go
│   ├── match/
│   │   └── main.go
│   ├── user/
│   │   └── main.go
│   └── tools/
│       └── main.go
├── api/
│   ├── proto/
│   └── http/
├── internal/
│   ├── gateway/
│   ├── game/
│   ├── match/
│   ├── user/
│   ├── douyin/
│   ├── storage/
│   ├── infra/
│   └── shared/
├── pkg/
├── configs/
├── deploy/
│   ├── k8s/
│   ├── docker/
│   └── scripts/
├── docs/
├── scripts/
├── Makefile
└── go.mod
```

## 二、cmd/ 服务入口

`cmd/` 下每个子目录对应一个单独服务的启动入口，仅负责组装依赖并启动，不写业务逻辑。

- `cmd/gateway/`
  - `main.go`：网关服务入口，初始化配置、日志、WebSocket/gRPC/HTTP 服务器。
- `cmd/game/`
  - `main.go`：游戏逻辑服务入口，启动游戏房间管理、状态同步相关服务。
- `cmd/match/`
  - `main.go`：匹配与房间分配服务入口，管理匹配队列与房间分配。
- `cmd/user/`
  - `main.go`：用户服务入口，提供用户、资产等接口。
- `cmd/tools/`
  - `main.go`：工具型程序（数据修复、统计等），通常是一次性或定时任务。

## 三、api/ 接口定义

- `api/proto/`
  - 存放 gRPC 的 `.proto` 文件。
  - 定义网关与内部服务之间的 RPC 接口，以及对外 SDK 使用的接口。
- `api/http/`
  - 存放 HTTP 接口定义（OpenAPI/Swagger 或路由声明）。
  - 用于抖音 Webhook、管理后台、第三方系统等。

接口定义统一集中在 `api/`，便于语言多端生成 SDK、统一变更管理。

## 四、internal/ 业务实现

`internal/` 是 Go 的特殊目录，仅本仓库可引用，用于存放具体业务实现，避免被外部依赖。

### 1. internal/gateway/

- `handler/`：处理客户端消息（登录、匹配、游戏操作等）。
- `session/`：会话管理、断线重连、用户与连接映射。
- `transport/`：对 WebSocket/gRPC Stream 抽象，提供统一接口。
- `router/`：将消息路由到各业务服务（game/match/user）。

### 2. internal/game/

- `engine/`：游戏核心逻辑和循环。
- `room/`：房间生命周期管理，包含创建、销毁、广播等。
- `state/`：房间内状态模型与持久化策略。

### 3. internal/match/

- `algo/`：匹配算法实现，包含段位匹配、超时处理等策略。
- `queue/`：匹配队列实现，结合 Redis 做排队管理。
- `service/`：对外部提供匹配相关 RPC 接口。

### 4. internal/user/

- `model/`：用户相关数据结构，与 PostgreSQL 表结构对应。
- `repo/`：用户数据访问仓储封装，对 PostgreSQL 和 Redis 操作进行统一封装。
- `service/`：用户登录、绑定、资产操作等业务逻辑。

### 5. internal/douyin/

- `client/`：基于抖音 Server SDK 的封装，统一调用抖音接口。
- `webhook/`：抖音回调入口，处理签名验证、重放防护等。
- `auth/`：抖音应用和用户授权流程管理。
- `mapper/`：抖音事件结构 → 内部 GameEvent 结构的转换逻辑。

### 6. internal/storage/

- `postgres/`：PostgreSQL 连接池、事务管理、迁移工具集成。
- `redis/`：Redis 客户端封装，支持单机、哨兵、集群。
- `repo/`：跨业务复用的数据访问层封装，例如通用分页、乐观锁。

### 7. internal/infra/

- `config/`：基于 viper 的配置加载。
- `log/`：基于 zap 的统一日志封装。
- `metrics/`：Prometheus 指标收集与暴露。
- `tracing/`：OpenTelemetry 链路追踪初始化。
- `mq/`：Kafka/NATS 封装。
- `server/`：统一的 HTTP/gRPC 服务器启动与优雅关闭逻辑。

### 8. internal/shared/

- `errors/`：错误码枚举与封装。
- `auth/`：内部服务鉴权逻辑。
- `id/`：ID 生成器（雪花等算法）。
- `time/`：时间工具封装，便于测试与模拟。

## 五、pkg/ 可复用包

`pkg/` 存放相对通用、与具体业务弱耦合的工具包，理论上可以被其他项目复用。

- `pkg/ws/`：WebSocket 抽象封装，包括 Codec、重试策略等。
- `pkg/grpcx/`：gRPC 客户端封装（重试、超时、中间件）。
- `pkg/middleware/`：HTTP/gRPC 中间件（日志、限流、恢复等）。
- `pkg/utils/`：通用工具函数（加解密、签名、字符串处理等）。

## 六、configs/ 配置

- 存放各环境的配置文件，如：
  - `config.dev.yaml`
  - `config.staging.yaml`
  - `config.prod.yaml`
- 内容包括：数据库连接、Redis 配置、MQ 地址、服务监听端口、feature flags 等。

## 七、deploy/ 部署相关

- `deploy/k8s/`
  - Kubernetes 部署文件（Deployment、Service、Ingress、HPA 等）。
- `deploy/docker/`
  - 各服务 Dockerfile，多阶段构建，体积更小。
- `deploy/scripts/`
  - 部署脚本、回滚脚本等，供 CI/CD 使用。

## 八、docs/ 文档

- 存放架构、技术栈、部署方案、开发计划等文档。
- 当前主要文档：
  - `architecture.md`：总体架构设计。
  - `tech-stack.md`：技术栈选型。
  - `deployment.md`：部署与云资源规划。
  - `development-plan.md`：开发周期与优先级规划。

## 九、scripts/ 与根目录文件

- `scripts/`
  - 存放本地开发、测试、构建相关脚本（如 `db-migrate.sh` 等）。
- `Makefile`
  - 统一开发命令入口，如：
    - `make run-gateway`
    - `make test`
    - `make build`
- `go.mod`
  - Go 模块定义与依赖版本。

通过上述目录结构，可以在保持单仓的前提下，实现多服务清晰分层，便于团队协作和后续演进为更细粒度的微服务架构。

