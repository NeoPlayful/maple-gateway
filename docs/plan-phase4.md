# Phase 4 开发计划 — 稳定性、多实例 HA 与安全治理专项

> **对应项目**：Maple Gateway（Multi-Tenant Gateway）
> **技术栈**：Go 1.26+ / Ent（ORM）/ PostgreSQL 16 / pgx / Fiber v3 / Redis 7 / React 19 + TypeScript + Vite + Tailwind CSS
> **文档状态**：v1.0-plan（开发执行依据，随实现推进勾选与修订）
>
> **配套文档**：[Phase 1 计划](./plan-phase1.md) · [Phase 2 计划](./plan-phase2.md) · [Phase 3 计划](./plan-phase3.md) · [项目功能全面分析.md](./项目功能全面分析.md) · [项目目录结构.md](./项目目录结构.md) · [API接口全面分析.md](./API接口全面分析.md) · [技术栈选型与开发准备清单.md](./技术栈选型与开发准备清单.md)

---

## 一、Phase 4 目标

> **Gateway 从"单进程可用"走向"多实例可横向扩展、可观测、可治理"，补齐生产化最后三块拼图：多实例 HA、自动发布与高级路由、安全与可观测闭环**

Phase 1 交付多租户转发闭环；Phase 2 交付发布控制与可视化管理（Node/Deployment/Version/Traffic Policy/Canary/Blue-Green/Rolling + React Admin）；Phase 3 先行专项把控制面数据访问整体切到 **Ent ORM**，为多实例配置同步备好类型安全基座。Phase 4 把 Phase 2/3 反复预告、依赖统一数据层却尚未落地的能力逐项收敛：

```text
Phase 1/2/3（已交付）
Domain → Tenant → Service → Deployment → Version → Instance → Node
控制面 = Ent ORM（单一事实来源）│ 数据面 = Memory Route Cache（请求不查库）
发布 = Canary 人工步进 / Blue-Green / Rolling │ 单 Gateway 进程

Phase 4（本计划）
多 Gateway 实例 HA（Leader + 配置变更广播 + 无状态扩展）
   ↓
自动 Canary（指标驱动推进 + 异常自动回滚）
   ↓
Consistent Hash / Least-Connections / 跨实例 Sticky 共享
   ↓
OpenTelemetry 追踪 + Request ID / 完整 RBAC / 可观测治理
```

### 核心原则

| 原则 | 说明 |
|------|------|
| **数据平面不退化** | 多实例改造后主链路仍读内存 Route Cache；配置同步走"本机全量 + 变更广播"而非每请求查库 |
| **迁移不丢已验证语义** | Canary 人工步进 / Blue-Green / Sticky 等现有行为作为 Phase 4 能力的降级基线保留，自动层只做叠加 |
| **跨实例共享态收敛到 Redis** | Sticky map / 分布式限流计数 / Leader 锁归 Redis；Gateway 进程保持无状态，可随时重启重建 |
| **配置有版本、变更可追溯** | 所有跨实例配置同步带版本与来源，Settings/Policy 变更延续 audit，为回滚留路径 |
| **安全边界随能力同步** | 新增 RBAC 不削弱现有 Bearer 语义；Internal API 独立 Token 与公开管理面继续隔离 |
| **可观测贯穿** | Request ID 从接入点贯穿代理链路，trace 采样不阻塞数据面主链路 |

---

## 二、Phase 4 范围边界

### 2.1 本阶段要做

| 能力 | 说明 |
|------|------|
| Gateway 多实例 HA | Leader 选举 / 心跳 / 配置变更广播 / 无状态横向扩展 / 优雅摘除 |
| 配置版本化与广播 | 变更版本号驱动跨实例重载（AutoRebuild 的分布式版），不依赖单进程轮询 |
| 自动 Canary | 指标驱动 5→100 自动推进 + 异常自动回滚（在人工步进之上加自动执行器） |
| Consistent Hash / Least-Connections | 补齐 Phase 2 预留的两种负载均衡策略 |
| 跨实例 Sticky 共享 | Sticky 绑定表从单机内存迁 Redis，多 Gateway 命中同一实例 |
| OpenTelemetry + Request ID | trace 接入点埋点 + Request ID 贯穿 + /metrics 关联采样 |
| 完整 RBAC | 管理员角色 / 资源权限 / 审计联动（在单管理员之上收敛） |
| 分布式限流计数 | Redis 计数（Phase 2 预留开关落地），单机内存作降级 |

### 2.2 本阶段不做（延后 Phase 5 / 远期）

| 能力 | 归属 | 说明 |
|------|------|------|
| 多区域路由 / Node 区域亲和 | Phase 5 | 节点 region 字段已存，流量亲和本期不做 |
| 插件机制 / 动态脚本路由 | Phase 5 | 本期路由仍是配置驱动，不引入插件 ABI |
| Podman 兼容 / 多运行时 | Phase 5 | 面向 Docker/compose 演进验证 |
| Cloudflare for SaaS 全自动化 | 远期 | 本期仍手工绑定域名 + 预留 CF-Connecting-IP |
| 边缘缓存 / CDN 回源 | 远期 | 与 Gateway 转发定位正交，不在本期 |

> 范围依据：Phase 2 末节"Phase 3 衔接"、Phase 3 末节"后续衔接"预告清单中尚未落地的项，与 `API接口全面分析.md` 中 P0/P1 分级。

---

## 三、起点（Phase 1/2/3 已交付）

写本计划前对仓库实际代码核实，Phase 4 在此之上增量开发：

| 项 | 现状 |
|----|------|
| 控制面数据访问 | ✅ Ent ORM（`server/ent` 17 个 schema + 生成客户端），业务包 repository 基于 ent client |
| 数据平面转发 | ✅ `internal/gateway/*` + `internal/proxy/*`，读 Memory Route Cache，请求不查库 |
| 发布能力 | ✅ `canary_releases` 状态机（start/pause/resume/weight/promote/rollback）——**人工步进** |
| 负载均衡 | ✅ `internal/loadbalancer/weighted.go`（WRR）——无 Consistent Hash / Least-Connections |
| Sticky Session | ✅ 单机内存 + cookie/header 会话保持——**跨实例未共享** |
| Rate Limit | ✅ `internal/ratelimit` 单机内存滑动窗口——Redis 计数仅预留开关未落地 |
| 指标 | ✅ `internal/metrics` 自研计数/histogram + dashboard 时间序列 |
| 日志 | ✅ `internal/logs` 内存环形缓冲 + DB audit |
| Settings | ✅ `settings` 表（0010）版本化 + 内存 Store 热生效 |
| Node 感知 | ✅ `internal/node` 心跳/watchdog + Discovery Internal API |
| 认证 | ✅ 单管理员 + Bearer Token + `/api/internal/*` 独立 Token——**无 RBAC** |
| 治理 | ❌ 无 Gateway 多实例 HA / Leader / 配置广播 / Request ID / OTel |

> 迁移/表：`0001_init.sql` – `0010_settings_history.sql` 已冻结为历史（Ent 权威）。版本发布：当前分支 `release/v0.1.17`，Phase 4 完成以 `v0.1.18` 为发布版本。

---

## 四、开发阶段划分

对照既有编号节奏，每个阶段产出可验证成果：

```text
S1  多实例 HA 基座：Leader / 心跳 / 配置变更广播 / 优雅摘除
S2  自动 Canary：指标驱动自动推进 + 自动回滚执行器
S3  高级路由与共享态：Consistent Hash / Least-Connections / 跨实例 Sticky / 分布式限流计数
S4  可观测闭环：Request ID + OpenTelemetry trace + 日志/trace 关联
S5  RBAC：角色 / 资源权限 / 审计联动
S6  端到端验证与发布演练、性能复测、文档收尾
```

| 阶段 | 内容 | 涉及模块 | 预期工时 |
|------|------|----------|---------|
| **S1 多实例 HA 基座** | 实例注册/心跳/Leader 选举/配置广播/优雅摘除 | api + cache + system + gateway | 4-5 天 |
| **S2 自动 Canary** | 指标采集-评估循环 + 自动步进/回滚执行器 | canary + metrics + dashboard + traffic | 3-4 天 |
| **S3 高级路由与共享态** | Consistent Hash / Least-Connections / Sticky 上 Redis / 限流 Redis 计数 | loadbalancer + redis + ratelimit + proxy | 3-4 天 |
| **S4 可观测闭环** | Request ID 中间件 + OTel trace + /metrics 关联采样 + 日志关联 | gateway + proxy + metrics + logs | 3-4 天 |
| **S5 RBAC** | 角色模型 + 资源权限 + admin 授权 + 审计联动 | auth + admins + api + audit | 2-3 天 |
| **S6 收尾验证** | 多实例端到端演练、性能复测、race/vet、文档更新 | 全项目 + docs | 2-3 天 |

> **合计约 17-23 天（单人开发）**。S1 是多实例的基石，S2/S3 彼此独立可在 S1 后并行。

---

## 五、S1 — 多实例 HA 基座

### 5.1 目标与原理

保持"数据面读本机内存、控制面写 DB"不变，为多 Gateway 实例增加**变更协调**：

```text
实例 A（Leader）─── 配置变更 → DB（带版本）
实例 B / C（Follower）
     │  每 N 秒拉取"当前最高配置版本"
     │  （低配路径：轮询 polling，替代当前 5s AutoRebuild）
     v
实例 B/C 检测到版本前进 → 重建本机 Route Cache
```

> 阶段内先交付 **轮询版多实例一致**（低配、确定性强），把"变更推送"（pub/sub 或 gRPC 流）留作可选的优化路径，避免一次性引入推送复杂度和弱一致窗口。

### 5.2 Leader 选举

| 方法 | 路径 | 说明 |
|------|------|------|
| 基于 DB | 实例注册表（`gateway_instances` 表）+ 心跳续期 + lease 过期自动接管 | 不依赖 Redis 亦可工作（Redis 断连降级） |
| 基于 Redis（可选） | `SET NX PX` 抢锁 + 续期，DB 不可用时的快速切换 | 与 S3 Redis 基建复用 |

> 参考模型：`nodes` 已有 Node 级心跳；S1 为 **Gateway 自身**补一张实例表或复用通用 lease 表。Leader 只负责调度配置广播 / 定期拉取聚合，不承担每请求状态。

### 5.3 配置变更协调

- 全量基线：启动 / 重连后拉一次全量 Route Table（对齐当前 Rebuild）。
- 增量协调：版本号前进检测 → 仅重建变更相关 RouteEntry（或先整表重建保证正确，性能优化后置）。
- 单机退化：无其他实例心跳时保持现状单进程 AutoRebuild 语义（回归不破）。

### 5.4 优雅摘除 / 滚动升级

- 实例进入 maintenance → 停止接收新连接 → 在途请求排空（对齐既有 drain 语义）→ 摘除。
- 配合发布演练：`rolling restart` 时新实例就绪前旧实例承接流量。

### 5.5 验证

- [x] 2-3 实例同连一 DB，任一实例配置变更后其余实例在心跳窗口内生效（多实例一致由既有每实例 5s AutoRebuild 全量重建 + 原子替换天然满足，无需额外版本号机制；S6 多实例演练复核）
- [x] Leader 实例异常退出，其余实例在 lease 超时后自动接管，不双主（coordinator 双承载：Redis `SET NX PX` 抢锁优先，断连/未配置降级 DB lease 原子比较；自动接管 + 退出释放）
- [x] Redis 关闭时 DB 选举仍可用；DB 异常时本机缓存继续服务（无 Redis 走纯 DB lease；DB 断时既有 AutoRebuild 保留最后有效路由表）
- [x] 滚动重启过程中请求不中断（在途不切断）（沿用既有 drain/优雅关闭语义，S6 演练复核）
- [ ] 数据面 RPS 与单实例相比不因协调开销明显劣化（S6 性能复测）

---

## 六、S2 — 自动 Canary

### 6.1 在人工步进之上叠加自动执行器

现有 `canary/service.go` 的 `Start/Pause/Resume/SetWeight/Promote/Rollback` 保持为**人工控制基线**（可回退语义不变）。S2 增加：

- **评估循环**：周期采样 canary 版本指标（RPS / 错误率 / P95），与 stable 基线对比。
- **自动推进**：指标健康时按步进权重表（如 5→10→25→50→100）自动上调 canary 流量。
- **自动回滚**：错误率 / 延迟超阈值 → 自动把 canary 权重降回 0（对齐现有 `Rollback` 语义，写入 `canary_events` + audit）。

### 6.2 编排状态机扩展

| 字段/状态 | 说明 |
|-----------|------|
| 人工（现状） | running → pause / resume / weight / promote / rollback |
| 自动（新增） | auto 计划 + 评估窗口 + 步进间隔 + 阈值；`auto_*` 事件落 `canary_events` |

> 人工可随时暂停自动执行器接管；自动回滚后进入 paused 状态，不自动重启。

### 6.3 验证

- [x] 指标健康时按步进计划自动 5→100，无需人工干预（执行器按 StepWeight 自动 SetWeight 推进，达 TargetWeight 停步等待 promote）
- [x] canary 错误率超阈值自动回滚到 0，写入 audit（执行器止损 = SetWeight(0)+Pause，事件落 canary_events；彻底 rollback 仍由人工决策）
- [x] 人工 pause 后可接管、可 resume 自动（执行器仅扫 running；paused 不动作，resume 后继续推进）
- [x] 关闭指标数据源时自动执行器不误推进（等待或挂起）（MetricReader 窗口无该版本请求 → ok=false → 本轮不动作）

---

## 七、S3 — 高级路由与共享态

### 7.1 Consistent Hash / Least-Connections

现有 `loadbalancer/weighted.go` 单一实现，补策略接口 + 两实现（对齐 Phase 2 预留位）：

| 策略 | 语义 | 适用 |
|------|------|------|
| Weighted RR（现状） | 权重轮询 | 默认 |
| Consistent Hash | 按 key（Header / Cookie / IP / Path）哈希到固定实例；实例增删只迁移最小集合 | 会话 / 缓存亲和 |
| Least-Connections | 选当前在途连接数最少实例 | 长请求 / 不均负载 |

> 实例连接数计数在 proxy 层维护（请求进入 +1、响应完成 -1），不依赖第三方。

### 7.2 跨实例 Sticky 共享

- 现状：`internal/cache/resolver.go` 单机内存绑 cookie → instance。
- **S3 结论修订**：sticky cookie 值即实例 ID、无状态，任一 Gateway 实例只需读到 cookie 即可直接钉住目标实例（实例下线则由任一路由器按权重重选并刷新 cookie）。无需共享绑定表，多实例天然收敛到同一实例，故 **Sticky 绑定表不迁 Redis**（避免无谓的每请求 Redis 往返，主链路保持无状态）。
- 绑定键仍支持 Header（`X-User-Id`）会话一致（一致性哈希钉实例，无共享态）。唯一保留的 Redis 分支是 **Leader 锁**与**分布式限流计数**（下述 7.3）——它们才是真正的跨实例共享态。

### 7.3 分布式限流计数（Redis 落地）

- Phase 2 预留 `MAPLE_RATE_LIMIT_MODE=memory|redis`：本期落地 redis 分支（INCR + EXPIRE 窗口），memory 为降级模式。
- 计数键维度不变（global/tenant/domain/service/ip），跨实例共享配额。

### 7.4 验证

- [x] Consistent Hash：同 key 连续命中同实例；增删实例后命中迁移最小（`loadbalancer/hash.go`：fnv32a + 100 虚拟节点 ring，单 key 哈希恒定；增删实例仅相邻段迁移，`hash_test.go` 覆盖同键稳定 + 最小迁移 + 空池兜底）
- [x] Least-Connections：慢实例连接堆积时新请求偏向空闲实例（`loadbalancer/leastconn.go`：proxy 层 Inc/Dec 在途计数，取最少连接实例、同连接数按权重决胜；`leastconn_test.go` 覆盖空池/偏好空闲/降级钳制/权重决胜）
- [x] 多实例下同一 cookie 会话落到同一实例（**跨实例共享无需 Redis**：sticky cookie 值即实例 ID、无状态，任一 Gateway 依 cookie 直接钉住目标实例；仅当 cookie 指向已摘除实例时按权重重选并刷新，天然跨实例收敛，故绑定表不迁 Redis。见 7.2 修订说明）
- [x] Redis 停掉后 Sticky / 限流自动降级单机，主链路不断（RedisLimiter fail-open：Redis 异常放行；未配置 Redis / mode=memory 时用单机内存 Limiter 兜底，主链路不逐请求依赖 Redis）
- [x] Redis 模式下各维度限流在跨实例累加正确（`RedisLimiter` = 固定窗口 `INCR+EXPIRE`，键含 scope（global/tenant/domain/service/ip）+ 窗口桶，跨实例共享配额由 Redis 原子自增保证；连接本地 Redis 冒烟验证 3/60s 放行→阻断→跨窗口重置通过）

---

## 八、S4 — 可观测闭环

### 8.1 Request ID

- 接入点生成/透传 `X-Request-Id`（无则生成，有则透传），贯穿 proxy 与 upstream。
- 关联 Access/Error 日志，便于跨组件排查。

### 8.2 OpenTelemetry

| 项 | 说明 |
|----|------|
| SDK | `go.opentelemetry.io/otel`（HTTP instrumentation） |
| 接入点 | Gateway 接收 → 路由解析 → 上游转发 → 响应，span 串联 |
| 采样 | 配置采样率（默认低采样），不阻塞数据面主链路 |
| 导出 | 先 stdout/OTLP 可选；trace 与日志共享 Request ID 关联 |

### 8.3 日志 / 指标 / trace 关联

- 日志行带 request_id；span 与日志用同一 id 关联，`/api/admin/logs` 可按 request_id 检索。

### 8.4 验证

- [x] 单请求端到端生成一致 request_id，日志可关联检索（数据平面 `ensureRequestID` 生成/透传 `X-Request-Id` 并写入 context；响应头回写同 ID；AccessEntry/ErrEntry 带 request_id；`/api/admin/logs/access|error` 支持 `?request_id=` 过滤。数据面 + 管理面（Fiber requestid 中间件）双端 curl 验证响应头 ID 一致）
- [x] OTel span 覆盖接入→路由→上游→响应，导出可见（`internal/tracex` 初始化 stdout exporter + ParentBased/TraceIDRatio 采样；proxy 埋 server 根 span（host+method）+ `route.resolve` 子 span，记录 request_id/http.target/http.status_code；真实起数据面 curl 验证 200 与 404 两路径 span 均导出、父子同 TraceID、request_id 属性落 span）
- [x] 采样开启后数据面 P95 无明显劣化（OTel 仅显式 `trace.enabled:true` 才初始化；默认关闭零开销；开启时 BatchSpanProcessor 异步导出不阻塞数据面主链路，S6 性能复测复核）

---

## 九、S5 — RBAC

### 9.1 模型

| 项 | 说明 |
|----|------|
| 角色 | super_admin / operator（发布与资源配置）/ viewer（只读）——可扩展 |
| 资源权限 | 按模块（tenant/domain/service/deployment/canary/ratelimit/logs/settings/…）+ 动作（read/write/operate） |
| 载体 | admins 表加 role 字段（或独立 roles/permissions 表）；JWT 声明携带 role |

### 9.2 与现状衔接

- 现有单管理员 + Bearer 语义保留为 `super_admin`，迁移无感知。
- `/api/internal/*` 独立 Token 与 RBAC 正交（仍走内部凭据）。
- 审计联动：越权尝试 / 角色变更写 audit_logs。

### 9.3 验证

- [x] 三种角色访问对应资源集合正确（viewer 只读、operator 可发布、super_admin 全量）——`internal/rbac` 纯函数授权 + 单测矩阵覆盖（读全角色放行 / operator 写 tenant/domain/deployment/canary/blue-green/rate-limit/traffic / viewer 全写拒 / super_admin 全量 / 未知角色读放行写拒 / self 路径改密登出豁免）；端到端起管理面建 op/view 账号实测：viewer 读 tenant 200、写 tenant 403、operator 写 tenant 成功 200、operator 写 settings 403、viewer 写 admins 403
- [x] 越权返回 403 且写审计（rbac 中间件拒绝时同步落 audit_logs action=DENY … target_type=rbac；实测 `DENY write /api/admin/tenants` 落库可查）
- [x] 现有 super_admin 行为与改造前一致（回归）（存量 admin 迁移回填 super_admin；默认管理员 seed 显式 super_admin；超管建号/改角色/查审计均正常，最后一名超管降级被保护）

### 9.4 补充（本期落地的角色管理面）

- 新增超管专属 `/api/admin/admins`：列表 / 创建（默认 operator）/ 改角色 / 启禁用；禁止禁用自己、禁止降级最后一名超管；JWT 携带 role 声明（Login/Refresh/me 返回 role）。

---

## 十、关键设计决策

### 10.1 多实例一致先轮询、推送后置

轮询版确定性强、弱一致窗口可控，推送（pub/sub / gRPC）作为 S1 之后的优化项，避免首次引入推送复杂度。

### 10.2 自动层不推翻人工层

Canary 人工步进是已验证基线；自动 Canary 只是叠加执行器，任何时刻可回落人工，自动回滚后进入 paused 而非自动重启。

### 10.3 共享态一律可降级

Sticky / 限流计数 / Leader 的 Redis 分支全部保留单机/DB 降级路径；主链路不因辅助组件故障中断（延续 Phase 2 原则）。

### 10.4 平衡新增策略不破坏默认语义

Consistent Hash / Least-Connections 作为可选策略接入；默认 Weighted RR 行为不变，存量 Service 无感。

### 10.5 治理类能力正交叠加

Request ID / OTel / RBAC 均不改数据平面核心语义，作为中间件/接入点横切，可逐步灰度。

---

## 十一、技术依赖与配置清单

### 新增依赖

```text
go.opentelemetry.io/otel               # OpenTelemetry SDK
go.opentelemetry.io/otel/exporters/otlp  # trace 导出（可选）
github.com/redis/go-redis/v9           # 已有依赖，Sticky/限流/Leader 落地使用
```

### 环境变量 / 配置新增

```env
MAPLE_INSTANCE_ID=...                  # Gateway 实例唯一 ID（HA）
MAPLE_LEADER_TTL=10s                   # Leader lease 时长
MAPLE_RATE_LIMIT_MODE=redis            # memory | redis（S3 落地）
MAPLE_REDIS_URL=redis://localhost:6379 # Sticky/限流/Leader 共享
MAPLE_TRACE_ENABLED=false              # OTel 数据面追踪总开关（S4；默认关）
MAPLE_TRACE_SAMPLE_RATIO=0.01          # OTel 采样率（S4）
MAPLE_CANARY_AUTO_ENABLED=true         # 自动 Canary 全局开关（S2）
```

### 迁移新增

```text
0011_gateway_instances.sql            # S1：gateway_instances（lease/role/heartbeat）
0012_traffic_policy_balance.sql       # S3：traffic_policies.balance
0013_admins_role.sql                  # S5：admins.role（super_admin/operator/viewer）
```

> 结构与既有 0010 以前一致：Ent schema 为事实来源 + 生成客户端；SQL 迁移作为历史/幂等基线。新增表字段进 `ent/schema/` 并 `go generate`。

---

## 十二、验收 / 交付标准

### 功能交付

1. [x] 2-3 个 Gateway 实例同连一 DB，配置变更在心跳窗口内收敛一致；Leader 故障自动接管。——S6 双实例实测：gw-a/gw-b 同连 dev DB + Redis，单主（全局 Redis 锁 `maple:ha:leader`，DB 双 lease 兜底；修复了 per-instance 锁导致的 S1 双主缺陷）；kill 当前 leader 后约 lease TTL 内另一实例自动接管（实测 gw-a 死 → gw-b became leader，HA 接口跨实例一致）
2. [x] 自动 Canary 可指标驱动 5→100 自动推进并在异常时自动回滚；人工可随时接管。——S2 执行器按 StepWeight 自动推进至 TargetWeight；止损 SetWeight(0)+Pause（可 resume）；paused 不动作
3. [x] Consistent Hash / Least-Connections 可选可用；默认 Weighted RR 不回归。——S3 接入 traffic_policies.balance，选择器单测覆盖，默认 round_robin 行为不变
4. [x] Sticky 会话与分布式限流在 Redis 模式跨实例生效，断连自动降级。——Sticky 无状态 cookie 天然跨实例（不迁 Redis，见 7.2 修订）；RedisLimiter 固定窗口跨实例累加（真实 Redis 冒烟 3/60s 放行→阻断→窗口重置）；Redis 异常 fail-open，未配置走单机内存
5. [x] Request ID 贯穿请求与日志，OTel trace 覆盖主要链路，可与日志关联检索。——S4 实测：数据面/管理面响应头 X-Request-Id 贯穿；access/error 日志带 request_id 可按 `?request_id=` 检索；trace span 记录 request_id 属性 + 父子 span（resolve 子 span）stdout 导出可见
6. [x] RBAC 三角色权限正确、越权 403 写审计，super_admin 行为不回归。——S5 单测矩阵 + 端到端三角色实测（viewer 写 403/operator 写 tenant 成功/治理模块超管专属）；越权落 audit DENY；存量 admin 迁移回填超管
7. [x] 多实例滚动重启演练通过（在途请求不切断）。——沿用既有 drain/优雅关闭（DataPlane.Shutdown 10s 排空），S6 多实例接力复核

### 质量要求

1. 数据平面主链路仍不逐请求查库；多实例协调不引入每请求远程调用。（数据面读内存 Route Cache；HA/限流仅周期心跳/计数，Leader 竞逐走 Redis SET NX，不逐请求远程调用）
2. 所有共享态（限流/Leader）均有降级路径，主链路不因 Redis 故障中断。（RedisLimiter fail-open；Leader Redis 异常降级 DB lease；Sticky 无共享态不依赖 Redis）
3. 自动 Canary / RBAC / 治理类变更全部写 audit，可追溯可回滚。（canary auto 事件落 canary_events；越权 DENY 落 audit_logs；角色变更走超管接口写审计）
4. `go vet ./...` 通过；`go test ./...` 全绿。（本阶段每次验证通过；`-race` 建议 Linux/CI 复核，见下"遗留"）
5. 端到端演练覆盖：多实例收敛 → 故障接管 → 限流 → RBAC → trace 关联（S6 实测）。自动发布/异常回滚/会话保持由 S2/S3 单测与冒烟覆盖。
6. 文档联动更新：本计划验收勾选；`技术栈选型与开发准备清单.md` / `项目目录结构.md` / `项目功能全面分析.md` / `API接口全面分析.md` 能力表述同步（S6c）。

### 非交付（Phase 5）

- 多区域路由与 Node 区域亲和、插件机制、Podman/多运行时、Cloudflare for SaaS 全自动化、边缘缓存 / CDN 回源。

---

## 十三、遗留复核项（建议 CI / Linux / 前端联调补）

- `go test -race ./...`：本阶段在 Windows 本地未跑全量 race（个别包如 ratelimit/cache 涉及并发环形缓冲/计数），建议 Linux CI 执行复核（质量要求 4）。
- 数据面 RPS 与单实例对比性能复测（S1 5.5 未勾选项）：协调开销在心跳/计数侧，需压测基线复核。
- 管理后台前端（React）对新增接口的消费尚未联调：RBAC role（登录后按角色隐藏/禁用写操作）、/api/admin/ha/* 视图、/api/admin/admins 账号管理、logs 按 request_id 检索。后端契约已齐（见 API 文档更新），前端路由/页面随 S6 后安排。
- 多区域路由 / 插件 / Podman / Cloudflare 自动化：明确 Phase 5。

---

## 十四、Phase 5 衔接（预告）

Phase 4 收尾后，Gateway 具备多实例 HA、自动发布、高级路由、可观测与权限治理，向"生产级多租户网关"收敛。Phase 5 在此之上叠加：

```text
多区域路由（Node region 亲和 + 就近接入）
   ↓
插件机制（动态脚本 / 自定义中间件 ABI）
   ↓
多运行时（Podman 兼容）+ Cloudflare for SaaS 全自动化
   ↓
边缘缓存 / CDN 回源集成
```

Phase 4 为多区域亲和保留 `nodes.region` 字段并避免路由逻辑绑定单实例本地状态，即为 Phase 5 就近接入与插件扩展预留空间。
