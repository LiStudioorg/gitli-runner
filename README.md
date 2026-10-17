# gitli-runner

**gitli-runner** 是 [gitli](https://github.com/LiStudioorg/gitli) 的**独立 Runner 二进制**，用于在隔离环境中执行用户仓库的工作流任务。

- **独立部署**：与 gitli 主服务分离，可运行在不同主机/容器中。
- **ID + Token 认证**：在 gitli 后台「添加 Runner」生成 `runner_id` 与 `token`，Runner 侧配置后即可使用，无需换取 token。
- **隔离执行**：默认使用 Docker/Podman 容器执行，避免污染宿主机。
- **流式日志**：任务日志实时回传到 gitli 主服务。
- **主服务永不执行用户脚本**：gitli 只做调度与日志存储，所有执行都在 Runner 侧。

许可证：**AGPL-3.0**

---

## 快速开始

```bash
# 1. 构建
make build           # 产出 ./gitli-runner（静态二进制）

# 2. 配置
cp runner.example.toml runner.toml
# 填入 runner_id / token / server

# 3. 运行
./gitli-runner run -c runner.toml
```

## 配置示例

见 [`runner.example.toml`](./runner.example.toml)。

```toml
[runner]
server = "https://git.example.com"
id = "runner-a1b2c3"
token = "glr_xxxxxxxx"
labels = ["linux", "docker"]
poll_interval = "10s"
heartbeat_interval = "30s"

[executor]
type = "docker"
image = "alpine:3.20"
timeout = "30m"
```

## 子命令

```
gitli-runner run       -c runner.toml   启动 Runner，拉取并执行任务
gitli-runner register  -c runner.toml   校验与主服务的连通性并上报信息
gitli-runner version                     打印版本
gitli-runner help                        显示帮助
```

## 与 gitli 的协议

Runner 通过以下 HTTP 端点与主服务通信（Bearer token + `X-Runner-ID` 认证）：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/v1/runner/heartbeat` | 上报心跳与能力 |
| POST | `/api/v1/runner/register` | 注册/连通性校验 |
| GET | `/api/v1/runner/jobs/next` | 拉取下一个任务（204 表示无任务） |
| POST | `/api/v1/runner/jobs/{id}/logs` | 流式回传日志分块 |
| POST | `/api/v1/runner/jobs/{id}/result` | 回传任务结果 |

## 安全说明

- 主服务下发的任务只含仓库地址、commit、workflow 内容，**不含任何 secret**。
- Token 建议通过配置文件或环境变量注入，切勿提交 `runner.toml`。
- 默认在容器内执行；`shell` 执行后端仅供调试，默认关闭。

## 构建

```bash
make build      # 本机静态编译
make test       # 单元测试
make lint       # gofmt + go vet
```


## 配置详解

完整模板见 [`runner.example.toml`](./runner.example.toml)。所有时间字段支持 `30s`/`10m`/`1h`，大小字段支持 `10MB`/`512MiB`/`1g`。

### `[core]`（必填）
| 键 | 说明 | 默认 |
| --- | --- | --- |
| `server` | 主服务地址 | — |
| `id` | Runner ID（后台生成） | — |
| `token` | Runner Token（只显示一次） | — |
| `labels` | 标签，用于工作流 `runs-on` 匹配 | `["self-hosted"]` |

### `[execution]`（并发与清理，关键）
| 键 | 说明 | 默认 |
| --- | --- | --- |
| `max_concurrent_jobs` | 最大并发任务数（信号量严格限制） | `1` |
| `job_timeout` | 单任务超时，超时 `context.WithTimeout` 终止 | `30m` |
| `work_dir` | 工作目录（拉取代码与执行脚本） | 缓存目录/临时目录 |
| `cleanup_policy` | `always` / `never` / `success` | `always` |

> **低配机器建议**：`max_concurrent_jobs = 1` 且 `cleanup_policy = "always"`，避免内存/磁盘被多任务同时打满。

### `[client]`
| 键 | 说明 | 默认 |
| --- | --- | --- |
| `poll_interval` | 轮询任务间隔 | `5s` |
| `heartbeat_interval` | 心跳间隔 | `30s` |
| `request_timeout` | 单次 API 超时 | `10s` |
| `tls_skip_verify` | 跳过 TLS 校验（仅测试） | `false` |

### `[environment]`
| 键 | 说明 |
| --- | --- |
| `variables` | 全局环境变量（可被工作流 `env` 覆盖） |
| `default_shell` | 默认 Shell；空则自动探测（Unix `sh`，Windows `cmd`） |

### `[log]`
| 键 | 说明 | 默认 |
| --- | --- | --- |
| `level` | `debug`/`info`/`warn`/`error` | `info` |
| `format` | `text`/`json` | `text` |
| `max_log_size` | 单任务日志上限（超出截断） | `10MB` |
| `file` | 日志文件（空则 stderr） | 空 |

### 工作流指定 Shell

工作流 YAML 支持 `shell: bash` / `shell: pwsh` / `shell: cmd`（Job 级或顶层）。Runner 会用 `exec.LookPath` 校验 Shell 是否存在，不存在则任务直接失败并给出明确错误；容器模式下在镜像内使用该 Shell。
