# Phase 3 开发计划 — 数据层 ORM 化（Ent）先行专项

> **对应项目**：Maple Gateway（Multi-Tenant Gateway）
> **技术栈**：Go 1.26+ / Ent（ORM）/ PostgreSQL 16 / pgx（底层驱动）/ Fiber v3 / Redis 7
> **文档状态**：v1.0-plan（开发执行依据，随实现推进勾选与修订）
>
> **配套文档**：[Phase 1 计划](./plan-phase1.md) · [Phase 2 计划](./plan-phase2.md) · [项目功能全面分析.md](./项目功能全面分析.md) · [项目目录结构.md](./项目目录结构.md) · [API接口全面分析.md](./API接口全面分析.md) · [技术栈选型与开发准备清单.md](./技术栈选型与开发准备清单.md)

---

## 一、Phase 3 目标

> **手写 SQL 控制面 → Ent（ORM）类型安全数据层，为后续分布式能力提供统一、可迁移的数据访问基座**

Phase 1/2 用 pgx 手写 SQL 完成了全部控制面持久化（17 张表、15 个业务包、自研 migration runner 0001–0008）。Phase 3 先行专项把控制面数据访问层整体切换到 **Ent（entgo.io/ent）**：以 Ent Schema 作为表结构的**单一事实来源**，用代码生成替代手写 `cols` 常量与 `scanXxx` 映射，在保留底层 pgx 连接池的前提下消灭手工 SQL 样板，并让后续 Gateway HA 的配置同步 / 分布式状态获得类型安全的数据访问基座。

```text
Phase 1/2（已交付）
Domain → Tenant → Service → Deployment → Version → Instance → Node
控制面数据访问 = pgx 手写 SQL + 自研 migration（0001–0008）

Phase 3（本专项）
控制面数据访问 = Ent Schema（单一事实来源）→ 代码生成 → 类型安全仓储
底层连接 = pgx（不变）│ 迁移 = 自研 runner 与 Ent migrate 二选一并稳定
```

### 核心原则

| 原则 | 说明 |
|------|------|
| **控制面专项** | 只改控制面数据访问层，**数据平面主链路读内存 Route Cache，不因改造触碰** |
| **先 Schema 后迁移** | Ent Schema 是结构唯一来源；迁移策略先与 0001–0008 对齐，不引入不可回滚的双轨漂移 |
| **仓储接口不变** | Repository 对外方法签名 / 错误语义（NotFound/Conflict 等）保持兼容，service/handler 无感知替换 |
| **可增量落地** | 每包独立替换可验证，不必一次性全量切换 |
| **数据面不查库延续** | ORM 只服务低频控制面读写，不进入请求转发主链路 |
| **性能不回归** | 聚合 / 复杂查询仍可走原生 SQL；Ent 生成的查询不得劣化现有 dashboard/audit 等聚合接口 |
| **底层 pgx 不变** | Ent 运行于 pgx 之上，连接池 / 事务管理方式对齐现状 |

---

## 二、Phase 3 范围边界

### 2.1 本专项要做

| 能力 | 说明 |
|------|------|
| Ent 引入 | 安装 `entgo.io/ent`，初始化 Schema，接入现有 `*pgxpool.Pool` |
| Schema 建模 | 17 张存量表逐一映射为 Ent Schema（字段 / 唯一约束 / 索引 / 外键与删除策略） |
| 迁移对齐 | 决定 Ent migrate 与自研 runner 的并存 / 切换策略，保证表结构与 0001–0008 产物一致 |
| 代码生成 | `go generate` 生成 ent 客户端，替换手写 `cols` / `scanXxx` |
| 仓储重构 | 15 个业务包 repository 改为基于 ent client，对外签名不变 |
| 复杂查询 | dashboard 聚合、audit 过滤等保留 / 迁移为 Ent 支持的 SQL 或原生查询，性能不回归 |
| 校验与回归 | `go test ./...` 全绿、端到端 CRUD / 发布动作回归、性能复测 |

### 2.2 本专项不做（延后 Phase 3 后续 / Phase 4）

| 能力 | 归属 | 说明 |
|------|------|------|
| Gateway 多实例 HA / 配置同步 / Leader | Phase 3 后续 | 依赖统一数据层基座，故 Ent 先行 |
| 自动 Canary（指标驱动推进 / 自动回滚） | Phase 3 后续 | 本专项只做数据层切换，不触碰发布编排逻辑 |
| OpenTelemetry / 分布式追踪 | Phase 3 后续 | 仅保留 Schema/仓储预留 |
| Consistent Hash / Least Connections | Phase 3 后续 | 与数据层无关，本期不动 |
| 多区域路由 / Node 区域亲和 | Phase 3 后续 | 本期不扩展模型 |
| 完整 RBAC / 子账号 / Cloudflare for SaaS 自动化 | Phase 3 后续 | 本期不涉及 |
| Podman 兼容 / 插件机制 | Phase 3 后续 | 本期不涉及 |

> 本阶段定位为 **Phase 3 先行专项**，只专注数据层 ORM 化。原计划界定的 HA / OTel / 自动 Canary 等能力在本文档末节以"后续衔接"形式保留，不在本专项实现。

---

## 三、起点（Phase 1/2 已交付）

- **表**：17 张 —— `tenants` / `domains` / `services` / `instances` / `health_checks` / `admins` / `audit_logs` / `nodes` / `deployments` / `deployment_versions` / `traffic_policies` / `rate_limits` / `canary_releases` / `canary_events` / `bluegreen_deployments` / `bluegreen_events` / `settings`
- **迁移**：自研 runner（`pkg/migrate.go`，`schema_migrations` 表 + 0001–0008 SQL，幂等可重跑）
- **数据访问**：pgx 手写 SQL repository，`cols` 常量 + `scanXxx` 模式，覆盖包列表见下文 S3
- **数据面**：请求转发读 Memory Route Cache，永不逐请求查 PostgreSQL（Ent 改造不触碰）

### 依赖与目录口径

- 当前依赖：`github.com/jackc/pgx/v5`（驱动 / 连接池）、`github.com/google/uuid`
- 待新增：`entgo.io/ent`（及生成所需工具）
- repository 目录分布在 `server/internal/{tenant,domain,service,instance,node,deployment,traffic,ratelimit,canary,bluegreen,settings,system,dashboard,health,...}`

---

## 四、开发阶段划分

```text
S1  Ent 基座：依赖 + Schema 骨架 + 迁移策略对齐
S2  核心配置包迁移：tenant / domain / service / instance（Phase 1 主链路配置）
S3  发布与编排包迁移：deployment / node / traffic / canary / bluegreen / settings / ratelimit
S4  聚合与审计查询：dashboard / audit / logs 复杂查询迁移
S5  收尾验证：全量回归、性能复测、文档与目录结构更新
```

| 阶段 | 内容 | 涉及模块 | 预期工时 |
|------|------|----------|---------|
| **S1 Ent 基座** | 引入 ent、Schema 骨架（先 2-3 张表试点）、migrate 策略、go generate 流水线 | pkg + ent schema | 2-3 天 |
| **S2 核心配置包迁移** | tenant/domain/service/instance repository 切换 Ent，保留签名与错误语义 | tenant/domain/service/instance | 3-4 天 |
| **S3 发布编排包迁移** | deployment/node/traffic/canary/bluegreen/settings/ratelimit 等仓库切换 | 各业务包 | 4-5 天 |
| **S4 聚合与审计查询** | dashboard/audit/logs 复杂查询：Ent 原生 SQL 或聚合迁移，性能对比 | dashboard + api/audit + logs | 2-3 天 |
| **S5 收尾验证** | go test 全绿、端到端回归、性能复测、文档更新（技术栈/目录/接口） | 全项目 + docs | 2-3 天 |

> **合计约 13-18 天（单人开发）**。S2/S3 是主体，各包彼此独立、可增量落地与单独验证。

---

## 五、Ent Schema 建模（S1）

### 5.1 建模总览

```text
Phase 1/2 存量表                              Ent Schema 映射
tenants / domains / services / instances        ent/schema/{tenant,domain,service,instance}.go
health_checks / admins / audit_logs             ent/schema/{healthcheck,admin,auditlog}.go
nodes / deployments / deployment_versions       ent/schema/{node,deployment,deploymentversion}.go
traffic_policies / rate_limits                  ent/schema/{trafficpolicy,ratelimit}.go
canary_releases / canary_events                 ent/schema/{canaryrelease,canaryevent}.go
bluegreen_deployments / bluegreen_events        ent/schema/{bluegreendeployment,bluegreenevent}.go
settings                                        ent/schema/setting.go
```

### 5.2 Schema 字段约定

- 每张表 `Field` 与存量 SQL 列一一对应（类型、nullable、default 对齐）
- 唯一约束：`domains.hostname`、`services(tenant_id,name)`、`admins.email`、`deployment_versions` 相关组合等，用 `Unique()` / `Indexes()` 表达
- 索引：`instances(service_id)`、`instances(health)`、`domains(hostname)`、审计 / 日志的 action/target_type 过滤等沿用
- 外键：Domain→Tenant、Service→Tenant、Instance→Service/Node、DeploymentVersion→Deployment 等关系用 `Edges` 表达，删除策略（级联 / 拒绝）与 0001–0008 一致
- 时间字段：沿用 `timestamp` 语义，`UpdatedAt` 走 Ent `UpdateTime()` 维护

### 5.3 迁移对齐策略（需在 S1 定稿）

| 方案 | 说明 | 取舍 |
|------|------|------|
| **A 并存** | 保留自研 runner 0001–0008 管存量结构；Ent 仅以 Schema 建模 + 生成代码，不负责建表 | 迁移风险最低，但结构仍以 SQL 为事实来源，与"Schema 单一来源"目标有差距 |
| **B 移交** | 以 Ent migrate 为权威：先 `ent migrate diff` 校验与现状一致，再在干净库/新库用 Ent 建表；存量库经校验后切换 | 达成单一事实来源，但需强校验防漂移，工作量最大 |

> 推荐 **B 移交 + 校验门禁**：Ent Schema 为唯一事实来源，`go generate` 生成迁移 diff，CI 中对旧库执行 diff 校验（无变更才通过），保证 Schema 与存量 0001–0008 产物等价后切换。0001–0008 保留为历史只读，不再追加。

### 5.4 验证

- [ ] `go generate ./...` 成功生成 ent 客户端与 migrate 产物
- [ ] 对新库：Ent migrate 建出的表结构与 0001–0008 逐列 diff 一致
- [ ] 对存量库（已跑 0001–0008）：migrate diff 校验通过、无 schema 漂移告警
- [ ] 试点 2-3 张表（如 tenants/domains/services）完成基本 CRUD 回归

---

## 六、S2 — 核心配置包迁移

### 6.1 模块与文件

| 文件（现状） | 目标 |
|------|------|
| `internal/tenant/model.go` + `repository.go` | 改为基于 ent client；保留 `New` / `GetByID` / `List` / `Update` / `SetStatus` 等对外方法 |
| `internal/domain/model.go` + `repository.go` | 同上 |
| `internal/service/model.go` + `repository.go` | 同上 |
| `internal/instance/repository.go` | 迁移，保留 SSRF 地址校验在 Create/Update 的既有位置与语义 |

### 6.2 迁移约定

- Repository 仍持连接：`pool *pgxpool.Pool` 或直接持 `*ent.Client`（由 `ent.NewClient` 基于既有池构造，二选一并全项目统一）
- 对外方法签名不变；`NotFound` / `Conflict` / 唯一冲突的错误语义继续通过 `pkg.ErrNotFound` / `pkg.ErrConflict` 返回，service/handler 不感知底层切换
- 手写 `cols` 常量 / `scanXxx` / `row.Scan` 删除，改为 `client.Tenant.Query()` / `Create()` 等生成 API

### 6.3 验证

- [ ] tenant / domain / service / instance 的 CRUD 单测（用现有或新增 repository 测试）全绿
- [ ] SSRF 校验行为未变（恶意内网地址创建 / 更新被拒）
- [ ] 通过 Management API 冒烟：建租户 → 绑域名 → 建服务 → 注册实例 → 路由命中

---

## 七、S3 — 发布与编排包迁移

### 7.1 包清单与特有约束

| 包 | 迁移注意 |
|------|------|
| `node` | 心跳 / 状态更新为高频写，验证批量更新路径 |
| `deployment` | Version 生命周期（stable/canary/active/standby/draining）状态机字段迁移 |
| `traffic` | 权重 / 匹配条件列较多，验证 JSON / 多列条件读写一致性 |
| `ratelimit` | 各维度限流配置读取 |
| `canary` | `canary_releases` + `canary_events` 关联，事务内推进 / promote / rollback |
| `bluegreen` | `bluegreen_deployments` + `bluegreen_events` 关联，事务与回滚 |
| `settings` | 动态配置读写；与现有内存 Reload 流程对接不变 |

### 7.2 事务边界

- 现状依赖事务的路径（canary 推进、bluegreen 切换、发布动作 + audit 写）改用 `ent.Client.Tx()`，保持原子性语义一致
- 审计写入继续与业务操作同事务或在既有提交点写入，行为不得改变

### 7.3 验证

- [ ] 各包单测全绿（node 心跳、deployment 版本状态机、canary/bluegreen 事件、settings Reload）
- [ ] 事务性发布动作在失败路径可回滚，无部分写
- [ ] Management API 冒烟：发布部署 → 建版本 → 配流量策略 → canary 推进 → promote / rollback → 蓝绿切换
- [ ] 审计日志在发布动作后正确落库

---

## 八、S4 — 聚合与审计查询迁移

### 8.1 需要迁移的复杂查询

| 场景 | 现状 | 迁移策略 |
|------|------|----------|
| dashboard `/overview` | 手工聚合 + window function + instance FILTER 聚合 | 优先用 Ent 聚合 / 原生 SQL（`sql/schema` 暴露），以**性能等价**为验收 |
| audit 列表 | `action LIKE` / target_type / time 过滤 + 分页 | 用 Ent 谓词迁移，维持分页与过滤语义 |
| access / error logs | ring buffer 内存查询（非 DB） | **不受影响**，确认无 DB 依赖即可 |

### 8.2 性能门槛

- dashboard / audit 查询在迁移前后跑基准对比，P95 / 扫描行数不得劣化
- 仍允许对报告型查询使用 Ent 暴露的原生 SQL，不强行套用 ORM 抽象

### 8.3 验证

- [ ] dashboard `/overview` 各指标数值与迁移前一致
- [ ] audit 过滤 / 分页行为等价
- [ ] 性能对比通过（无劣化）

---

## 九、技术依赖与配置清单

### 新增依赖

```text
entgo.io/ent                     # Ent ORM
entgo.io/ent/dialect             # 配合 pgx dialect / sql
entgo.io/ent/dialect/sql         # 原生 SQL 能力
entgo.io/ent/dialect/sql/schema  # migrate diff 产物
```

### 环境变量 / 配置

- 数据库连接仍走 `MAPLE_DATABASE_URL`，Ent 基于既有 pgx 池构造客户端，**不新增连接配置**
- 迁移开关可按需：`MAPLE_DB_MIGRATE_MODE=ent|legacy`（默认进入校验门禁）

### 目录结构演进（后续更新 `项目目录结构.md`）

```text
server/
├── ent/                         # Ent 生成代码（schema → 客户端，可 regenerate）
│   ├── schema/                  # 手写 Ent Schema（唯一事实来源）
│   ├── migrate/                 # Ent 迁移产物
│   ├── *.go                     # 生成代码（ent client / predicates / hooks）
│   └── generate.go
├── internal/{业务包}/repository.go   # 基于 ent client，对外签名不变
└── migrations/                  # 0001–0008 保留为历史只读
```

---

## 十、关键设计决策

### 10.1 Schema 单一事实来源

Ent Schema 取代手写建表 SQL 成为结构唯一来源；0001–0008 冻结为历史。任何表结构变更改 Schema → generate → diff，不再直接编辑 SQL 迁移。

### 10.2 迁移双轨收敛到 Ent

从"自研 runner + 0001–0008"收敛为"Ent migrate 权威 + 校验门禁"：CI 对旧库跑 diff，无漂移才通过；新库直接 Ent 建表。存量库在线切换需先备份并在低峰执行。

### 10.3 Repository 对外契约不变

切换以"service/handler 零改动"为验收目标——错误语义、事务边界、SSRF 校验位置都必须在 Ent 版本里原样保留。这是把本次改造控制为"纯内部重构"的关键。

### 10.4 聚合查询允许原生 SQL

dashboard / 报告型查询是性能敏感点，允许用 Ent 暴露的 dialect/sql 写原生查询，不为了"纯 ORM"牺牲执行计划可控性。

### 10.5 增量落地与回滚

每包独立 PR、独立可验证；若某包出现无法收敛的性能 / 语义偏差，可单独回退该包为 pgx 手写 SQL，不影响其余包。

---

## 十一、验收 / 交付标准

### 功能交付

1. 全部 17 张存量表有对应 Ent Schema，`go generate` 一键生成客户端与迁移 diff。
2. 迁移策略定稿并执行：新库由 Ent 建表，存量库经 diff 校验后切换，0001–0008 冻结为历史。
3. 15 个业务包 repository 基于 ent client，对外方法签名与错误语义与切换前一致。
4. 数据面主链路未受改造影响（仍读内存 Route Cache，请求不查库）。
5. dashboard / audit 等复杂查询结果等价且性能不劣化。
6. `go vet ./...` 通过；`go test ./...` 全绿；关键路径端到端演练通过（建租户 → 发布 → canary → 回滚 → 审计）。

### 质量要求

1. service/handler 层对本次切换**零改动**（或仅构造注入处）。
2. 迁移门禁：Schema 与存量结构 diff 校验纳入 CI，防漂移。
3. 事务性发布动作原子性保持，失败路径无部分写。
4. 删除 / 更新级联策略与现状一致。
5. 文档联动更新：`技术栈选型与开发准备清单.md` 数据访问行落定为 Ent 已采用；`项目目录结构.md` 增补 `ent/`；`项目功能全面分析.md` / `API接口全面分析.md` 如有数据访问表述同步。

### 非交付（后续阶段）

- Gateway 多实例 HA / 配置同步 / Leader / 自动故障转移
- 自动 Canary（指标驱动推进与自动回滚）
- OpenTelemetry / 分布式追踪 / Request ID 体系
- Consistent Hash / Least Connections、多区域路由、完整 RBAC / Cloudflare for SaaS 自动化、Podman 兼容、插件机制

---

## 十二、后续衔接（预告）

本专项完成后，控制面获得统一的 Ent 数据层。后续 Phase 3 能力按序叠加：

```text
统一 Ent 数据层（本专项交付）
   ↓
Gateway 多实例 HA（配置同步 + 无状态扩展，依赖可迁移的统一状态模型）
   ↓
自动 Canary（指标驱动推进 + 异常自动回滚）
   ↓
OpenTelemetry 追踪 / Consistent Hash / Least Connections
   ↓
多区域路由 + 完整 RBAC + Cloudflare for SaaS 自动化 + Podman 兼容
```

Ent 生成的类型安全查询与统一迁移能力，正是上述分布式 / 自动化能力的"地基"——这也是把 Ent 改造放到本阶段最前的原因。
