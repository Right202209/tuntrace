# tuntrace

Clash Verge / mihomo 桌面流量归因工具：把 `/connections` 数据按**进程 / 域名 / IP / 出口节点**四个维度做分钟级聚合，落到本地 SQLite，并提供内嵌 Web 页面。

> v1 仅在 Windows 桌面验证，**完全依赖 mihomo 的 `find-process-mode`** 在 `metadata.process` / `metadata.processPath` 里把进程信息填好（TUN 与系统代理均支持）。

## 快速开始

### 1. 在 mihomo / Clash Verge 的 profile YAML 里启用进程探测

```yaml
external-controller: 127.0.0.1:9090
secret: ""           # 可选
find-process-mode: always   # 必须！否则进程列全空
tun:
  enable: true       # 或者继续用系统代理也行
  stack: mixed
```

### 2. 构建（在带 Go 1.22+ 的机器上）

```bash
go mod tidy
GOOS=windows GOARCH=amd64 go build -o tuntrace.exe ./cmd/tuntrace
```

或本机直接跑：

```bash
go run ./cmd/tuntrace
```

由于走 `modernc.org/sqlite`（纯 Go），从 Linux/macOS 交叉编译 Windows 二进制无需 mingw。

### 3. 运行

```powershell
.\tuntrace.exe                                    # 默认 :8080，data\tuntrace.db
.\tuntrace.exe -listen :9000 -db D:\tunt.db
```

环境变量优先级：`MIHOMO_URL` / `MIHOMO_SECRET` > 数据库存的值 > 首次打开页面手动填。

打开浏览器 `http://localhost:8080`：
- 默认进程 tab，按下行字节排序
- 切换 域名 / 目标 IP / 出口节点 / 实时
- 右上角"设置"按钮可改 mihomo URL / secret

## 命令行参数

| flag | 默认 | 说明 |
| --- | --- | --- |
| `-listen` | `:8080` (`TUNTRACE_LISTEN`) | HTTP 监听地址 |
| `-db` | `data/tuntrace.db` (`TUNTRACE_DB`) | SQLite 路径，目录会自动创建 |
| `-poll` | `5s` | mihomo 轮询周期 |
| `-retention-days` | `30` | 聚合数据保留天数 |

## API

| Method | Path | 说明 |
| --- | --- | --- |
| GET  | `/api/health` | liveness |
| GET/POST | `/api/settings/mihomo` | 读/写 mihomo 接入信息 |
| GET  | `/api/summary?group=process\|host\|ip\|outbound&from=&to=&top=` | 维度排行（from/to 单位为 unix 分钟）|
| GET  | `/api/timeseries?dim=process&value=chrome.exe&from=&to=` | 单实体时序 |
| GET  | `/api/connections/live` | collector 内存里最近一次的活跃连接快照 |
| GET  | `/api/diagnostics` | 最近 5 分钟内 process_name 为空的字节占比，UI 用来弹横幅 |

## 数据模型

只一张事实表 `traffic_aggregated`，以 `(分钟, 进程, 进程路径, 域名, 目标IP, 出口, chains_json, 协议)` 为主键，写入用 upsert 累加 upload/download/conn_count。每小时按 `bucket_minute < cutoff` 删旧行，每周 VACUUM 一次。

`app_settings` 是 KV 表，存 mihomo URL / secret。

## 开发与测试

```bash
go test ./...
```

覆盖：
- `internal/store`：迁移幂等、KV 往返、upsert 累加、维度白名单、缺失进程占比
- `internal/collector`：新连接 / 增量 / 零增量 / counter-reset / 失活清理 / 单连接计数倒退
- `internal/aggregator`：分钟桶合并、只 flush 已完成桶、FlushAll

## v1 不做（v2 候选）

- 当 mihomo 字段为空时，原生 Windows `GetExtendedTcpTable` 反查 PID
- macOS / Linux 桌面构建
- 自动切换 / 阈值告警
- 多机聚合
- 域名分组（eTLD+1）
