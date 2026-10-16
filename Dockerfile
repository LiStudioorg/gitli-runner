# SPDX-License-Identifier: AGPL-3.0
# gitli-runner 镜像：纯静态二进制 + git + 容器运行时客户端。
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/gitli-runner .

FROM alpine:3.20
# 运行时依赖：git（拉取仓库）与 docker/podman CLI（执行任务）。
RUN apk add --no-cache git ca-certificates docker-cli \
    && adduser -D -H -s /sbin/nologin runner
WORKDIR /var/lib/gitli-runner
COPY --from=build /out/gitli-runner /usr/local/bin/gitli-runner
USER runner
ENTRYPOINT ["/usr/local/bin/gitli-runner"]
CMD ["run", "-c", "/etc/gitli-runner/runner.toml"]
