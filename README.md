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
