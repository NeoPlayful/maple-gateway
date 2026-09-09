# Maple Gateway API 接口全面分析

> 项目名称：Maple Gateway 项目定位：Multi-Tenant Gateway 技术栈：Go +
> Fiber v3 + PostgreSQL 16 + Redis 7 + Memory Route Cache 架构模式：Data
> Plane 与 Control Plane 分离，Maple Gateway
> 统一负责多租户路由、反向代理、负载均衡、故障切换与流量策略
> 部署模式：Docker / Linux Binary，支持多 Node、多 Container
> 前后端模式：server + frontend 前后端分离 容器管理：Container Manager
> 负责 Node / Container 生命周期，Maple Gateway 负责流量

------------------------------------------------------------------------

# 一、认证系统 (Auth)

Management API 管理员认证。

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/auth/login` | 管理员登录 |
| POST | `/api/auth/logout` | 管理员退出 |
| GET | `/api/auth/me` | 获取当前管理员信息（v0.1.18 起含 `role`：super_admin / operator / viewer） |
| POST | `/api/auth/refresh` | 刷新 Token |
| POST | `/api/auth/change-password` | 修改密码 |

> 认证：Bearer JWT，载荷含 admin id + email + role（RBAC 授权依据）。登录/刷新/me 均返回管理员 `role`。
> 管理员账号管理（超管专属）：`GET/POST /api/admin/admins`、`PATCH /api/admin/admins/:id/role`、`PATCH /api/admin/admins/:id/status`。

------------------------------------------------------------------------

# 二、Dashboard

用于 Maple Gateway 管理后台首页。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/dashboard/overview` | Gateway 总览 |
| GET | `/api/admin/dashboard/traffic` | 请求流量统计 |
| GET | `/api/admin/dashboard/errors` | 错误率统计 |
| GET | `/api/admin/dashboard/latency` | 延迟统计 |
| GET | `/api/admin/dashboard/instances` | Instance 健康状态统计 |
| GET | `/api/admin/dashboard/nodes` | Node 状态统计 |
| GET | `/api/admin/dashboard/canary` | Canary 发布状态 |

------------------------------------------------------------------------

# 三、Tenant 管理 (Tenants)

Tenant 是 Maple Gateway 的核心资源之一。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/tenants` | Tenant 列表 |
| POST | `/api/admin/tenants` | 创建 Tenant |
| GET | `/api/admin/tenants/:id` | Tenant 详情 |
| PATCH | `/api/admin/tenants/:id` | 修改 Tenant |
| DELETE | `/api/admin/tenants/:id` | 删除 Tenant |
| POST | `/api/admin/tenants/:id/enable` | 启用 Tenant |
| POST | `/api/admin/tenants/:id/disable` | 禁用 Tenant |
| POST | `/api/admin/tenants/:id/suspend` | 暂停 Tenant |
| GET | `/api/admin/tenants/:id/routes` | 查看 Tenant 当前路由 |

Tenant 状态建议：

-   active
-   suspended
-   disabled

------------------------------------------------------------------------

# 四、Domain 管理 (Domains)

负责：

Domain → Tenant

映射。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/domains` | Domain 列表 |
| POST | `/api/admin/domains` | 添加 Domain |
| GET | `/api/admin/domains/:id` | Domain 详情 |
| PATCH | `/api/admin/domains/:id` | 修改 Domain |
| DELETE | `/api/admin/domains/:id` | 删除 Domain |
| POST | `/api/admin/domains/:id/enable` | 启用 Domain |
| POST | `/api/admin/domains/:id/disable` | 禁用 Domain |
| POST | `/api/admin/domains/:id/verify` | 验证 Domain 状态 |
| GET | `/api/admin/domains/:id/routes` | 查看 Domain 实际路由 |

Domain 需要关联：

-   tenant_id
-   hostname
-   status
-   service_id
-   tls_mode

------------------------------------------------------------------------

# 五、Service 管理 (Services)

一个 Tenant 可以拥有多个 Service。

例如：

-   storefront
-   admin
-   api

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/services` | Service 列表 |
| POST | `/api/admin/services` | 创建 Service |
| GET | `/api/admin/services/:id` | Service 详情 |
| PATCH | `/api/admin/services/:id` | 修改 Service |
| DELETE | `/api/admin/services/:id` | 删除 Service |
| POST | `/api/admin/services/:id/enable` | 启用 Service |
| POST | `/api/admin/services/:id/disable` | 禁用 Service |
| GET | `/api/admin/services/:id/instances` | 获取 Service 实例 |
| GET | `/api/admin/services/:id/routes` | 查看 Service 当前路由 |

------------------------------------------------------------------------

# 六、Deployment 管理 (Deployments)

Deployment 表示一个 Service 的部署。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/deployments` | Deployment 列表 |
| POST | `/api/admin/deployments` | 创建 Deployment |
| GET | `/api/admin/deployments/:id` | Deployment 详情 |
| PATCH | `/api/admin/deployments/:id` | 修改 Deployment |
| DELETE | `/api/admin/deployments/:id` | 删除 Deployment |
| GET | `/api/admin/deployments/:id/versions` | Deployment Version 列表 |
| GET | `/api/admin/deployments/:id/instances` | Deployment Instance 列表 |

Maple Gateway 不通过这些接口创建 Container。

Deployment 主要保存 Gateway 所需的流量和版本关系。

------------------------------------------------------------------------

# 七、Deployment Version

支持 Stable、Canary、Blue / Green 等版本状态。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/deployments/:id/versions` | Version 列表 |
| POST | `/api/admin/deployments/:id/versions` | 注册 Version |
| GET | `/api/admin/deployments/:id/versions/:versionId` | Version 详情 |
| PATCH | `/api/admin/deployments/:id/versions/:versionId` | 修改 Version |
| DELETE | `/api/admin/deployments/:id/versions/:versionId` | 删除 Version |
| POST | `/api/admin/deployments/:id/versions/:versionId/stable` | 设置为 Stable |
| POST | `/api/admin/deployments/:id/versions/:versionId/drain` | Version 停止接收新流量 |

Version 状态可包括：

-   stable
-   canary
-   active
-   standby
-   draining
-   inactive

------------------------------------------------------------------------

# 八、Instance 管理 (Instances)

Instance 表示实际运行的 Container 实例。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/instances` | Instance 列表 |
| GET | `/api/admin/instances/:id` | Instance 详情 |
| POST | `/api/admin/instances/register` | 注册 Instance |
| PATCH | `/api/admin/instances/:id` | 更新 Instance 信息 |
| DELETE | `/api/admin/instances/:id` | 注销 Instance |
| POST | `/api/admin/instances/:id/enable` | 允许接收流量 |
| POST | `/api/admin/instances/:id/disable` | 禁止接收流量 |
| POST | `/api/admin/instances/:id/drain` | 进入 Connection Draining |
| POST | `/api/admin/instances/:id/undrain` | 取消 Draining |
| GET | `/api/admin/instances/:id/health` | 获取 Instance 健康状态 |
| GET | `/api/admin/instances/:id/traffic` | 获取 Instance 流量统计 |

Instance 注册信息：

-   instance_id
-   tenant_id
-   service_id
-   deployment_id
-   version_id
-   node_id
-   address
-   port
-   protocol
-   weight
-   status

------------------------------------------------------------------------

# 九、Node 管理 (Nodes)

Node 生命周期主要由 Container Manager 管理。

Maple Gateway 保存流量路由需要的 Node 状态。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/nodes` | Node 列表 |
| GET | `/api/admin/nodes/:id` | Node 详情 |
| POST | `/api/admin/nodes/register` | 注册 Node |
| PATCH | `/api/admin/nodes/:id` | 更新 Node 信息 |
| DELETE | `/api/admin/nodes/:id` | 注销 Node |
| POST | `/api/admin/nodes/:id/enable` | Node 加入流量 |
| POST | `/api/admin/nodes/:id/disable` | Node 停止接收新流量 |
| POST | `/api/admin/nodes/:id/maintenance` | 设置维护状态 |
| GET | `/api/admin/nodes/:id/instances` | 获取 Node 上的 Instance |
| GET | `/api/admin/nodes/:id/health` | Node 健康状态 |

------------------------------------------------------------------------

# 十、Service Discovery

用于 Container Manager 与 Maple Gateway 同步运行状态。

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/internal/discovery/nodes/register` | 注册 Node |
| POST | `/api/internal/discovery/nodes/:id/heartbeat` | Node 心跳 |
| POST | `/api/internal/discovery/instances/register` | 注册 Instance |
| PATCH | `/api/internal/discovery/instances/:id` | 更新 Instance |
| DELETE | `/api/internal/discovery/instances/:id` | 注销 Instance |
| POST | `/api/internal/discovery/instances/:id/heartbeat` | Instance 心跳 |
| POST | `/api/internal/discovery/sync` | Container Manager 全量状态同步 |

`/api/internal/*`：

-   不对公网开放
-   使用内部认证
-   仅供 Container Manager / Node Agent / Maple Gateway 内部组件使用

------------------------------------------------------------------------

# 十一、Route 管理 (Routes)

用于查看 Maple Gateway 当前实际生效的路由。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/routes` | 当前 Route 列表 |
| GET | `/api/admin/routes/:id` | Route 详情 |
| GET | `/api/admin/routes/domain/:hostname` | 根据 Domain 查询路由 |
| GET | `/api/admin/routes/tenant/:tenantId` | 查询 Tenant 路由 |
| GET | `/api/admin/routes/service/:serviceId` | 查询 Service 路由 |
| POST | `/api/admin/routes/reload` | 重新构建 Route Cache |

核心路由：

Domain → Tenant → Service → Deployment → Version → Instance → Node

------------------------------------------------------------------------

# 十二、Route Cache

Maple Gateway 请求主链路使用 Memory Route Cache。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/cache/status` | Route Cache 状态 |
| GET | `/api/admin/cache/stats` | Cache Hit / Miss 统计 |
| POST | `/api/admin/cache/rebuild` | 全量重建 Route Cache |
| POST | `/api/admin/cache/refresh` | 增量刷新 Route Cache |

正常情况下：

Management API 修改配置 → PostgreSQL → Route Cache Sync

不需要管理员频繁手动调用 Cache API。

------------------------------------------------------------------------

# 十三、Load Balancing

配置 Service / Deployment 的负载均衡策略。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/services/:id/load-balancing` | 获取负载均衡策略 |
| PATCH | `/api/admin/services/:id/load-balancing` | 修改负载均衡策略 |
| GET | `/api/admin/services/:id/upstreams` | 查看当前 Upstream Pool |

支持策略：

-   round_robin（默认）
-   weighted_round_robin
-   sticky
-   least_connections（v0.1.18 已实现，`traffic_policies.balance=least_conn`）
-   consistent_hash（v0.1.18 已实现，`traffic_policies.balance=consistent_hash`）

------------------------------------------------------------------------

# 十四、Health Check

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/health-checks` | Health Check 配置列表 |
| POST | `/api/admin/health-checks` | 创建 Health Check |
| GET | `/api/admin/health-checks/:id` | Health Check 详情 |
| PATCH | `/api/admin/health-checks/:id` | 修改 Health Check |
| DELETE | `/api/admin/health-checks/:id` | 删除 Health Check |
| POST | `/api/admin/health-checks/:id/test` | 手动测试 Health Check |
| GET | `/api/admin/health/instances` | 所有 Instance 健康状态 |

支持：

-   HTTP Health Check
-   TCP Health Check
-   Failure Threshold
-   Success Threshold
-   Interval
-   Timeout

------------------------------------------------------------------------

# 十五、Failover

Failover 主要自动执行，管理接口用于查看和人工控制。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/failover/status` | Failover 总体状态 |
| GET | `/api/admin/failover/events` | Failover 事件 |
| GET | `/api/admin/failover/instances/:id` | Instance Failover 状态 |
| POST | `/api/admin/failover/instances/:id/remove` | 手动摘除 Instance |
| POST | `/api/admin/failover/instances/:id/restore` | 手动恢复 Instance |

------------------------------------------------------------------------

# 十六、Traffic Policy

Traffic Policy 控制请求应该进入哪个 Version / Instance。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/traffic-policies` | Traffic Policy 列表 |
| POST | `/api/admin/traffic-policies` | 创建 Traffic Policy |
| GET | `/api/admin/traffic-policies/:id` | Traffic Policy 详情 |
| PATCH | `/api/admin/traffic-policies/:id` | 修改 Traffic Policy |
| DELETE | `/api/admin/traffic-policies/:id` | 删除 Traffic Policy |
| POST | `/api/admin/traffic-policies/:id/enable` | 启用策略 |
| POST | `/api/admin/traffic-policies/:id/disable` | 禁用策略 |
| POST | `/api/admin/traffic-policies/:id/test` | 测试策略匹配 |

Traffic Policy 支持：

-   Percentage
-   Version Weight
-   Instance Weight
-   Header Match
-   Cookie Match
-   Path Match
-   Sticky Routing

------------------------------------------------------------------------

# 十七、Canary Deployment

Maple Gateway 负责 Canary Traffic，不负责创建 Canary Container。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/canary` | Canary 发布列表 |
| POST | `/api/admin/canary` | 创建 Canary 流量策略 |
| GET | `/api/admin/canary/:id` | Canary 详情 |
| PATCH | `/api/admin/canary/:id` | 修改 Canary 配置 |
| POST | `/api/admin/canary/:id/start` | 开始 Canary |
| POST | `/api/admin/canary/:id/pause` | 暂停 Canary |
| POST | `/api/admin/canary/:id/resume` | 恢复 Canary |
| POST | `/api/admin/canary/:id/weight` | 调整 Canary 流量比例 |
| POST | `/api/admin/canary/:id/promote` | Canary 晋升 Stable |
| POST | `/api/admin/canary/:id/rollback` | 回滚到 Stable |
| GET | `/api/admin/canary/:id/metrics` | Canary 指标 |

典型流程：

5% → 10% → 25% → 50% → 100%

------------------------------------------------------------------------

# 十八、Blue / Green Deployment

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/blue-green` | Blue / Green 发布列表 |
| POST | `/api/admin/blue-green` | 创建 Blue / Green 流量配置 |
| GET | `/api/admin/blue-green/:id` | 发布详情 |
| POST | `/api/admin/blue-green/:id/switch` | 切换 Active Version |
| POST | `/api/admin/blue-green/:id/rollback` | 回滚上一版本 |

Maple Gateway 只执行流量切换。

实际 Container 部署仍由 Container Manager 完成。

------------------------------------------------------------------------

# 十九、Rolling Update Traffic

Maple Gateway 配合 Container Manager 完成 Rolling Update。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/rolling-updates` | Rolling Update 状态列表 |
| GET | `/api/admin/rolling-updates/:id` | 更新状态 |
| POST | `/api/admin/instances/:id/drain` | 旧 Instance 停止新请求 |
| POST | `/api/admin/instances/:id/undrain` | Instance 恢复接流量 |

主要职责：

新 Instance Healthy → 加入 Upstream Pool

旧 Instance Draining → 停止新请求 → 等待连接结束 → Container Manager
销毁

------------------------------------------------------------------------

# 二十、Rate Limit

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/rate-limits` | Rate Limit 列表 |
| POST | `/api/admin/rate-limits` | 创建限流策略 |
| GET | `/api/admin/rate-limits/:id` | 限流详情 |
| PATCH | `/api/admin/rate-limits/:id` | 修改限流策略 |
| DELETE | `/api/admin/rate-limits/:id` | 删除限流策略 |
| POST | `/api/admin/rate-limits/:id/enable` | 启用限流 |
| POST | `/api/admin/rate-limits/:id/disable` | 禁用限流 |

支持维度：

-   Global
-   Tenant
-   Domain
-   Service
-   IP

后端（v0.1.18）：默认单机内存滑动窗口；`MAPLE_RATE_LIMIT_MODE=redis` 时走 Redis 固定窗口（INCR+EXPIRE，键含 scope+窗口桶，跨实例共享配额），Redis 异常 fail-open 降级，主链路不断。

------------------------------------------------------------------------

# 二十一、Metrics

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/metrics` | Prometheus Metrics |
| GET | `/api/admin/metrics/overview` | 管理后台指标总览 |
| GET | `/api/admin/metrics/traffic` | 流量指标 |
| GET | `/api/admin/metrics/latency` | 延迟指标 |
| GET | `/api/admin/metrics/errors` | 错误指标 |
| GET | `/api/admin/metrics/tenants/:id` | Tenant 指标 |
| GET | `/api/admin/metrics/services/:id` | Service 指标 |
| GET | `/api/admin/metrics/instances/:id` | Instance 指标 |

------------------------------------------------------------------------

# 二十二、日志系统 (Logs)

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/logs/access` | Access Log（v0.1.18 支持 `?request_id=` 过滤） |
| GET | `/api/admin/logs/error` | Error Log（v0.1.18 支持 `?request_id=` 过滤） |
| GET | `/api/admin/logs/routing` | Routing Log |
| GET | `/api/admin/logs/health` | Health Log |
| GET | `/api/admin/logs/security` | Security Log |
| GET | `/api/admin/logs/audit` | Audit Log |

支持筛选：

-   Tenant
-   Domain
-   Service
-   Instance
-   Node
-   Status Code
-   时间范围

------------------------------------------------------------------------

# 二十三、系统配置 (Settings)

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/settings` | 获取 Gateway 配置 |
| PATCH | `/api/admin/settings/gateway` | Gateway 基础配置 |
| PATCH | `/api/admin/settings/proxy` | Proxy 配置 |
| PATCH | `/api/admin/settings/health` | Health Check 默认配置 |
| PATCH | `/api/admin/settings/security` | 安全配置 |
| PATCH | `/api/admin/settings/logging` | 日志配置 |
| PATCH | `/api/admin/settings/metrics` | Metrics 配置 |

动态配置修改后应触发：

配置校验 → 保存 → Route / Runtime Config 更新

不要求重启 Maple Gateway。

------------------------------------------------------------------------

# 二十四、Cloudflare 配置

用于 Maple Gateway 与 Cloudflare for SaaS 的平台配置管理。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/cloudflare/settings` | 获取 Cloudflare 配置状态 |
| PATCH | `/api/admin/cloudflare/settings` | 修改 Cloudflare 集成配置 |
| GET | `/api/admin/cloudflare/domains/:id/status` | 获取 Custom Hostname 状态 |
| POST | `/api/admin/cloudflare/domains/:id/sync` | 同步 Domain 状态 |

Cloudflare API 凭据等敏感信息不得通过普通列表接口明文返回。

------------------------------------------------------------------------

# 二十五、系统维护 (System)

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/system/health` | Maple Gateway 健康检查 |
| GET | `/api/system/ready` | Readiness Check |
| GET | `/api/system/live` | Liveness Check |
| GET | `/api/admin/system/info` | 系统信息 |
| GET | `/api/admin/system/version` | 当前版本 |
| GET | `/api/admin/system/runtime` | Go Runtime 信息 |
| POST | `/api/admin/system/reload` | 重载动态配置 |

------------------------------------------------------------------------

# 二十六、内部 Container Manager API

这一组接口是 Maple Gateway 与 Container Manager 的核心边界。

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/internal/nodes/register` | Container Manager 注册 Node |
| PATCH | `/api/internal/nodes/:id` | 更新 Node |
| DELETE | `/api/internal/nodes/:id` | 注销 Node |
| POST | `/api/internal/nodes/:id/heartbeat` | Node 心跳 |
| POST | `/api/internal/instances/register` | 注册 Container Instance |
| PATCH | `/api/internal/instances/:id` | 更新 Instance |
| DELETE | `/api/internal/instances/:id` | 注销 Instance |
| POST | `/api/internal/instances/:id/health` | 更新 Instance Health |
| POST | `/api/internal/instances/:id/drain` | 通知 Instance 进入 Draining |
| POST | `/api/internal/sync` | 全量同步 Node / Instance 状态 |

Maple Gateway 不提供：

-   create-container
-   delete-container
-   pull-image
-   start-container
-   stop-container

这些接口属于 Container Manager。

------------------------------------------------------------------------

# 二十七、事件模型 (Internal Events)

除 REST API 外，Maple Gateway 内部应围绕状态事件更新 Route Cache。

建议事件：

| 事件 | 说明 |
|------|------|
| `DomainBound` | Domain 已绑定 |
| `DomainUnbound` | Domain 已解绑 |
| `TenantUpdated` | Tenant 状态变化 |
| `ServiceUpdated` | Service 配置变化 |
| `DeploymentUpdated` | Deployment 变化 |
| `VersionPublished` | 新 Version 发布 |
| `InstanceRegistered` | Instance 注册 |
| `InstanceUpdated` | Instance 状态变化 |
| `InstanceHealthy` | Instance 健康 |
| `InstanceUnhealthy` | Instance 异常 |
| `InstanceDraining` | Instance 开始排空 |
| `InstanceRemoved` | Instance 注销 |
| `NodeOnline` | Node 在线 |
| `NodeOffline` | Node 离线 |
| `TrafficPolicyUpdated` | 流量策略变化 |

事件最终更新：

PostgreSQL / Runtime State → Memory Route Cache

------------------------------------------------------------------------

# 二十八、API 安全边界

Maple Gateway API 分为三类。

| API 类型 | 路径 | 访问范围 |
|----------|------|----------|
| System API | `/api/system/*` | 健康检查 / 平台探针 |
| Admin API | `/api/admin/*` | Maple Gateway 管理后台 |
| Internal API | `/api/internal/*` | Container Manager / Node Agent |

原则：

1.  `/api/admin/*` 必须认证（Bearer JWT）。
2.  `/api/internal/*` 不对公网开放。
3.  Internal API 使用独立认证机制（与 RBAC 正交）。
4.  Data Plane 的 80 / 443 与 Management API 端口分离。
5.  PostgreSQL / Redis 不暴露公网。
6.  Cloudflare API Token 不写入日志。
7.  Instance Upstream 地址必须校验，防止 SSRF。
8.  所有管理操作写入 Audit Log。
9.  RBAC（v0.1.18）：`/api/admin/*` 经角色授权 —— super_admin 全量 / operator 资源配置与发布 / viewer 只读；越权 403 且写 audit（DENY）。

------------------------------------------------------------------------

# 二十九、接口优先级

| 优先级 | 模块 |
|--------|------|
| P0 | Auth、Tenant、Domain、Service、Instance、Route、Route Cache、Health Check、Load Balancing、System |
| P1 | Node、Deployment、Traffic Policy、Canary、Failover、Rate Limit、Metrics、Logs、Container Manager Internal API |
| P2 | Blue / Green、Rolling Update 高级控制、Cloudflare 集成 |

> v0.1.18 已交付：自动 Canary（/api/admin/canary 之上叠加自动执行器，MAPLE_CANARY_AUTO_ENABLED）、HA（/api/admin/ha 实例/Leader 查看）、分布式限流 Redis、Request ID / OTel、RBAC。均从"后续/P2"移至已实现。

------------------------------------------------------------------------

# 三十、Phase 1 MVP API

MVP 优先实现：

``` text
/api/auth/*
/api/admin/tenants/*
/api/admin/domains/*
/api/admin/services/*
/api/admin/instances/*
/api/admin/routes/*
/api/admin/cache/*
/api/admin/health-checks/*
/api/admin/services/:id/load-balancing
/api/system/health
/api/system/ready
/api/system/live
```

以及最基础的 Internal API：

``` text
/api/internal/instances/register
PATCH /api/internal/instances/:id
DELETE /api/internal/instances/:id
```

MVP 目标：

> 完成 Domain → Tenant → Service → Healthy Instance → Reverse Proxy
> 的完整闭环。

------------------------------------------------------------------------

# Maple Gateway API 设计原则

1.  Go 统一负责 Gateway 后端与 Management API。
2.  React Frontend 只调用 `/api/admin/*`，不参与 Data Plane。
3.  Data Plane 与 Management API 使用独立监听端口。
4.  Domain → Tenant → Service → Deployment → Version → Instance → Node
    是核心资源模型。
5.  请求转发主链路读取 Memory Route Cache，不逐请求查询 PostgreSQL。
6.  Tenant、Domain、Service 等配置变化必须自动同步 Route Cache。
7.  Load Balancer 只选择 Healthy、Enabled、非 Draining Instance。
8.  Failover 自动执行，Admin API 主要用于查看与人工干预。
9.  Canary、Blue / Green、Rolling Update 都基于 Version + Traffic
    Policy。
10. Maple Gateway 只控制流量，不负责 Container 生命周期。
11. Container Manager 通过 `/api/internal/*` 同步 Node / Instance 状态。
12. `/api/internal/*` 与 `/api/admin/*` 使用不同的安全边界。
13. Management API 的修改操作应具备幂等性与 Audit Log。
14. 所有列表 API 支持分页、筛选和排序。
15. 所有时间统一使用 UTC 存储，API 使用 ISO 8601。
16. API 错误响应采用统一结构。
17. 动态路由和流量配置修改不要求 Gateway reload。
18. MVP 优先完成核心转发闭环，不提前实现复杂 Kubernetes 式调度 API。
