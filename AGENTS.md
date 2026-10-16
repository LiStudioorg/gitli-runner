# AGENTS.md — gitli-runner 工作手册

本文件面向参与开发 gitli-runner 的 AI Agent 与人类工程师，是最高优先级准则。
任何改动都必须遵守；若与需求冲突，先更新本文件再写代码。

提交前必须全部通过：

```bash
gofmt -l .        # 无输出
go vet ./...      # 无错误
go test ./...     # 通过
go build -o gitli-runner .   # 成功
```

---

## 1. 定位

gitli-runner 是 gitli 的**独立 Runner 二进制**，与主服务分离部署，负责在**隔离环境**中执行用户仓库的工作流任务，并把日志与结果回传主服务。

核心边界：

- **主 gitli 永不执行用户脚本**；Runner 负责执行，且默认在容器内隔离运行。
- Runner 通过 **ID + Token** 与主服务通信，Token 由 gitli 后台生成，明文只在生成时显示一次。
- **所有源文件首行加 SPDX 标识**：`// SPDX-License-Identifier: AGPL-3.0`。

## 2. 核心约束

1. **单二进制**：`go build -o gitli-runner .` 只产出一个可执行文件。
2. **静态编译**：`CGO_ENABLED=0 -trimpath -ldflags="-s -w"`。
3. **无外部强依赖**：仅依赖系统 `git` 与容器运行时（docker/podman）。
4. 所有子进程调用使用 `exec.CommandContext`，分开传参，**禁止 shell 字符串拼接**，必须带超时与环境变量白名单。
5. 大文件/日志流式处理，禁止一次性 `io.ReadAll`。
6. 配置格式 **TOML**，库用 `github.com/BurntSushi/toml`；工作流用 YAML，库用 `gopkg.in/yaml.v3`。
7. 日志用 `log/slog`。

## 3. 技术栈

Go 1.23+、`github.com/BurntSushi/toml`、`gopkg.in/yaml.v3`、标准库 `net/http`、`os/exec`、`log/slog`。

## 4. 目录结构

```
gitli-runner/
├── main.go                     # 入口，子命令分发
├── go.mod / go.sum
├── Makefile
├── README.md / AGENTS.md / LICENSE
├── runner.example.toml
├── Dockerfile / docker-compose.yml / .dockerignore
├── cmd/                        # 预留子命令实现
└── internal/
    ├── config/                 # TOML 配置加载
    ├── logx/                   # slog 封装
    ├── client/                 # 与主服务的 HTTP 协议客户端
    ├── executor/               # 执行后端（docker / podman / shell）
    ├── workflow/               # workflow YAML 解析
    ├── runner/                 # 任务循环与生命周期
    └── register/               # 注册/连通性校验
```

## 5. 依赖方向

```
main.go → internal/{config,logx,register,runner}
runner  → internal/{client,config,executor,workflow}
client  → internal/config
executor→ internal/{config,logx}
workflow→ 仅标准库 + yaml
config  → 仅标准库 + toml
```

禁止循环依赖。

## 6. 与 gitli 主服务的协议

认证：`Authorization: Bearer <token>` + `X-Runner-ID: <runner_id>`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/v1/runner/heartbeat` | 心跳与能力上报 |
| POST | `/api/v1/runner/register` | 注册/连通性校验 |
| GET  | `/api/v1/runner/jobs/next` | 拉取任务，204 无任务 |
| POST | `/api/v1/runner/jobs/{id}/logs` | 流式日志分块（octet-stream） |
| POST | `/api/v1/runner/jobs/{id}/result` | 结果 `{status, exit_code, error}` |

任务结构（`jobs/next` 返回）：

```json
{
  "id": "123",
  "run_id": "123",
  "repo_url": "https://git.example.com/owner/repo.git",
  "ref": "refs/heads/main",
  "sha": "abc123",
  "token": "",
  "workflow": "<yaml content>",
  "env": {},
  "timeout_sec": 1800
}
```

## 7. 安全要求

- Token 不写日志、不落库（Runner 侧无数据库），仅内存持有。
- 默认容器执行；`shell` 后端默认关闭，启用需显式配置。
- 容器可配置资源限制（cpus/memory/pids）与网络策略。
- 任务 payload 不含 secret；secret 由独立通道拉取（预留）。
- 日志回传分块，避免打满网络。

## 8. 编码规范

- 每个 `.go` 文件首行 `// SPDX-License-Identifier: AGPL-3.0`。
- 错误用 `fmt.Errorf("...: %w", err)` 包装；外部输入一律校验。
- 不写无意义注释；注释解释「为什么」。
- 必须通过 `gofmt`、`go vet`。
