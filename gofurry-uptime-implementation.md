# gofurry/uptime 项目实施文档

> 仓库：`github.com/gofurry/uptime`  
> 目标：实现一个基于 `net/http` 的轻量级 Go 服务在线率中间件，支持单机 SQLite 与多机 PostgreSQL，记录最近 N 天服务在线率，并提供内嵌状态页面与 JSON API。

---

## 1. 项目目标

`gofurry/uptime` 用于解决一个明确问题：

> 在不引入 Prometheus、Grafana、Uptime Kuma 等完整监控系统的前提下，为 Go 服务提供轻量级的历史在线率展示。

核心能力：

- 基于 `net/http`，可作为普通 Go Web 服务中间件接入。
- 后台定时写入服务心跳，默认每 3 秒一次。
- 按日统计服务在线率。
- 页面展示最近 N 天每日在线率。
- 使用红 / 黄 / 绿小竖条表示每日可用性。
- 支持 SQLite 单机模式。
- 支持 PostgreSQL 多机共享模式。
- 支持多个 Go 服务共享同一个存储。
- 支持数据保留天数、采集频率、服务 ID、实例 ID、存储路径等配置。
- 历史原始采样自动汇总为日快照，删除旧采样，保持轻量。

---

## 2. 非目标

第一阶段不要做成完整监控系统，明确不做以下能力：

- 不替代 Prometheus / Grafana。
- 不提供复杂指标采集，例如 CPU、内存、磁盘、网络。
- 不提供告警通知。
- 不做外部公网探活。
- 不做跨区域探测。
- 不做分布式一致性协调。
- 不做复杂状态页发布系统。
- 不做用户、权限、团队、多租户后台。
- 不做复杂图表库依赖。

这个库的核心是：

> 当前 Go 进程是否持续存活，以及最近 N 天在线率如何。

---

## 3. 和 `gofurry/monitor` 的边界

`gofurry/monitor` 适合展示实时运行状态，例如 CPU、内存、请求延迟、Go runtime 等。

`gofurry/uptime` 适合展示历史在线率，例如最近 30 天每天是否稳定在线。

建议形成互补关系：

```text
gofurry/monitor  -> 当前状态、实时快照
gofurry/uptime   -> 历史在线率、每日可用性
```

不要把 uptime history 直接塞进 `monitor`，否则 `monitor` 会从轻量实时面板变成带历史存储的监控系统，边界会变重。

---

## 4. 核心设计原则

### 4.1 单机优先，多机可选

默认体验应该足够轻：

```text
一个 Go 服务
一个 SQLite 文件
一个 /uptime 页面
最近 N 天在线率
```

高级场景再启用 PostgreSQL：

```text
多台机器
多个 Go 服务
共享同一个 PostgreSQL
统一展示所有服务在线率
```

### 4.2 只写 up，不写 down

服务宕机时，进程已经无法写入 `down` 记录。

因此模型应该是：

```text
服务存活时，每隔 sample interval 写入一个 heartbeat。
缺失的 heartbeat slot 视为 down。
```

不要设计成：

```text
每 3 秒写一条 up/down 状态。
```

而应该设计成：

```text
每 3 秒写入一条 up sample。
没有 sample 的时间槽天然表示 down。
```

### 4.3 在线率按 slot 统计

假设采集频率是 3 秒：

```text
一天最大 slot 数 = 24 * 60 * 60 / 3 = 28800
```

某天有 28700 个有效 heartbeat slot：

```text
uptime_rate = 28700 / 28800 = 99.65%
```

对于当天，分母不能用完整一天，否则早上看今日在线率会很低。当天分母应使用：

```text
从当天 00:00 到当前时刻的理论 slot 数
```

### 4.4 历史原始采样自动归档

原始采样只需要保留当天或最近 1~2 天。

进入新的一天后，把昨天及更早的采样汇总到 `uptime_daily`，然后删除旧采样。

最终存储应该保持为：

```text
今日 raw samples + 最近 N 天 daily snapshots
```

### 4.5 服务和实例分离

跨机器后必须区分两个概念：

```text
Service  = 逻辑服务，例如 gofurry-api
Instance = 某次运行实例，例如 gofurry-api@server-a pid=1234
```

默认展示服务级在线率。

同一个 service 下，只要某个 instance 在某个 slot 有 heartbeat，就认为该 service 在该 slot 在线。

---

## 5. 架构设计

### 5.1 模块结构

建议目录：

```text
uptime/
├── config.go
├── uptime.go
├── middleware.go
├── handler.go
├── heartbeat.go
├── rollup.go
├── model.go
├── store.go
├── id.go
├── internal/
│   ├── timeutil/
│   ├── html/
│   │   ├── page.go
│   │   └── assets.go
│   └── sqlutil/
├── store/
│   ├── sqlite/
│   │   ├── sqlite.go
│   │   ├── schema.go
│   │   └── queries.go
│   └── postgres/
│       ├── postgres.go
│       ├── schema.go
│       └── queries.go
├── examples/
│   ├── basic/
│   ├── sqlite/
│   └── postgres/
└── README.md
```

### 5.2 核心流程

```text
应用启动
  |
  v
uptime.New(config)
  |
  ├─ 初始化 Store
  ├─ 创建 / 更新 service
  ├─ 创建 instance
  ├─ 执行历史 rollup
  ├─ 执行 cleanup
  └─ 启动 heartbeat goroutine
          |
          v
    每 sample interval 写入 heartbeat
          |
          v
    跨天时 rollup 昨天及更早数据
```

页面请求：

```text
GET /uptime
  |
  v
Query services
Query recent daily snapshots
Query today raw samples
Merge result
Render HTML
```

JSON 请求：

```text
GET /uptime/api/status
  |
  v
返回 services + daily bars + current status
```

---

## 6. 推荐公开 API

### 6.1 基础用法

```go
package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gofurry/uptime"
	"github.com/gofurry/uptime/store/sqlite"
)

func main() {
	mux := http.NewServeMux()

	up, err := uptime.New(uptime.Config{
		ServiceID:      "demo-api",
		ServiceName:    "Demo API",
		SampleInterval: 3 * time.Second,
		RetentionDays:  30,
		DaysToShow:     30,
		Store: sqlite.New(sqlite.Config{
			Path: "./uptime.db",
		}),
	})
	if err != nil {
		log.Fatal(err)
	}
	defer up.Close()

	mux.Handle("/", up.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	})))

	mux.Handle("/uptime", up.Handler())

	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

### 6.2 PostgreSQL 用法

```go
up, err := uptime.New(uptime.Config{
	ServiceID:      "gofurry-api",
	ServiceName:    "GoFurry API",
	SampleInterval: 3 * time.Second,
	RetentionDays:  30,
	DaysToShow:     30,
	NodeID:          1,

	Store: postgres.New(postgres.Config{
		DSN: "postgres://user:pass@127.0.0.1:5432/uptime?sslmode=disable",
	}),
})
```

### 6.3 Config 设计

```go
type Config struct {
	ServiceID          string
	ServiceName        string
	ServiceDescription string

	SampleInterval time.Duration
	RetentionDays  int
	DaysToShow     int

	Timezone *time.Location

	NodeID      int64
	InstanceID  int64
	IDGenerator IDGenerator

	Store Store

	UI UIConfig

	Logger Logger
}
```

### 6.4 UIConfig

```go
type UIConfig struct {
	Title string
	Path  string

	GreenThreshold  float64 // default 0.99
	YellowThreshold float64 // default 0.95

	ShowInstanceDetails bool
}
```

默认颜色规则：

```text
绿色：uptime_rate >= 99%
黄色：95% <= uptime_rate < 99%
红色：uptime_rate < 95%
灰色：无数据
```

---

## 7. Store 抽象

### 7.1 Store Interface

核心层不要直接依赖 SQLite 或 PostgreSQL。

```go
type Store interface {
	Init(ctx context.Context) error

	UpsertService(ctx context.Context, service Service) error
	UpsertInstance(ctx context.Context, instance Instance) error

	WriteHeartbeat(ctx context.Context, heartbeat Heartbeat) error

	RollupDaily(ctx context.Context, options RollupOptions) error
	Cleanup(ctx context.Context, options CleanupOptions) error

	ListServices(ctx context.Context) ([]Service, error)
	QueryDaily(ctx context.Context, options QueryDailyOptions) ([]DailyStatus, error)
	QueryTodaySamples(ctx context.Context, options QueryTodaySamplesOptions) ([]TodaySampleStatus, error)

	Close() error
}
```

### 7.2 SQLite Store

适合：

```text
单机服务
多个本机 Go 服务共享同一个 SQLite 文件
轻量部署
无额外数据库
```

建议 SQLite 初始化：

```sql
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA busy_timeout = 5000;
```

SQLite 模式注意：

- 文件路径需要可写。
- 多个进程共享同一个 SQLite 文件时必须在同一台机器上。
- 不建议把 SQLite 文件放到 NFS 等网络文件系统。
- 高频写入下需要处理 `database is locked`，应设置 `busy_timeout` 并使用短事务。

### 7.3 PostgreSQL Store

适合：

```text
多机器
多个服务
多个实例
共享一个中心数据库
统一 dashboard
```

PostgreSQL 模式注意：

- 所有服务使用同一套 DSN。
- 每台机器的 `NodeID` 应不同。
- 服务时间最好使用统一时区。
- 插入 heartbeat 应使用 upsert / on conflict do nothing。
- day / slot 的生成规则必须全局一致。

---

## 8. 数据模型

### 8.1 Service

逻辑服务，稳定存在。

```go
type Service struct {
	ID              string
	Name            string
	Description     string
	CreatedAt       time.Time
	LastSeenAt      time.Time
	SampleInterval  time.Duration
}
```

`ServiceID` 必须由用户显式配置，例如：

```text
gofurry-api
gofurry-worker
gofurry-collector
```

不要用雪花 ID 作为 `service_id`，因为 service 是业务身份，应该稳定、可读、可配置。

### 8.2 Instance

一次进程运行实例。

```go
type Instance struct {
	ID        int64
	ServiceID string
	Hostname  string
	PID       int
	StartedAt time.Time
	LastSeenAt time.Time
}
```

`instance_id` 可以使用雪花算法生成。

### 8.3 Heartbeat

一次有效心跳采样。

```go
type Heartbeat struct {
	ServiceID  string
	InstanceID int64
	Day        string
	Slot       int64
	SeenAt     time.Time
}
```

### 8.4 DailyStatus

日聚合快照。

```go
type DailyStatus struct {
	ServiceID       string
	Day             string
	UpSlots         int
	ExpectedSlots   int
	UptimeRate      float64
	Finalized       bool
}
```

---

## 9. 数据库 Schema

### 9.1 services

```sql
CREATE TABLE IF NOT EXISTS services (
    service_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL,
    last_seen_at BIGINT NOT NULL,
    sample_interval_seconds INTEGER NOT NULL
);
```

### 9.2 instances

```sql
CREATE TABLE IF NOT EXISTS instances (
    instance_id BIGINT PRIMARY KEY,
    service_id TEXT NOT NULL,
    hostname TEXT NOT NULL DEFAULT '',
    pid INTEGER NOT NULL DEFAULT 0,
    started_at BIGINT NOT NULL,
    last_seen_at BIGINT NOT NULL
);
```

### 9.3 uptime_samples

```sql
CREATE TABLE IF NOT EXISTS uptime_samples (
    service_id TEXT NOT NULL,
    instance_id BIGINT NOT NULL,
    day TEXT NOT NULL,
    slot BIGINT NOT NULL,
    seen_at BIGINT NOT NULL,

    PRIMARY KEY (service_id, instance_id, day, slot)
);
```

### 9.4 uptime_daily

```sql
CREATE TABLE IF NOT EXISTS uptime_daily (
    service_id TEXT NOT NULL,
    day TEXT NOT NULL,
    up_slots INTEGER NOT NULL,
    expected_slots INTEGER NOT NULL,
    uptime_rate REAL NOT NULL,
    finalized INTEGER NOT NULL,

    PRIMARY KEY (service_id, day)
);
```

### 9.5 索引

```sql
CREATE INDEX IF NOT EXISTS idx_uptime_samples_service_day
ON uptime_samples(service_id, day);

CREATE INDEX IF NOT EXISTS idx_uptime_samples_day
ON uptime_samples(day);

CREATE INDEX IF NOT EXISTS idx_uptime_daily_service_day
ON uptime_daily(service_id, day);
```

---

## 10. Slot 计算

### 10.1 基本规则

```go
func SlotOf(t time.Time, interval time.Duration) int64 {
	return t.Unix() / int64(interval.Seconds())
}
```

同一个 slot 内重复写入，应通过主键去重。

### 10.2 Day 计算

```go
func DayOf(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02")
}
```

### 10.3 Expected Slots

完整一天：

```go
expected := int((24 * time.Hour) / sampleInterval)
```

当天：

```go
startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
expectedToday := int(now.Sub(startOfDay) / sampleInterval)
```

注意：

- `expectedToday` 最小应为 1，避免除零。
- 跨夏令时地区时，一天不一定是 24 小时。默认可以用固定 86400 秒处理，或者在高级版本中按 `startOfDay -> nextDay` 计算。
- 建议默认使用 `time.Local`，也允许用户配置 `Timezone`。
- 多机模式下，所有服务建议使用同一时区。

---

## 11. 在线率计算

### 11.1 单实例

```text
uptime_rate = count(distinct slot) / expected_slots
```

### 11.2 多实例同一服务

同一个 service 下，只要任意 instance 在某个 slot 有 heartbeat，该 service 视为在线。

因此服务级聚合是：

```sql
COUNT(DISTINCT slot)
```

而不是：

```sql
COUNT(*)
```

### 11.3 示例

3 秒采样一次：

```text
expected_slots = 28800
up_slots = 28700
uptime_rate = 28700 / 28800 = 0.9965
```

展示为：

```text
99.65%
```

---

## 12. Rollup 设计

### 12.1 触发时机

不要只依赖午夜定时器。服务可能午夜时宕机。

应在以下时机调用轻量 rollup：

- 应用启动时。
- 每次写 heartbeat 前或后。
- 页面查询前。
- 定时后台任务中。

### 12.2 Rollup 范围

只汇总今天之前的日期：

```text
day < today
```

对于历史日期：

```text
expected_slots = full_day_expected_slots
```

### 12.3 Rollup SQL 逻辑

逻辑服务级汇总：

```sql
SELECT
    service_id,
    day,
    COUNT(DISTINCT slot) AS up_slots
FROM uptime_samples
WHERE day < ?
GROUP BY service_id, day;
```

写入 `uptime_daily`：

```text
service_id
day
up_slots
expected_slots
uptime_rate
finalized = true
```

然后删除已汇总的 raw samples：

```sql
DELETE FROM uptime_samples
WHERE day < ?;
```

### 12.4 幂等性

Rollup 必须可重复执行。

使用 upsert：

```text
PRIMARY KEY(service_id, day)
```

重复 rollup 同一天时，覆盖已有 daily snapshot。

---

## 13. Cleanup 设计

### 13.1 Daily Retention

保留最近 N 天 daily 快照：

```text
RetentionDays = 30
```

删除：

```text
day < today - RetentionDays
```

### 13.2 Samples Retention

默认只保留今天 raw samples。

也可以保留最近 1~2 天，用于容错：

```text
RawSampleRetentionDays = 2
```

第一版可以不暴露该配置，内部默认保留今天即可。

---

## 14. Heartbeat 写入

### 14.1 心跳循环

```go
func (u *Uptime) startHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(u.config.SampleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			u.writeHeartbeat(ctx, now)
		}
	}
}
```

### 14.2 写入内容

每次心跳应更新：

- `services.last_seen_at`
- `instances.last_seen_at`
- `uptime_samples`

### 14.3 写入失败处理

写入失败不应该导致业务服务退出。

建议：

- 记录错误日志。
- 连续失败可以在 dashboard 标记 storage error。
- 不 panic。
- 不影响主业务 HTTP handler。

---

## 15. 雪花 ID 设计

### 15.1 使用位置

建议：

```text
service_id   -> 用户配置的稳定字符串
instance_id  -> 雪花 ID
event_id     -> 暂不需要
heartbeat_id -> 暂不需要
```

不要给 heartbeat 单独创建 ID，使用复合主键即可。

### 15.2 IDGenerator 接口

```go
type IDGenerator interface {
	NextID() int64
}
```

### 15.3 默认策略

建议支持两种：

1. 用户显式传入 `InstanceID`。
2. 用户传入 `IDGenerator`。
3. 用户配置 `NodeID`，内部创建 snowflake generator。
4. 如果都没有，使用 hostname hash + pid + timestamp 生成一个低冲突 instance id。

### 15.4 NodeID 注意事项

多机 PostgreSQL 模式下，每台机器的 `NodeID` 应不同。

配置示例：

```go
uptime.Config{
	NodeID: 1,
}
```

文档中需要明确说明：

```text
NodeID must be unique across machines when using the built-in snowflake generator.
```

---

## 16. HTTP Handler 与 Middleware

### 16.1 Middleware 职责

`Middleware` 不应该每个请求都写 heartbeat。heartbeat 是后台 ticker 写入的。

Middleware 的主要作用：

- 让用户以中间件方式接入。
- 未来可以扩展 request-aware 信息。
- 保持和 `net/http` 使用习惯一致。

```go
func (u *Uptime) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}
```

### 16.2 Dashboard Handler

```go
func (u *Uptime) Handler() http.Handler
```

建议路由：

```text
GET /uptime
GET /uptime/api/status
```

如果用户只挂载 `/uptime`，内部根据 path 分发。

### 16.3 JSON API

```text
GET /uptime/api/status
```

返回结构：

```json
{
  "generated_at": "2026-06-24T10:00:00Z",
  "sample_interval_seconds": 3,
  "days": 30,
  "services": [
    {
      "id": "gofurry-api",
      "name": "GoFurry API",
      "last_seen_at": "2026-06-24T09:59:57Z",
      "current_status": "up",
      "daily": [
        {
          "day": "2026-06-24",
          "uptime_rate": 0.998,
          "up_slots": 12000,
          "expected_slots": 12024,
          "finalized": false
        }
      ]
    }
  ]
}
```

---

## 17. Dashboard UI 设计

### 17.1 页面目标

页面只展示必要信息：

- 服务名。
- 当前状态。
- 最近 N 天每日在线率。
- 每日小竖条。
- 鼠标悬浮 tooltip。
- 最后心跳时间。
- 采集频率。
- 存储模式。

### 17.2 小竖条

示意：

```text
GoFurry API       ▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏
Worker            ▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏
Collector         ▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏▏
```

### 17.3 Tooltip

每日小竖条 tooltip：

```text
Date: 2026-06-24
Uptime: 99.65%
Up slots: 28700 / 28800
Estimated downtime: 5m
Status: Finalized
```

当天：

```text
Date: 2026-06-24
Uptime so far: 99.80%
Up slots: 12000 / 12024
Status: Today, not finalized
```

### 17.4 当前状态判断

当前状态可以通过 `last_seen_at` 判断：

```text
now - last_seen_at <= sample_interval * 2  => up
now - last_seen_at > sample_interval * 2   => down
```

可配置：

```go
StatusTimeoutMultiplier int // default 2
```

第一版可以内置，不暴露。

---

## 18. 单机 / 多机模式

### 18.1 SQLite 单机模式

适合部署：

```text
api 服务
worker 服务
collector 服务

都在同一台机器上
都写入 /var/lib/gofurry/uptime.db
```

每个服务配置不同 `ServiceID`：

```go
ServiceID: "api"
ServiceID: "worker"
ServiceID: "collector"
```

### 18.2 PostgreSQL 多机模式

适合部署：

```text
server-a: api
server-b: worker
server-c: collector

都写入同一个 PostgreSQL
```

展示页面可以挂在任意一个服务上，只要它能读同一个 PostgreSQL。

### 18.3 多副本服务

例如：

```text
gofurry-api@server-a
gofurry-api@server-b
```

两者使用同一个：

```go
ServiceID: "gofurry-api"
```

但有不同：

```go
InstanceID
NodeID
hostname
pid
```

服务级 uptime 统计时：

```text
任意 instance 有 heartbeat => service 在线
```

---

## 19. 错误处理策略

### 19.1 初始化错误

初始化 Store 失败时，`uptime.New` 返回错误。

用户可自行决定是否终止业务服务。

### 19.2 运行期写入错误

heartbeat 写入失败：

- 不影响主业务 handler。
- 记录日志。
- 记录最近错误到内存状态。
- dashboard 可以展示 storage error。

### 19.3 查询错误

dashboard 查询失败：

- 返回 HTTP 500。
- 页面展示简洁错误信息。
- JSON API 返回错误结构。

---

## 20. 并发与锁

### 20.1 SQLite

注意点：

- 每个写入事务要短。
- 不要长时间持有事务。
- 使用 WAL。
- 设置 `busy_timeout`。
- 写 heartbeat 使用 upsert / insert ignore。
- 多进程共享同一文件时，路径必须一致且可写。

### 20.2 PostgreSQL

注意点：

- 使用连接池。
- 写入 heartbeat 使用 `ON CONFLICT DO NOTHING`。
- 更新 service / instance 使用 `ON CONFLICT DO UPDATE`。
- Rollup 可重复执行。
- 可以用事务包住 rollup + delete。

---

## 21. 测试计划

### 21.1 单元测试

覆盖：

- day 计算。
- slot 计算。
- expected slots 计算。
- uptime rate 计算。
- color threshold 计算。
- current status 判断。
- service / instance ID 生成。
- rollup 幂等性。

### 21.2 SQLite 集成测试

覆盖：

- 初始化 schema。
- 写入 heartbeat。
- 重复 slot 不重复计数。
- 多 service 写入。
- rollup 后 daily 正确。
- cleanup 后旧数据删除。
- dashboard query 正确。

### 21.3 PostgreSQL 集成测试

覆盖：

- schema 初始化。
- 多 instance 同 service 去重聚合。
- 不同 service 独立统计。
- `ON CONFLICT DO NOTHING` 正常。
- rollup 事务正确。
- 多连接并发写入。

PostgreSQL 测试可以使用 Docker Compose 或 testcontainers。

### 21.4 并发测试

覆盖：

- 多 goroutine 同时写 heartbeat。
- 多 service 同时写同一个 SQLite。
- rollup 和 heartbeat 并发。
- dashboard 查询和 heartbeat 并发。

### 21.5 Benchmark

建议 benchmark：

- heartbeat write SQLite。
- heartbeat write PostgreSQL。
- dashboard query 30 days / 10 services。
- HTML render。
- JSON render。

---

## 22. 推荐开发阶段

### Phase 0：仓库初始化

目标：

- 创建 Go module。
- 添加基础 README。
- 添加 MIT 或 Apache-2.0 协议。
- 添加 CI。
- 添加 gofmt / go test。
- 定义 package API。

任务：

```text
go mod init github.com/gofurry/uptime
mkdir -p store/sqlite examples/basic
```

### Phase 1：核心模型与 SQLite

目标：

- 完成 `Config`。
- 完成 `Store` interface。
- 完成 SQLite store。
- 完成 heartbeat ticker。
- 完成 daily rollup。
- 完成 cleanup。
- 完成 JSON API。

验收：

- 一个 Go demo 可以运行。
- 访问 `/uptime/api/status` 能看到最近 N 天状态。
- SQLite 文件中只保留必要数据。
- 重启服务后统计连续。

### Phase 2：Dashboard UI

目标：

- 内嵌 HTML 页面。
- 服务列表。
- 当前 up/down 状态。
- 最近 N 天小竖条。
- tooltip 展示详细统计。
- 支持亮色 / 暗色。

验收：

- 访问 `/uptime` 可以直接看到状态页。
- 页面无外部依赖。
- 移动端可读。
- 小竖条颜色正确。

### Phase 3：PostgreSQL Store

目标：

- 实现 PostgreSQL store。
- 支持多机器共享数据库。
- 支持多 instance 聚合。
- 支持雪花 instance ID。
- 补充 examples/postgres。

验收：

- 两个不同进程写同一个 PostgreSQL。
- 同 service 多 instance 只按 distinct slot 统计。
- 不同 service 可以统一展示。
- PostgreSQL dashboard 正常。

### Phase 4：稳定性与发布

目标：

- 完善测试。
- 添加 benchmark。
- 完善 README。
- 添加配置文档。
- 发布 v0.1.0。

验收：

- CI 通过。
- 主要 API 稳定。
- SQLite 模式可作为默认推荐。
- PostgreSQL 模式可作为高级功能。

---

## 23. README 首屏建议

README 第一屏只保留核心信息：

```md
# uptime

Tiny uptime history middleware for Go net/http.

- Records heartbeat samples in the background
- Shows daily uptime bars for the last N days
- SQLite for single-machine deployments
- PostgreSQL for multi-machine deployments
- No Prometheus, no Grafana, no external service required
```

安装：

```bash
go get github.com/gofurry/uptime
```

基础示例：

```go
up, _ := uptime.New(uptime.Config{
    ServiceID:      "demo-api",
    ServiceName:    "Demo API",
    SampleInterval: 3 * time.Second,
    RetentionDays:  30,
    Store: sqlite.New(sqlite.Config{Path: "./uptime.db"}),
})
defer up.Close()

mux.Handle("/", up.Middleware(app))
mux.Handle("/uptime", up.Handler())
```

---

## 24. 第一版推荐默认值

```go
SampleInterval: 3 * time.Second
RetentionDays:  30
DaysToShow:     30
Timezone:       time.Local

GreenThreshold:  0.99
YellowThreshold: 0.95
```

当前状态：

```text
last_seen_at <= now - sample_interval * 2 => down
```

默认存储：

```text
SQLite
```

---

## 25. 实施注意事项

### 25.1 不要让 dashboard 影响业务服务

dashboard 查询失败不能影响主业务 handler。

heartbeat 写入失败也不能影响主业务 handler。

### 25.2 不要过度设计

第一版只做：

```text
heartbeat
daily uptime
SQLite
HTML dashboard
JSON API
```

PostgreSQL 可以在 Store 接口稳定后加入。

### 25.3 不要保存过多 raw samples

raw samples 是中间数据，不是长期数据。

长期展示只需要 `uptime_daily`。

### 25.4 不要把 service_id 自动化

`service_id` 是用户的业务身份，必须稳定。

自动生成会导致每次重启都出现新服务，破坏历史统计。

### 25.5 不要把 down 写入数据库

down 是由缺失 heartbeat 推导出来的，不需要写入。

---

## 26. 最小可行版本定义

v0.1.0 应该包含：

- `net/http` 接入。
- `uptime.New`。
- `Middleware`。
- `Handler`。
- SQLite store。
- 3 秒默认 heartbeat。
- service + instance 模型。
- 日在线率聚合。
- 最近 30 天小竖条页面。
- JSON API。
- 自动 rollup。
- 自动 cleanup。
- 基础测试。

v0.1.0 可以暂不包含：

- PostgreSQL。
- 告警。
- 用户认证。
- 多主题自定义。
- instance 详情页。
- 外部探活。
- Prometheus exporter。

---

## 27. 后续可扩展方向

在核心稳定后可以考虑：

- PostgreSQL store。
- instance 维度详情。
- 服务分组。
- 只读 token。
- 自定义 dashboard path。
- 自定义阈值。
- 嵌入到已有管理后台。
- 导出 JSON 给前端自绘。
- 可选 Prometheus exporter。

这些都应该在核心稳定之后再做，不要影响第一版轻量定位。

---

## 28. 最终设计结论

`gofurry/uptime` 的推荐定位：

> 一个轻量级 Go `net/http` uptime history middleware，用 heartbeat slot 记录服务存活状态，用 SQLite 支持单机，用 PostgreSQL 支持多机，以日颗粒度展示最近 N 天在线率。

核心技术路线：

```text
服务活着时写 heartbeat
缺失 heartbeat 推导 down
按 slot 去重
按 day 聚合
历史 raw samples 自动汇总并删除
SQLite 默认
PostgreSQL 可选
service_id 稳定配置
instance_id 使用雪花或自定义生成器
内嵌 HTML 展示每日小竖条
JSON API 提供数据
```

第一版重点：

```text
小
稳
无外部依赖页面
SQLite 可用
API 清晰
后续可扩展 PostgreSQL
```
