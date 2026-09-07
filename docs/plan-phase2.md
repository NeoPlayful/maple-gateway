# Phase 2 开发计划 — 发布控制与可视化管理

> **对应项目**：Maple Gateway（Multi-Tenant Gateway）
> **技术栈**：Go 1.24+ / net/http Reverse Proxy / Fiber v3（Management API）/ PostgreSQL 16 / Redis 7 / React 19 + TypeScript + Vite + Tailwind CSS
> **文档状态**：v1.0-plan（开发执行依据，随实现推进勾选与修订）
>
> **配套文档**：[Phase 1 计划](./plan-phase1.md) · [项目功能全面分析.md](./项目功能全面分析.md) · [项目目录结构.md](./项目目录结构.md) · [API接口全面分析.md](./API接口全面分析.md) · [技术栈选型与开发准备清单.md](./技术栈选型与开发准备清单.md)

---

## 一、Phase 2 目标

> **多 Node 可感知 → 版本可区分 → 按策略分流 → 金丝雀 / 蓝绿 / 滚动可编排 → 请求可控流 → 有可视化后台**

Phase 1 完成了 **Multi-Tenant Gateway 核心闭环**（Host → Tenant → Service → Healthy Instance → Proxy，含 LB / Health / Failover / AutoRebuild / Management API）。Phase 2 把"单 Service 直接挂一组 Instance"的模型升级为"**Service → Deployment → Version → Instance**"，叠加 **Traffic Policy / Canary / Blue-Green / Rolling / Sticky** 等发布控制能力，引入 **Node 感知与 Container Manager 状态同步**，并补齐 **Rate Limit / Metrics / Logs / Settings**，最后交付 **React 管理后台**。

```text
Phase 1（已交付）
Host → Tenant → Service → Healthy Instance → Proxy

Phase 2（本计划）
Host → Tenant → Service ──→ Deployment ──→ Version(stable/canary) ──→ Instance ──→ Node
                             ↓ 叠加
             Traffic Policy + Canary + Blue/Green + Rolling Update + Sticky
                             ↓ 叠加
             Rate Limit + Metrics + Logs + Settings + Admin Dashboard
```

### 核心原则

| 原则 | 说明 |
|------|------|
| **P1 优先** | 先做模型演进与流量编排内核（Deployment / Version / Traffic Policy / Canary），再做指标与前端 |
| **Data Plane 无感升级** | 数据平面改造（Sticky / Rate Limit / 指标埋点）不允许破坏 Phase 1 已验证的转发能力 |
| **请求不查库** | 延续：主链路仍读 Memory Route Cache；策略 / 限流配置缓存化后参与路由 |
| **Gateway 只管流量** | Node / Container 生命周期归 Container Manager，Gateway 只接收状态并路由，不调用 Docker Engine |
| **前端只做管理** | React 只调用 `/api/admin/*`，不参与 Data Plane |
| **Redis 按需引入** | 仅 Sticky 共享态 / 分布式限流计数器场景启用，主链路不强依赖 |
| **向后兼容** | Phase 1 直接挂 Service 的 Instance 保留可迁移路径，升级过程不丢存量配置 |
| **内存为主、表复制为工具** | 全文用 Markdown 表格描述模型与接口，不用额外工具脚本 |

---

## 二、Phase 2 范围边界

### 2.1 本阶段要做

| 能力 | 说明 |
|------|------|
| 模型演进 | 引入 `nodes` / `deployments` / `deployment_versions`；`instances` 挂上 deployment_id / version_id / node_id |
| Node 感知 | Node 注册 / 心跳 / 状态缓存；Node offline 停止分配其上实例；维护状态 |
| 版本管理 | Version 的 stable / canary / active / standby / draining 生命周期 |
| Traffic Policy | 版本权重、Instance 权重、Header / Cookie / Path 匹配、百分比路由、Sticky、策略优先级 |
| Canary | 基于 Traffic Policy 的百分比分流；start / pause / resume / weight / promote / rollback |
| Blue / Green | 双版本整组切换 + 快速回滚 |
| Rolling Update | 配合 Container Manager 的流量接入 / 摘除 + Connection Draining |
| Sticky Session | Cookie / Header 会话保持 |
| Rate Limit | Global / Tenant / Domain / Service / IP 限流；Redis 计数（可选）、429 自定义 |
| Metrics | Prometheus /metrics + 管理端指标查询（RPS / 延迟 / 错误 / 实例健康） |
| Logs | Access / Error / Routing / Health / Security / Audit 分类查询接口 |
| Settings | 动态配置修改 → 校验 → 保存 → Runtime 更新（不重启） |
| Internal API | Container Manager / Node Agent 状态同步接口（register / heartbeat / sync） |
| Admin Dashboard | React 管理后台（登录 + Dashboard + 资源页 + 发布控制页） |
| TLS 数据平面 | HTTPS 监听、证书配置（列入 S1，按需启用） |

### 2.2 本阶段不做（延后 Phase 3）

| 能力 | 归属阶段 | 说明 |
|------|----------|------|
| Gateway 多实例 HA | Phase 3 | Gateway 实例间配置同步、Leader、滚动升级 |
| OpenTelemetry / 分布式追踪 | Phase 3 | 本阶段先用 Prometheus 指标 + Request ID |
| Consistent Hash / Least Connections | Phase 3 | 本阶段保留策略位，不实现 |
| 自动 Canary（指标驱动自动推进 / 自动回滚） | Phase 3 | 本阶段 Canary 为人工操作步进 |
| 多区域路由 | Phase 3 | Node 区域感知仅存字段，不做流量亲和 |
| Podman 兼容 | Phase 3 | 本阶段面向 Docker / compose 测试 |
| 完整 RBAC / 子账号 / Cloudflare for SaaS 自动化 | Phase 3 | 本阶段沿用管理员账号 + Bearer |

> 范围依据：`API接口全面分析.md` 第二十九节（接口优先级 P1 为 Phase 2 主范围）与
> `项目功能全面分析.md` 第二十七节（Phase 2 清单）。

---

## 三、起点（Phase 1 已交付）

写本计划前对仓库实际代码核实，Phase 2 在此之上增量开发：

| 项 | 状态 |
|----|------|
| 数据平面 HTTP 转发（net/http ReverseProxy） | ✅ `internal/proxy/*` + `internal/gateway/*` |
| DB 动态路由 + 5s AutoRebuild（无需重启） | ✅ `internal/cache/{route,auto,resolver}.go` |
| 路由状态拒绝（租户禁用 → 503、非法 Host → 400） | ✅ `internal/router/*` + `internal/proxy/proxy.go` |
| Smooth Weighted RR（含权重变化重置） | ✅ `internal/loadbalancer/weighted.go` |
| 主动健康检查（阈值 + 宽限 + 摘除/恢复） | ✅ `internal/health/checker.go` + AutoRebuild |
| Management API（Tenant/Domain/Service/Instance CRUD + Auth + 审计） | ✅ `internal/api/app.go` + 各模块 handler/repository |
| Instance 预留 version / node_id 字段 | ✅ `instances.version` / `instances.node_id` |
| Docker / Linux 部署 + 迁移器 + seed | ✅ 自研 migrate、`deploy/*`、docker-compose |
| 数据库迁移 | ✅ `0001_init.sql` / `0002` / `0003`（下一步将是 `0004_*`） |

> 尚未存在：`nodes` / `deployments` / `deployment_versions` 表，Traffic Policy、Canary、
> Rate Limit、Metrics、Logs 查询、Internal API、HTTPS 监听、前端代码。这些是本计划全部增量。

---

## 四、Phase 2 开发阶段划分

对照 Phase 1（S1-S8）延续同一编号节奏，每个阶段产出可验证成果：

```text
S1  模型演进：nodes / deployments / deployment_versions + instances 改造 + TLS 数据平面
S2  Traffic Policy 内核 + 数据平面策略分流
S3  Node 感知 + Container Manager Internal API + Sticky Session
S4  Canary / Blue-Green / Rolling Update 流量编排
S5  Rate Limit（Global/Tenant/Domain/Service/IP）
S6  Metrics + Logs + Settings 可观测与控制
S7  Admin Dashboard（React 前端）
S8  端到端验证与发布演练、性能复测、收尾部署
```

| 阶段 | 内容 | 涉及模块 | 预期工时 |
|------|------|----------|---------|
| **S1 模型演进与数据平面 TLS** | nodes / deployments / versions 建表 + instances 挂载 + 存量数据兼容 + HTTPS 监听 | migration + deployment/node/instance + gateway | 3-4 天 |
| **S2 流量策略内核** | TrafficPolicy 模型 + 路由表携带版本/策略 + 数据平面分流选择器 | traffic + cache + router + loadbalancer | 3-4 天 |
| **S3 Node 感知与 Sticky** | Node 注册/心跳/状态 + Internal API + 状态同步 + Sticky Session | node + discovery + cache + loadbalancer/sticky | 3-4 天 |
| **S4 发布编排** | Canary / Blue-Green / Rolling Update 流量状态机与控制接口 | canary + deployment + traffic + failover | 4-5 天 |
| **S5 限流** | RateLimit 模型 + 缓存化 + 各维度限流器 + 429 | ratelimit + cache + proxy | 2-3 天 |
| **S6 可观测与控制** | Prometheus metrics + 日志查询 + 动态 Settings | metrics + logging + system + proxy | 3-4 天 |
| **S7 Admin Dashboard** | React 工程 + 登录 + 布局 + 资源页 + 发布控制页 + 图表 | frontend/ | 5-8 天 |
| **S8 收尾验证** | 端到端发布演练、性能复测、race 测试、部署收尾 | 全项目 | 2-3 天 |

> **合计约 25-35 天（单人开发）**。S7 前端体量最大，可穿插在 S5/S6 之后并行启动工程骨架。

---

## 五、数据模型演进（S1）

### 5.1 模型总览（在 Phase 1 基础上新增）

```text
Phase 1                               Phase 2 新增
tenants                               nodes
domains                               deployments
services                              deployment_versions
instances（升级：挂 deployment/version/node）
health_checks
admins / audit_logs
```

核心关系扩展为：

```text
Domain → Tenant → Service → Deployment → Version → Instance → Node
```

### 5.2 新增表

#### nodes

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID | 主键 |
| name | String | 节点名（唯一），如 node-01 |
| host | String | 节点地址（管理面） |
| region | String? | 区域（Phase 3 亲和预留） |
| labels | Json? | 节点标签 |
| status | Enum | online / offline / maintenance / disabled |
| weight | Int | 节点权重（预留） |
| last_seen_at | Timestamptz? | 最近心跳 |
| created_at / updated_at | | |

#### deployments

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID | 主键 |
| service_id | UUID | 关联 Service |
| name | String | 部署名（service 内唯一），如 prod |
| status | Enum | active / paused / stopped |
| strategy | Enum | rolling / recreate / blue_green（记录型，执行归 Container Manager） |
| created_at / updated_at | | |

#### deployment_versions

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID | 主键 |
| deployment_id | UUID | 关联 Deployment |
| version | String | 版本号（deployment 内唯一），如 v1 / v2 |
| image | String? | 镜像标识（信息记录） |
| status | Enum | stable / canary / active / standby / draining / inactive |
| created_at / updated_at | | |

#### traffic_policies

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID | 主键 |
| service_id | UUID | 关联 Service |
| name | String | 策略名 |
| priority | Int | 优先级（低数值优先匹配） |
| match | Json? | 匹配规则（header / cookie / path / percent） |
| version_id | UUID? | 命中后定向 Version |
| weight | Int | 权重（version 分流场景） |
| sticky | Json? | sticky 配置（cookie name / ttl） |
| status | Enum | enabled / disabled |
| created_at / updated_at | | |

#### rate_limits

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID | 主键 |
| scope | Enum | global / tenant / domain / service / ip |
| tenant_id / domain_id / service_id | UUID? | 按 scope 取值 |
| limit | Int | 请求配额（窗口内） |
| window_seconds | Int | 时间窗口 |
| burst | Int? | 突发容量 |
| response_code | Int | 默认 429 |
| status | Enum | enabled / disabled |
| created_at / updated_at | | |

### 5.3 升级存量表

`instances` 升级（新增列，迁移保证存量行可迁移）：

```text
instances.service_id      ──► 保留；新增 deployment_id / version_id（可空，兼容 Phase 1 直挂）
instances.node_id         ──► 升级为引用 nodes(id)
instances.version         ──► 若 Version 体系启用，取 deployment_versions.version 冗余镜像
```

> 兼容策略：`deployment_id / version_id / node_id` 为空时实例仍按 Phase 1 语义直挂 Service，
> 路由表构建时两者并存，后续迁移到 Version 体系只是把存量实例归属到 default deployment/version。

### 5.4 迁移清单

- [x] `0004_deployments_nodes_versions.sql`：nodes / deployments / deployment_versions
- [x] `0005_traffic_policies_rate_limits.sql`
- [ ] `instances`：新增列 + 外键 + 索引（deployment_id / version_id 组合索引，node_id 索引）
- [ ] `deployment_versions(version)` 与 `deployments(name)` 唯一约束
- [ ] 存量 Phase 1 数据向后兼容：已注册实例在无 deployment/version 时可继续路由

---

## 六、S1 — 模型演进与数据平面 TLS

### 6.1 模块与文件（目录结构权威口径见 `项目目录结构.md`）

| 文件 | 说明 |
|------|------|
| `internal/node/{model,repository,service,handler}.go` | Node 管理 |
| `internal/deployment/{model,repository,service,handler,version}.go` | Deployment / Version |
| `internal/traffic/{policy,matcher,selector,service,handler}.go` | Traffic Policy（S2 使用） |
| `internal/ratelimit/*` | 限流（S5 使用） |
| `internal/metrics/{metrics,handler}.go` | 指标（S6 使用） |
| `migrations/0004_*.sql` / `0005_*.sql` | 建表 |
| `internal/instance/*` | 挂载 deployment / version / node |
| `internal/gateway/*` + `config` | HTTPS 监听 |
| `internal/cache/{route,auto,resolver}.go` | 路由表携带 Node / Version（随阶段演进） |

### 6.2 HTTPS 数据平面（按需启用）

- [x] config 增加 `gateway.https.{address,enabled,cert,key}`（当前 config 已预留 cert/key 字段）
- [x] 证书存在时与 HTTP 双监听；无证书场景维持 HTTP 单监听
- [x] 验证 `curl https://... --resolve` 可转发

### 6.3 验证

- [x] `0004 / 0005` 迁移在空库与 Phase 1 存量库上均能执行
- [x] Node / Deployment / Version 可 CRUD（先临时接口或 SQL 验证）
- [x] 存量实例在无 deployment/version 下仍可路由（回归不破）
- [x] HTTPS 监听开启后数据平面转发正常

---

## 七、S2 — 流量策略内核

> 目标：把路由从"每 Host 一个 Pool"升级为"每 Host 命中 Service → 依策略在
> Deployment 的多个 Version 之间选择 → 该 Version 的健康实例池里 LB"。

### 7.1 路由表结构演进

```text
RouteEntry（升级）
  domain / tenant / service
  versions: [ { version_id, status, weight,
               instances: [ { instance_id, node_id, version_id, address:port,
                              weight, health, status, draining } ] } ]
  policies: [ { name, priority, match, target_version_id, weight, sticky } ]
```

> 数据平面选择顺序（Phase 2）：
> Host 命中 → Service → 逐个按 priority 匹配策略（Header/Cookie/Path/百分比）→
> 命中策略定向 Version / 按版本权重分配 → 该 Version 健康池内 LB → Proxy。

### 7.2 模块与文件

| 文件 | 说明 |
|------|------|
| `internal/traffic/policy.go` | 策略模型与序列化 |
| `internal/traffic/matcher.go` | Header / Cookie / Path / 百分比匹配 |
| `internal/traffic/selector.go` | 从候选版本中选出目标版本 |
| `internal/cache/route.go` | 路由表重建时解析 deployment→versions→policies |
| `internal/loadbalancer/weighted.go` | 版本权重 + 实例权重两层 WRR 复用 |

### 7.3 必须处理

| 场景 | 处理 |
|------|------|
| 无任何策略 | 退化为"单一活跃 Version 或 stable 池"，行为与 Phase 1 一致 |
| 无匹配版本 | 返回 503（无 Healthy / 无可路由版本） |
| 百分比路由 | 按稳定随机 + sticky key 兜底 |
| 策略优先级冲突 | 高优先级先判；同优先级按权重 |
| 版本无健康实例 | 跳过该版本；全无则 503 |

### 7.4 验证

- [x] 90/10 两版本按比例分流（长样本计数接近 9:1）
- [x] Header 匹配（如 `x-canary=beta`）请求固定进 canary 版本
- [x] Path 前缀匹配定向
- [x] 无策略时与 Phase 1 行为一致（回归用例）

---

## 八、S3 — Node 感知、Container Manager 同步与 Sticky

### 8.1 目标

Phase 2 引入**跨 Node 的实例发现**：Container Manager / Node Agent 通过
Internal API 把 Node 与 Instance 状态推到 Maple Gateway，Gateway 缓存后路由。

```text
Node Agent / Container Manager
        │  POST /api/internal/discovery/... (register / heartbeat / sync)
        v
Maple Gateway（缓存 Node / Instance 状态）
        │
        v  AutoRebuild → RouteTable（过滤 offline Node 上的实例）
```

### 8.2 Internal API（独立认证，不对公网开放）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/internal/discovery/nodes/register` | 注册 Node |
| POST | `/api/internal/discovery/nodes/:id/heartbeat` | Node 心跳（更新 last_seen_at） |
| POST | `/api/internal/discovery/instances/register` | 注册 Instance |
| PATCH | `/api/internal/discovery/instances/:id` | 更新 Instance |
| DELETE | `/api/internal/discovery/instances/:id` | 注销 Instance |
| POST | `/api/internal/discovery/instances/:id/health` | 上报 Health |
| POST | `/api/internal/discovery/sync` | Container Manager 全量状态同步 |

> 认证：`MAPLE_INTERNAL_TOKEN` 独立于 admin Token；internal 分组不挂在
> `/api/admin` Bearer 中间件下（新增独立中间件）。

### 8.3 Node 状态感知

- Node 心跳超时（如 3 个心跳周期未到）→ Node offline → AutoRebuild 摘除其上全部实例。
- Node maintenance / disabled → 停分配新流量（其上实例 draining）。
- Node 状态变化不直接操控 Docker，只影响路由选择。

### 8.4 Sticky Session

- [ ] 命中策略携带 sticky 配置时，写入/读取 cookie（如 `MAPLE_SRV`）
- [ ] Cookie 值与 目标 version / instance 绑定；TTL 后过期回落到策略权重
- [ ] 内存表存储 session → instance（单节点）；跨 Gateway 共享归 Phase 3（Redis 预留）
- [ ] 支持按 Header（`X-User-Id`）做会话一致性

### 8.5 验证

- [ ] Node 心跳停止后其上实例自动摘除；心跳恢复后重新加入
- [ ] register → heartbeat → sync 全链路状态同步正确
- [ ] 同一会话（cookie）连续请求命中同一实例
- [ ] Internal API 无内部 Token 返回 401；公网不可达（端口/网段隔离验证）

---

## 九、S4 — Canary / Blue-Green / Rolling Update

> Maple Gateway 只执行**流量控制**，容器创建/销毁归 Container Manager。

### 9.1 Canary（基于 Traffic Policy + 版本权重）

```text
Canary 发布
  stable v1 (weight 90)  +  canary v2 (weight 10)
     ↓ 逐步：10 → 25 → 50 → 100（人工步进，接口驱动）
     ↓ promote：v2 提升为 stable，v1 draining → inactive
     ↓ rollback：v2 权重归 0，全部回 v1
```

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/canary` | 发布列表 |
| POST | `/api/admin/canary` | 创建 Canary（service_id、stable/deployment_version、canary version、初始权重） |
| GET | `/api/admin/canary/:id` | 详情 |
| PATCH | `/api/admin/canary/:id` | 改配置 |
| POST | `/api/admin/canary/:id/start` | 开始 |
| POST | `/api/admin/canary/:id/pause` | 暂停（权重冻结） |
| POST | `/api/admin/canary/:id/resume` | 恢复 |
| POST | `/api/admin/canary/:id/weight` | 调整 canary 流量比例 |
| POST | `/api/admin/canary/:id/promote` | 晋升 stable |
| POST | `/api/admin/canary/:id/rollback` | 回滚 stable |

### 9.2 Blue / Green

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/blue-green` | 列表 |
| POST | `/api/admin/blue-green` | 创建（service_id、blue/green version） |
| GET | `/api/admin/blue-green/:id` | 详情 |
| POST | `/api/admin/blue-green/:id/switch` | 切换 Active 版本 |
| POST | `/api/admin/blue-green/:id/rollback` | 回滚上一 Active |

> 切换前后可做一次健康/权重验证；实现本质是"版本权重整体翻转 + 旧版 draining"。

### 9.3 Rolling Update（流量配合）

| 场景 | Gateway 动作 |
|------|--------------|
| 新 Version 实例注册并 Healthy | AutoRebuild 后自动加入 Pool 接收流量 |
| 旧 Version 实例被 drain | 停止新请求（从池移除），等待在途连接结束 |
| 发布失败回滚 | 新版本实例 drain，旧版权重恢复 |

### 9.4 状态机与可回滚

所有发布动作写 **audit + 发布事件表（traffic_policies 变更记录 / deployment_versions 状态变迁）**，
支持从 Dashboard 一键回滚。

### 9.5 验证

- [x] Canary 从 10% 步进到 100% 全链路可操作，weight 变更即时生效（复用 S7 验证的 WRR）
- [x] promote 后 v2 成为 stable，请求全部进入 v2
- [x] rollback 后流量全部回到 v1
- [x] Blue/Green switch / rollback 整体翻转正确
- [x] 发布过程中旧实例 drain 语义正确（在途请求不被切断）

> S4 实现说明：Canary 经 `canary_releases` 状态机（migration 0006），start/pause/resume/weight/promote/rollback 每动作在事务内原子改 `deployment_versions.weight` 与状态并写 `canary_events` 流水；Blue/Green 经 `bluegreen_deployments`（migration 0007）翻转 active/standby 角色并记录 `bluegreen_events` 供回滚。发布动作由 AutoRebuild(5s) 同步到路由表即时生效。drain 语义：draining/standby 版本由 `versionRoutable` + resolver weight≤0 过滤双层保障从路由池即刻移除，停止接收新请求。

---

## 十、S5 — 限流系统

### 10.1 模型

| 维度 | Key | 说明 |
|------|-----|------|
| global | 全局 | 单机内存 / Redis 全局限流 |
| tenant | tenant_id | 租户总配额 |
| domain | domain_id | 域名配额 |
| service | service_id | 服务配额 |
| ip | 客户端 IP | 单 IP 配额 |

> 计数器：单 Gateway 内用内存滑动窗口/令牌桶；多 Gateway 部署用 Redis INCR + 窗口（Phase 3
> 强化，Phase 2 提供实现与开关）。

### 10.2 数据平面接入点

RateLimit 配置同样进入路由缓存（RateLimitCache），请求在 Host 命中后、转发前执行：

```text
Request → Host 命中 → RateLimit 判定（各维度取交集，任一超限则 429）
                              ↓ 通过
              Traffic Policy 分流 → LB → Proxy
```

### 10.3 接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/rate-limits` | 列表 |
| POST | `/api/admin/rate-limits` | 创建 |
| GET | `/api/admin/rate-limits/:id` | 详情 |
| PATCH | `/api/admin/rate-limits/:id` | 修改 |
| DELETE | `/api/admin/rate-limits/:id` | 删除 |
| POST | `/api/admin/rate-limits/:id/enable` | 启用 |
| POST | `/api/admin/rate-limits/:id/disable` | 禁用 |

### 10.4 验证

- [x] 单维度限流命中返回 429（自定义响应可配）
- [x] 多维度叠加时取最严
- [x] 窗口滑动正确（不因突发漏过超量）
- [x] 关闭 Redis 时内存限流仍可用

> S5 实现说明：`rate_limits` 表（migration 0005）由 `internal/ratelimit` 提供 CRUD；
> 数据平面经 route cache（`RouteEntry.Limits`，按 global/domain/tenant/service/ip 预分组）
> + `CacheResolver` 内内存滑动窗口 `Limiter` 判定（任一维度超限 → `router.ErrRateLimited` → 429）。
> 规则变化随 AutoRebuild(5s) 生效，无需重启；无 Redis 依赖（单机内存计数）。
> e2e：service 5/60s → 前 5 个 200 后 429；叠加 domain 3/60s → 第 4 个即 429（取最严）；
> 窗口 60s 滑动后自动恢复；ip 2/60s → 前 2 个 200 后 429。

---

## 十一、S6 — 可观测与控制（Metrics / Logs / Settings）

### 11.1 Metrics（Prometheus）

| 指标 | 类型 | 说明 |
|------|------|------|
| maple_requests_total{tenant,service,status} | Counter | 请求量 |
| maple_request_duration_seconds | Histogram | 端到端延迟 |
| maple_upstream_requests_total | Counter | 上游请求 |
| maple_upstream_errors_total | Counter | 上游错误 |
| maple_upstream_duration_seconds | Histogram | 上游延迟 |
| maple_active_connections | Gauge | 活跃连接 |
| maple_route_cache_hits_total / misses_total | Counter | 缓存命中 |
| maple_instance_health{instance_id,health} | Gauge | 实例健康 |
| maple_rate_limit_hits_total{scope} | Counter | 限流命中 |

- [ ] `GET /metrics`（Prometheus 文本格式，9090 或与 mgmt 分离配置）
- [ ] 管理端指标查询：`/api/admin/metrics/{overview,traffic,latency,errors,tenants/:id,services/:id,instances/:id}`
- [ ] 埋点在数据平面（proxy）与系统组件（health/ratelimit）内完成

### 11.1b Dashboard 聚合接口（S7 前端总览数据源）

DashboardPage 所需数据由后端聚合，前端不直接拼接多路列表请求：

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/dashboard/overview` | 总览卡片：Tenant/Domain/Service/Node/Instance 计数、Healthy/Unhealthy 实例、5xx、P95/P99、活跃连接 |
| GET | `/api/admin/dashboard/traffic` | 请求流量趋势（时间序列） |
| GET | `/api/admin/dashboard/errors` | 错误率趋势 |
| GET | `/api/admin/dashboard/latency` | 延迟分位趋势 |
| GET | `/api/admin/dashboard/instances` | 实例健康状态统计 |
| GET | `/api/admin/dashboard/nodes` | Node 状态统计 |
| GET | `/api/admin/dashboard/canary` | 进行中的 Canary 状态列表 |

> 实现上基于 S6 的 metrics 内部聚合 + 资源计数，落到本阶段新增的 `internal/system` /
> `internal/metrics` dashboard handler。归属排期计入 S7（随前端一起联调），后端接口在 S7 先期完成。

### 11.2 Logs（分类查询）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/logs/access` | Access Log |
| GET | `/api/admin/logs/error` | Error Log |
| GET | `/api/admin/logs/routing` | Routing Log |
| GET | `/api/admin/logs/health` | Health Log |
| GET | `/api/admin/logs/security` | Security Log |
| GET | `/api/admin/logs/audit` | Audit Log |

> 后端做分类写入 + 查询接口（含分页 / 时间范围 / tenant / status 筛选）。落地形态：DB 审计 +
> 结构化文件；本阶段不引入独立日志服务。

### 11.3 Settings（动态配置）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/settings` | 获取配置 |
| PATCH | `/api/admin/settings/gateway` | Gateway 基础配置 |
| PATCH | `/api/admin/settings/proxy` | Proxy 配置 |
| PATCH | `/api/admin/settings/health` | Health 默认配置 |
| PATCH | `/api/admin/settings/security` | 安全配置 |
| PATCH | `/api/admin/settings/logging` | 日志配置 |
| PATCH | `/api/admin/settings/metrics` | Metrics 配置 |

> 保存后触发运行时更新（连接/传输/超时/限流阈值），不要求重启。配置版本化用于审计与回滚。

### 11.4 验证

- [x] curl `/metrics` 能看到各指标并随请求增长
- [x] 管理端日志接口可按 host / 状态码 / 时间范围筛选
- [x] 修改 Settings 后（如 proxy 超时）无需重启即生效
- [ ] 指标与日志覆盖 tenant/service/instance 维度，Dashboard 可用（S7）
- [ ] `/api/admin/dashboard/overview` 返回的计数与资源列表一致（S7 联调时核验）

> S6 实现说明：
> - **Metrics**：`internal/metrics` 自研原子计数/histogram（无外部依赖），proxy 埋点
>   （maple_requests_total / request_duration_seconds / upstream_requests_total /
>   active_connections），`GET /metrics` Prometheus 文本导出。修复 statusRecorder 缺失
>   Flush/Hijack 导致的 ReverseProxy 卡死。
> - **Logs**：`internal/logs` 内存环形缓冲记录数据平面访问日志，`GET /api/admin/logs/access`
>   支持 host/status/time 过滤 + 分页（error/audit 类沿用 zap + audit_logs 表）。
> - **Settings**：settings 表（migration 0008）+ `internal/settings`，GET/PATCH
>   `/api/admin/settings/:section`，写库即 version+1 并 Reload 内存 Store；消费方经
>   Store.Get/GetString/GetInt 读取即热生效（无需重启），audit 中间件自动记录变更。

---

## 十二、S7 — Admin Dashboard（React 前端）

> 前端是 Phase 2 的正式交付物（Phase 1 仅保留 `frontend/` 占位，无代码）。
> 它只调用 `/api/auth/*` 与 `/api/admin/*`，不参与 Data Plane。
> 目录结构权威口径见 `项目目录结构.md` 的 frontend 章节。

### 12.1 技术栈

| 项 | 选型 | 说明 |
|----|------|------|
| 框架 | React 19 | 管理后台 SPA |
| 语言 | TypeScript 5.x | 全量类型，对齐后端 JSON 响应 |
| 构建 | Vite | dev / build / dev proxy |
| 样式 | Tailwind CSS 4 | 布局与通用 UI |
| 路由 | react-router | AuthLayout / AdminLayout 双层路由 |
| 状态 | Zustand | auth / 页面级缓存 |
| 请求 | fetch 轻封装（或 axios） | Bearer 注入、401 统一处理、解包 `{code,message,data,meta}` |
| 图表 | Chart.js（或自绘 SVG） | Dashboard 趋势曲线 |

### 12.2 工程骨架

| 文件 | 说明 |
|------|------|
| `frontend/package.json` / `tsconfig.json` / `vite.config.ts` | React 19 / Vite / TS；`vite.config.ts` 配 dev proxy `/api → http://localhost:4000` |
| `frontend/src/main.tsx` / `App.tsx` | 挂载入口与路由表 |
| `frontend/src/api/client.ts` | fetch 封装：baseURL `/api`、token 注入、401 统一处理、统一响应解包 |
| `frontend/src/api/*.ts` | 按模块的请求封装：auth / tenants / domains / services / deployments / instances / nodes / traffic / canary / rate-limits / logs / settings / metrics |
| `frontend/src/types/*.ts` | 与后端 JSON 对应 TS 类型（含分页 meta / 状态枚举） |
| `frontend/src/stores/auth.ts` | 登录态（token + 当前管理员）与持久化 |
| `frontend/src/stores/*.ts` | 页面级缓存（可选） |
| `frontend/src/hooks/*` | 通用 hook：usePagedList / useAction / usePolling 等 |
| `frontend/src/components/{charts,tables,forms,status}/` | 图表 / 数据表格 / 表单 / 状态徽章组件 |
| `frontend/src/layouts/{AdminLayout,AuthLayout}.tsx` | 页面框架 |
| `frontend/src/pages/*.tsx` | 页面（见 12.6） |
| `frontend/Dockerfile` | 构建产物 → Nginx 托管（反代 `/api` 到 Management API） |

### 12.3 API 客户端与数据流约定

- 沿用 Phase 1 统一响应：成功 `code === 0`，data 携带业务数据，meta 携带分页信息。
- 列表请求传 `page / page_size` 等，meta.total 供表格分页。
- 请求自动附加 `Authorization: Bearer <token>`。
- 收到 401 → 清登录态 → 跳 Login；业务错误码非 0 → 统一 Toast 提示。
- 状态动作（enable / disable / drain / canary weight / promote / switch 等）成功后刷新当前列表或轮询详情。

### 12.4 认证与路由守卫

- `/login` 登录成功 → 持久化 token → 跳 Dashboard。
- 受保护路由未登录 → 重定向 Login；token 失效（401）→ 自动登出。
- 刷新页面后凭持久化 token 调 `GET /api/admin/auth/me` 恢复会话。

### 12.5 布局

- **侧边栏固定方案**：AdminLayout 用**独立 h-screen 根容器**（不是 min-h-screen），
  侧边栏 sticky、内容区独立 `overflow-y-auto`——避免外层包裹容器导致滚动与吸顶失效
  （此前已在其他项目踩坑并沉淀为固定规则）。
- 顶栏：系统信息 / 管理员 / 退出；侧边导航：Dashboard、Tenants、Domains、Services、
  Deployments、Instances、Nodes、Traffic、Canary、Health、Logs、Settings。

### 12.6 页面清单（对应 `项目目录结构.md` frontend pages）

| 页面 | 内容 |
|------|------|
| LoginPage | 登录 / token 持久化 |
| DashboardPage | 总览：请求量、活跃连接、5xx、P95/P99、Tenant/Domain/Node/Instance 计数、健康实例比例、Canary 状态 |
| TenantsPage / DomainsPage / ServicesPage | 各资源 CRUD + 状态开关 |
| DeploymentsPage | Deployment + Version 列表 / 注册 / stable 切换 |
| InstancesPage | Instance 列表 / 注册 / enable-disable-drain / 健康状态 |
| NodesPage | Node 列表 / 状态 / 心跳时间 / 其上 Instance |
| TrafficPage | Traffic Policy 增删改 + 优先级 + 匹配规则表单 |
| CanaryPage | 发布列表 + 权重滑杆 + start/pause/promote/rollback 操作 |
| HealthPage | 健康检查配置 / 实例健康矩阵 |
| LogsPage | 分类日志查看 + tenant / 时间范围筛选 |
| SettingsPage | 动态设置表单 |

### 12.7 关键交互示例

- CanaryPage 权重滑杆 → `POST /api/admin/canary/:id/weight`，提交后刷新实际流量分布。
- Dashboard 定时轮询 `/api/admin/dashboard/*` 与实例健康矩阵。
- DeploymentsPage 将 Version 设为 stable → 二次确认 → `POST .../versions/:versionId/stable`。

### 12.8 验证

- [ ] 登录后各页面 CRUD 可用且与后端实时同步
- [ ] 创建 Domain 后 Dashboard / Routes 立即可见
- [ ] CanaryPage 拖权重即时反映到实际流量分布
- [ ] 401 过期 token 自动登出
- [ ] 桌面与窄屏布局可用（h-screen 侧边栏方案）
- [ ] `npm run build` 通过；Nginx 产物可访问并反代 `/api` 到 4000

---

## 十三、API 清单汇总（Phase 2 新增）

| 模块 | 接口数（约） | 阶段 | 优先级 |
|------|------------|------|--------|
| Deployment / Version | 12 | S1/S4 | P0 |
| Node 管理 | 10 | S1/S3 | P0 |
| Traffic Policy | 9 | S2 | P0 |
| Internal Discovery | 8 | S3 | P0 |
| Canary | 12 | S4 | P0 |
| Blue / Green | 5 | S4 | P1 |
| Rolling Update 查看 | 3 | S4 | P1 |
| Rate Limit | 7 | S5 | P1 |
| Metrics | 7 | S6 | P1 |
| Logs | 6 | S6 | P1 |
| Settings | 7 | S6 | P1 |
| Dashboard | 7 | S6/S7 | P1 |
| Auth 增强（refresh / change-password） | 2 | S6 | P1 |
| **合计** | **约 95** | | |

> 多数为 Phase 1 已有 CRUD 模式（handler → service → repository → response）的复制扩展，
> 单接口成本低；工作重心在策略内核（S2/S4）与前端（S7）。

---

## 十四、技术依赖与配置清单

### 后端新增依赖

```text
github.com/prometheus/client_golang        # Metrics 采集与 /metrics
github.com/redis/go-redis/v9               # 限流计数 / Sticky 共享（可选启用）
github.com/gofiber/fiber/v3                # 已有，继续
golang.org/x/crypto / golang-jwt/jwt/v5    # 已有
```

> 限流算法若自研内存实现为主，Redis 仅多实例场景启用，不把 Redis 变为主链路硬依赖。

### 环境变量新增

```env
MAPLE_INTERNAL_TOKEN=...                   # Container Manager / Node Agent 专用
MAPLE_METRICS_ADDR=:9090                   # Prometheus 抓取端口
MAPLE_REDIS_URL=redis://localhost:6379     # 限流/Sticky 共享（可选）
MAPLE_RATE_LIMIT_MODE=memory               # memory | redis
```

### 配置结构新增（config.example.yaml）

```yaml
gateway:
  https:
    address: ":443"
    enabled: false
    cert: /etc/maple-gateway/tls.crt
    key: /etc/maple-gateway/tls.key
metrics:
  address: ":9090"
ratelimit:
  mode: memory
traffic:
  sticky_cookie_name: MAPLE_SRV
  sticky_ttl: 24h
internal:
  token_env: MAPLE_INTERNAL_TOKEN
```

---

## 十五、Docker Compose / 部署演进

```yaml
services:
  postgres: { ... }                 # Phase 1
  redis:    { enabled for Phase 2 可选 }  # 限流/Sticky 共享
  maple-gateway:
    ports:
      - "80:80"
      - "443:443"                   # Phase 2 按需启用 TLS
      - "4000:4000"
      - "9090:9090"                 # Metrics，仅监控网段
  frontend:
    # React Admin；开发 5173，生产由 Nginx 托管构建产物，仅内网
  # 测试容器：shop-a / shop-b / shop-canary（多版本模拟发布）
```

- `frontend/Dockerfile`（build + nginx serve）与 `server/Dockerfile` 并列。
- 生产注意：4000 / 5432 / 6379 / 9090 不对公网开放；`/api/internal/*` 端口与网段隔离。

---

## 十六、关键设计决策

### 16.1 两层负载均衡

Phase 1 只有"实例 WRR"。Phase 2 引入"**版本权重 → 实例权重**"两层：

```text
策略分流（Header/Cookie/Path/percent）
   ↓
版本选择（Deployment 内 versions 按 weight WRR）
   ↓
实例选择（该 Version 健康实例池 WRR / sticky）
   ↓
Proxy
```

### 16.2 Version 是分流的最小单元

- Canary / Blue-Green 都以 `deployment_versions` 为操作对象，不直接操作 instance 权重。
- 旧版本 promote / 回滚 = 版本权重整体切换 + 状态机迁移。

### 16.3 路由表继续内存化

策略与限流配置同样随 AutoRebuild 进入内存（或独立缓存表），数据平面每请求零 DB 查询不变。

### 16.4 Gateway 仍无状态

Node / Instance 状态来自 Internal API 心跳上报，可随时重同步；Redis 只承载跨实例共享态
（限流计数、sticky map），Gateway 进程本身保持无状态，为 Phase 3 HA 留空间。

### 16.5 Sticky 的实现取舍

单 Gateway 内存 map 足够 MVP；Phase 2 提供 cookie 绑定到 instance_id + 幂等兜底，Redis 承载
多 Gateway 时为 Phase 3 预留接口，避免过度设计。

### 16.6 安全边界强化

| 项 | 说明 |
|----|------|
| Internal API 独立 Token | 与 admin Bearer 分离；不对公网开放 |
| Metrics 网段 | 9090 仅监控网络可访问 |
| SSRF 防护延续 | Internal 注册的 instance address 同 Phase 1 校验 |
| Node/Instance 上报鉴权 | heartbeat / sync 全走 internal Token |
| 发布动作审计 | 所有 canary / bg / rolling 操作写 audit_logs |
| 日志脱敏 | 不记录 Token / 密码 / 内部凭据 |

### 16.7 兼容与回滚

- Phase 1 直挂实例在无 deployment 时继续可路由（S1 回归保证）。
- 每次发布保留上一 Active 版本状态，Promote / Switch 前可一键回滚。
- 配置带版本号，Settings / Policy 变更留 audit 轨迹。

---

## 十七、开发顺序建议

```text
server/（延续 Phase 1）
├── internal/deployment/     → S1 模型 + 管理 API
├── internal/node/           → S1/S3
├── internal/traffic/        → S2 内核
├── internal/cache/          → S2/S3 路由表携带 versions/policies/node 状态
├── internal/loadbalancer/   → S2 版本权重层；S3 sticky
├── internal/discovery/      → S3 Internal API
├── internal/ratelimit/      → S5
├── internal/canary/         → S4 编排
├── internal/metrics/        → S6
├── internal/logging/        → S6 查询
├── internal/system/         → S6 Settings
└── internal/gateway/        → S1 HTTPS；数据平面各阶段接入点

frontend/（S7 起）
└── src/{pages,components,layouts,api,stores,types,...}
```

> 建议在 S2 完成后即搭 S7 工程骨架（并行推进），避免后端 S5/S6 期间前端无事可做。

---

## 十八、Phase 2 交付标准

### 功能交付

1. 一个 Service 可挂多个 Deployment/Version，通过 Traffic Policy 实现 Header/Cookie/Path/百分比分流。
2. 金丝雀 10→100 可步进、可暂停、可 promote、可 rollback；Blue/Green 与 Rolling 流量配合可用。
3. 多 Node 实例发现与心跳同步：Node 下线其上实例自动摘除，恢复后自动加入。
4. Sticky Session 保证同一会话落在同一实例/版本。
5. Tenant/Domain/Service/IP 限流可用，超限返回可配 429。
6. Prometheus `/metrics` 与日志查询接口可用；Settings 修改不重启生效。
7. React Admin 可完成登录、总览与全部管理操作（含 Canary 权重实时调整）。
8. 存量 Phase 1 配置（直挂 Service 的实例）平滑迁移，不丢服务。

### 质量要求

1. 数据平面主链路仍不查询 PostgreSQL；策略/限流配置内存化。
2. Sticky / 限流在 Redis 关闭时退化为单机内存，主链路不因辅助组件故障中断。
3. 所有发布动作与 Settings 变更写入审计。
4. Internal API 无 Token 401；`/metrics` 受限访问；SSRF 防护延续。
5. `go test ./...` 全绿；`go test -race ./...` 无数据竞争（Linux/CI 执行；Windows 本机无 gcc，-race 需 cgo，改以 `go vet ./...` + 并发路径实测替代）。
6. 端到端演练脚本覆盖：注册双版本 → 分流 → Canary 步进 → 回滚 → 限流 → 指标。
7. 前端构建通过（`npm run build`），开发环境 5173 与后端 4000 代理联通。

### 非交付（Phase 3）

- Gateway 多实例 HA / 配置同步 / Leader / 自动故障转移
- OpenTelemetry / 分布式追踪 / Request ID 体系
- Consistent Hash / Least Connections
- 自动 Canary（指标驱动推进与自动回滚）
- 多区域路由与 Node 区域亲和
- Podman 兼容、插件机制、完整 RBAC / Cloudflare for SaaS 自动化

---

## 十九、Phase 3 衔接（预告）

Phase 2 收尾后，发布控制闭环与可视化管理均可用。Phase 3 在此之上叠加：

```text
Gateway 多实例 HA（配置同步 + 无状态扩展）
   ↓
自动 Canary（指标驱动 5→100 自动推进 + 异常自动回滚）
   ↓
OpenTelemetry 追踪 / Consistent Hash / Least Connections
   ↓
多区域路由 + 完整 RBAC + Cloudflare for SaaS 自动化 + 插件机制
```

Phase 2 保持 Gateway Stateless、Node/Instance 状态可重同步、Sticky/限流共享态走 Redis，
即为 Phase 3 多 Gateway HA 预留空间。
