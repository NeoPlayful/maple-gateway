# Phase 1 开发计划 — MVP 核心转发闭环

> **对应项目**：Maple Gateway（Multi-Tenant Gateway）
> **技术栈**：Go 1.24+ / net/http Reverse Proxy / Fiber v3（Management API）/ PostgreSQL 16 / Redis 7（辅助）/ Memory Route Cache / Docker
> **文档状态**：v1.0-plan（开发执行依据，随实现推进勾选与修订）
>
> **配套文档**：[项目功能全面分析.md](./项目功能全面分析.md) · [技术栈选型与开发准备清单.md](./技术栈选型与开发准备清单.md) · [项目目录结构.md](./项目目录结构.md) · [API接口全面分析.md](./API接口全面分析.md)

---

## 一、Phase 1 目标

> **实例能注册 → 域名能绑定 → 请求按 Host 路由 → 命中健康实例 → 反向代理转发 → 故障自动摘除 → 管理 API 可配置**

Phase 1 只做 Multi-Tenant Gateway 最核心的**数据平面闭环**与最小可用的**控制平面**：

```text
Client → Maple Gateway → Healthy Instance(Container) → 响应返回
         （Host 识别 → Tenant → Service → Instance → Reverse Proxy）
```

### 核心原则

| 原则 | 说明 |
|------|------|
| **P0 优先** | 只做 Gateway Core、Reverse Proxy、Domain/Tenant/Service/Instance 路由、Route Cache、LB、Health、Failover、Management API |
| **数据平面优先** | 先用静态路由验证转发能力，再逐步引入数据库与控制平面 |
| **请求不查库** | 主链路读 Memory Route Cache，不逐请求查询 PostgreSQL |
| **不做发布控制** | Canary / Blue-Green / Rolling Update / Traffic Policy 归 Phase 2 |
| **不做多 Node** | 单 Node + 多 Instance 起步，多 Node / Container Manager 归 Phase 2 |
| **不做管理前端** | React Admin 归 Phase 2，Phase 1 用 Management API + curl 验证 |
| **不做 Nginx** | Maple Gateway 自身承担 Reverse Proxy，不强制依赖 Nginx |
| **不建容器** | Container 生命周期归 Container Manager，Phase 1 用 compose 测试容器模拟 |
| **Docker 优先** | 全程 Docker Compose 开发与部署，同时保留 Linux Binary 入口 |
| **与业务解耦** | 只做流量入口与转发，不做任何业务逻辑 |

------------------------------------------------------------------------

## 二、Phase 1 范围边界

### 2.1 本阶段要做（P0）

| 能力 | 说明 |
|------|------|
| Gateway Core | HTTP/HTTPS 接入、请求生命周期、优雅关闭 |
| Reverse Proxy | HTTP/1.1、HTTP/2、WebSocket、SSE、Streaming、Header 处理 |
| Tenant Routing | Host → Domain → Tenant → Service → Healthy Instance |
| Route Cache | 内存路由表，配置变更后自动同步 |
| Service Discovery | Instance 动态注册 / 更新 / 注销 |
| Load Balancing | Round Robin + Weighted Round Robin |
| Health Check | HTTP / TCP 主动健康检查，阈值状态机 |
| Failover | 异常实例自动摘除、恢复后重新加入 |
| Management API | Auth + Tenant/Domain/Service/Instance + Route/Cache + 系统接口 |
| 基础日志 | Access / Error / Routing 日志 |
| 部署 | Docker Compose + Linux Binary（systemd） |

### 2.2 本阶段不做（延后）

| 能力 | 归属阶段 | 说明 |
|------|----------|------|
| Traffic Policy / Canary / Blue-Green / Rolling Update | Phase 2 | 基于 Version 的流量控制 |
| 多 Node / Container Manager 集成 | Phase 2 | Node 生命周期与实例状态外部同步 |
| Node / Deployment / Version 管理 API | Phase 2 | 本阶段 Instance 直挂 Service |
| Rate Limit | Phase 2 | Tenant / Domain / Service / IP 限流 |
| Metrics / Prometheus 采集 | Phase 2 | 本阶段仅输出基础日志 |
| Admin Dashboard（React frontend） | Phase 2 | 本阶段前端目录不建代码 |
| 完整 RBAC / 用户体系 / Cloudflare 集成 | Phase 2 | 本阶段仅管理员 Token 认证 |

> 范围依据：`API接口全面分析.md` 第三十节（Phase 1 MVP API）与 `项目功能全面分析.md` 第二十七节（Phase 1 MVP）。

------------------------------------------------------------------------

## 三、Phase 1 开发阶段划分

对应 `技术栈选型与开发准备清单.md`「二十三、MVP 开发顺序」的六步展开。

```text
步骤1  Host + 静态路由 + Reverse Proxy + Container
步骤2  Domain → Tenant → Service → Instance
步骤3  Memory Route Cache + PostgreSQL + Management API
步骤4  多 Instance + LB + Health Check + Failover
步骤5  Container Manager 集成 + 多 Node   ← Phase 2
步骤6  Traffic Policy + Canary            ← Phase 2
```

| 阶段 | 内容 | 涉及模块 | 预期工时 |
|------|------|----------|---------|
| **S1 基础设施** | Go 项目脚手架、配置加载、日志、Docker 环境 | 全项目 | 1-2 天 |
| **S2 数据平面** | 静态路由 + Reverse Proxy（HTTP/WebSocket/SSE/流式） | gateway + proxy + router | 3-4 天 |
| **S3 数据模型** | PostgreSQL + 迁移 + 核心表 + seed | tenant/domain/service/instance + pkg/database | 2-3 天 |
| **S4 路由缓存** | Memory Route Table + 配置同步 + 事件模型 | cache + discovery | 2-3 天 |
| **S5 管理 API** | Auth + CRUD + Route/Cache 查看 + 系统接口 | auth + api + 各模块 handler/service | 3-4 天 |
| **S6 路由引擎** | Host 规范化、边界处理、与数据平面打通 | router + security | 2 天 |
| **S7 弹性能力** | Round Robin / Weighted、Health Check、Failover | loadbalancer + health + failover | 2-3 天 |
| **S8 收尾部署** | Docker / Linux 部署、性能与边界验证、race 测试 | gateway + 全项目 | 1-2 天 |

> **合计约 16-23 天（单人开发）**，建议按顺序推进，每个阶段产出可验证成果。

------------------------------------------------------------------------

## 四、数据模型设计

### 4.1 Phase 1 模型总览

```text
Tenant            租户（多租户第一层）
Domain            自定义域名 → Tenant 映射
Service           Tenant 下的应用服务（如 storefront / api）
Instance          实际 Container 实例
HealthCheck       健康检查配置（挂 Service 或全局默认）
Admin             管理员账号（Management API 登录）
AuditLog          管理操作审计
```

核心关系：

```text
Domain
  ↓
Tenant
  ↓
Service
  ↓
Instance
```

### 4.2 各模型核心字段

> Phase 2 引入 Deployment / DeploymentVersion / Node / TrafficPolicy / RateLimit / GatewayConfig 后，
> Instance 再挂上 deployment_id / version_id / node_id。Phase 1 预留 `version` 字符串字段即可。

#### Tenant

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID | 主键 |
| name | String | 租户名称 |
| slug | String | 唯一标识，如 tenant_1001 |
| status | Enum | active / suspended / disabled |
| description | String? | 描述 |
| createdAt | DateTime | |
| updatedAt | DateTime | |

#### Domain

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID | 主键 |
| tenantId | UUID | 关联 Tenant |
| hostname | String | 域名（小写、可含 punycode），唯一 |
| serviceId | UUID? | 默认 Service（Host 命中后转发到该 Service） |
| status | Enum | active / disabled / pending |
| verifiedAt | DateTime? | 验证通过时间（Cloudflare 归 Phase 2） |
| createdAt | DateTime | |
| updatedAt | DateTime | |

#### Service

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID | 主键 |
| tenantId | UUID | 关联 Tenant |
| name | String | 服务名（租户内唯一），如 storefront / api |
| protocol | Enum | http / https（upstream 协议） |
| status | Enum | active / disabled |
| createdAt | DateTime | |
| updatedAt | DateTime | |

#### Instance

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID | 主键 |
| serviceId | UUID | 关联 Service |
| nodeId | UUID? | Phase 1 默认本机节点，Phase 2 关联 Node |
| version | String? | 版本标签，Phase 2 金丝雀复用 |
| address | String | 容器内部 IP / Host |
| port | Int | 容器端口 |
| protocol | Enum | http / https |
| weight | Int | 权重，默认 1 |
| status | Enum | enabled / disabled / draining |
| health | Enum | unknown / healthy / unhealthy / recovering |
| lastSeenAt | DateTime? | 最近一次心跳 / 健康检查成功时间 |
| createdAt | DateTime | |
| updatedAt | DateTime | |

#### HealthCheck

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID | 主键 |
| serviceId | UUID? | 为空表示全局默认配置 |
| type | Enum | http / tcp |
| path | String? | HTTP 检查路径，默认 /health |
| interval | Int | 检查间隔（秒） |
| timeout | Int | 超时（秒） |
| failureThreshold | Int | 连续失败次数触发摘除 |
| successThreshold | Int | 连续成功次数恢复 |
| gracePeriod | Int | 实例启动宽限期（秒） |
| createdAt | DateTime | |
| updatedAt | DateTime | |

#### Admin

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID | 主键 |
| email | String | 唯一，登录名 |
| passwordHash | String | bcrypt 哈希 |
| name | String | 显示名 |
| status | Enum | active / disabled |
| createdAt | DateTime | |
| updatedAt | DateTime | |

#### AuditLog

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID | 主键 |
| adminId | UUID? | 操作人 |
| action | String | 如 tenant.create / instance.deregister |
| targetType | String | 对象类型 |
| targetId | String? | 对象 ID |
| detail | Json? | 操作详情（敏感字段脱敏） |
| ip | String? | 操作 IP |
| createdAt | DateTime | |

### 4.3 数据访问原则

```text
Management API 修改配置
        ↓
PostgreSQL（持久化）
        ↓
配置同步（增量 + 全量）
        ↓
Memory Route Table
        ↓
请求 O(1) / 低成本查找
```

- 数据平面**永不**逐请求查询 PostgreSQL。
- Redis 是辅助组件，Phase 1 只用于可选缓存，不作为强制依赖。
- 数据库异常时，Route Cache 保留最后有效配置继续服务。

------------------------------------------------------------------------

## 五、S1 — 基础设施与项目脚手架

### 5.1 项目初始化

- [ ] 初始化 `server/` — Go 1.24+ module，入口 `cmd/server/main.go`
- [ ] 初始化 `frontend/` 占位说明（Phase 2 再建 React，本阶段不生成代码）
- [ ] 配置 `docker-compose.yml`（maple-gateway + postgres + redis + 测试容器）
- [ ] 编写 `server/configs/config.example.yaml` 与 `config.yaml`
- [ ] 编写 `.env.example`（数据库 / Redis / Admin Token / 日志级别）

### 5.2 配置加载

```yaml
# server/configs/config.yaml
gateway:
  http:
    address: ":80"
  https:
    address: ":443"        # Phase 1 可不开 TLS，保留配置位
management:
  address: ":4000"
health:
  interval: 10s
  timeout: 3s
database:
  driver: postgres
redis:
  enabled: false            # Phase 1 默认关闭
logging:
  level: info
```

环境变量覆盖敏感配置：

```text
MAPLE_DATABASE_URL
MAPLE_REDIS_URL
MAPLE_ADMIN_TOKEN
MAPLE_LOG_LEVEL
MAPLE_GATEWAY_HTTP_ADDR
MAPLE_MANAGEMENT_ADDR
```

### 5.3 后端基础架构

| 文件 | 说明 |
|------|------|
| `pkg/database.go` | pgx 连接池初始化 |
| `pkg/redis.go` | Redis 连接（可选，封装开关） |
| `pkg/response.go` | 统一响应 `{code, message, data, meta}` |
| `pkg/logger.go` | Zap 日志初始化 |
| `pkg/validator.go` | 参数校验 |
| `pkg/errors.go` | 公共错误定义 + 错误码 |
| `configs/config.yaml` | 启动配置 |

### 5.4 端口规划（权威口径见 `项目目录结构.md`）

| 服务 | 端口 | 说明 |
|------|------|------|
| Maple Gateway HTTP | 80 | 数据平面 HTTP |
| Maple Gateway HTTPS | 443 | 数据平面 HTTPS |
| Management API | 4000 | 控制面 API（限内网） |
| Metrics | 9090 | Prometheus（Phase 2 使用） |
| PostgreSQL | 5432 | 数据库 |
| Redis | 6379 | 辅助缓存 |
| Admin Dev | 5173 | React 开发（Phase 2） |

### 5.5 验证

- [ ] `go build ./cmd/server` 成功
- [ ] 配置可被 YAML + ENV 覆盖加载
- [ ] docker compose 能拉起 postgres + redis + 测试容器
- [ ] 统一响应结构与错误码在空接口上可用

------------------------------------------------------------------------

## 六、S2 — 反向代理数据平面

> 目标：**不依赖数据库**，先用配置文件里的静态路由，验证 Maple Gateway 能否把一个请求正确转发到测试容器，并支持 WebSocket / SSE / 流式。

### 6.1 模块与文件

| 文件 | 说明 |
|------|------|
| `internal/gateway/server.go` | HTTP Server 组装、生命周期 |
| `internal/gateway/listener.go` | 80/443 监听与端口分离 |
| `internal/gateway/shutdown.go` | 优雅关闭（连接排空） |
| `internal/proxy/proxy.go` | Reverse Proxy 主入口 |
| `internal/proxy/transport.go` | Upstream Transport（keep-alive、超时） |
| `internal/proxy/headers.go` | Header 处理（X-Forwarded-* / Host 策略） |
| `internal/proxy/websocket.go` | WebSocket 升级转发 |
| `internal/proxy/streaming.go` | 流式 / 分块 / SSE 转发 |
| `internal/router/route.go` | 静态路由结构（本阶段由配置填充） |

### 6.2 转发核心

代理核心与 Management API 解耦，直接基于 Go 标准库：

```text
net/http
    ↓
Proxy Engine（httputil.ReverseProxy 封装）
    ↓
Routing Engine（Host → upstream）
```

不采用：

```text
所有代理流量 → Fiber Business Handler → 再做代理
```

### 6.3 必须验证的能力

- [ ] HTTP/1.1 转发
- [ ] HTTP/2（h2c / 对外协商，视部署方式）
- [ ] WebSocket 升级代理
- [ ] SSE 长连接代理
- [ ] Chunked Response
- [ ] Streaming Request / Streaming Response
- [ ] Large Request / Response Body
- [ ] Keep-Alive
- [ ] Upstream Timeout
- [ ] Client Disconnect（取消传播）
- [ ] Context Cancel 传播
- [ ] Header 透传与重写
- [ ] X-Forwarded-For / X-Forwarded-Proto 处理
- [ ] Host Header 策略（透传 / 重写）
- [ ] CF-Connecting-IP 识别（Cloudflare 场景预留）

### 6.4 边界状态处理

| 场景 | 处理 |
|------|------|
| Upstream 连接失败 | 记录错误，返回 502/504，可切换其他实例（S7 接入） |
| 未匹配任何路由 | 返回 404 / 默认站点响应 |
| 请求头过大 | 限制并返回 431 |
| Body 过大 | 可配置限制，超限返回 413 |
| 客户端断开 | 取消上游请求，释放连接 |
| Upstream 长时间无响应 | 超时后返回 504 |

### 6.5 验证

- [ ] 静态配置一个 Host → 测试容器，curl 能完整拿到响应
- [ ] WebSocket 客户端能连通并双向收发
- [ ] SSE 能持续推送
- [ ] 大文件上传下载不断流
- [ ] 断开客户端后上游连接被取消

------------------------------------------------------------------------

## 七、S3 — 数据模型与 PostgreSQL 存储

> 目标：引入 PostgreSQL，把 Domain / Tenant / Service / Instance 变成**持久化配置**。

### 7.1 模块与文件

| 文件 | 说明 |
|------|------|
| `migrations/0001_init.sql` | Phase 1 核心表建表 |
| `internal/tenant/model.go` + `repository.go` | Tenant 模型与数据访问 |
| `internal/domain/model.go` + `repository.go` | Domain 模型与数据访问 |
| `internal/service/model.go` + `repository.go` | Service 模型与数据访问 |
| `internal/instance/model.go` + `repository.go` | Instance 模型与数据访问 |
| `internal/auth/service.go` | 管理员登录与 Token（S5 使用） |

### 7.2 迁移清单

- [ ] 表：`tenants` / `domains` / `services` / `instances`
- [ ] 表：`health_checks` / `admins` / `audit_logs`
- [ ] 唯一约束：`domains.hostname`、`services(tenant_id, name)`、`admins.email`
- [ ] 索引：`instances(service_id)`、`instances(health)`、`domains(hostname)`
- [ ] 外键与删除策略（Tenant 删除时级联/拒绝的策略明确并实现）
- [ ] Seed：默认管理员账号（从环境变量读取初始密码）

### 7.3 数据访问

- [ ] `pkg/database.go` 用 pgx 建立连接池
- [ ] 所有 repository 只被 service 层调用，handler 不直接碰 SQL
- [ ] 迁移脚本可重复执行（迁移版本表）

### 7.4 验证

- [ ] `docker compose` 启动后自动执行迁移，表结构正确
- [ ] 可对 Tenant / Domain / Service / Instance 做基础 CRUD（先用临时接口或 SQL 验证）
- [ ] 默认管理员账号可登录（S5 联调）

------------------------------------------------------------------------

## 八、S4 — Memory Route Cache 与配置同步

> 目标：请求主链路脱离数据库，改为读内存路由表；配置变更后增量/全量同步。

### 8.1 模块与文件

| 文件 | 说明 |
|------|------|
| `internal/cache/route.go` | 路由表结构（Domain → Route Entry） |
| `internal/cache/tenant.go` | Tenant 状态缓存 |
| `internal/cache/instance.go` | Instance 池缓存 |
| `internal/cache/sync.go` | 全量重建 / 增量刷新 / 事件订阅 |

### 8.2 路由表结构

```text
hostname
   ↓  （规范化后）
Route Entry:
  tenant:   { id, status }
  service:  { id, protocol, default upstream info }
  pool:     [ { instance id, address:port, weight, health, status } ]
```

### 8.3 配置同步机制

```text
写入变更（Management API）
        ↓
DB 事务提交
        ↓
发布事件（如 InstanceRegistered / TenantUpdated）
        ↓
Cache Sync 订阅 → 更新内存路由表（原子替换）
```

| 同步类型 | 触发时机 |
|----------|----------|
| 全量重建 | 启动、`POST /api/admin/cache/rebuild`、订阅断线重连 |
| 增量更新 | 任一配置/实例状态变更事件 |
| 兜底 | DB 异常时保留最后有效路由表 |

### 8.4 验证

- [ ] 启动后自动从 PostgreSQL 全量加载路由
- [ ] 新增 Domain 后无需重启即可命中新路由
- [ ] Instance 注销后从池中移除
- [ ] 停掉 DB 后已有请求仍能正常转发（读缓存兜底）
- [ ] 并发读写路由表无数据竞争（race 测试）

------------------------------------------------------------------------

## 九、S5 — Management API 控制面

> 目标：提供最小可用控制面，让配置可以通过 API 管理，并自动同步到 Route Cache。

### 9.1 认证（Auth）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/auth/login` | 管理员登录（email + password），返回 Bearer Token |
| GET | `/api/auth/me` | 获取当前管理员信息 |
| POST | `/api/auth/logout` | 退出登录 |

> Phase 1 使用管理员账号（`admins` 表）+ Bearer Token 中间件即可；Refresh / 改密 / RBAC 归 Phase 2。
> Internal API 采用独立 Token（`MAPLE_ADMIN_TOKEN` 派生或单独配置），与 Admin API 分离。

### 9.2 系统接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/system/health` | 健康检查 |
| GET | `/api/system/ready` | Readiness（DB/缓存是否就绪） |
| GET | `/api/system/live` | Liveness |

### 9.3 Tenant 管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/tenants` | Tenant 列表（分页 + 筛选） |
| POST | `/api/admin/tenants` | 创建 Tenant |
| GET | `/api/admin/tenants/:id` | Tenant 详情 |
| PATCH | `/api/admin/tenants/:id` | 修改 Tenant |
| DELETE | `/api/admin/tenants/:id` | 删除 Tenant（有 Domain/Service 时拒绝） |
| POST | `/api/admin/tenants/:id/enable` | 启用 |
| POST | `/api/admin/tenants/:id/disable` | 禁用 |
| POST | `/api/admin/tenants/:id/suspend` | 暂停 |

### 9.4 Domain 管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/domains` | Domain 列表 |
| POST | `/api/admin/domains` | 添加 Domain |
| GET | `/api/admin/domains/:id` | Domain 详情 |
| PATCH | `/api/admin/domains/:id` | 修改 Domain（含默认 Service） |
| DELETE | `/api/admin/domains/:id` | 删除 Domain |
| POST | `/api/admin/domains/:id/enable` | 启用 |
| POST | `/api/admin/domains/:id/disable` | 禁用 |

### 9.5 Service 管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/services` | Service 列表 |
| POST | `/api/admin/services` | 创建 Service |
| GET | `/api/admin/services/:id` | Service 详情 |
| PATCH | `/api/admin/services/:id` | 修改 Service |
| DELETE | `/api/admin/services/:id` | 删除 Service（有 Instance 时拒绝） |
| POST | `/api/admin/services/:id/enable` | 启用 |
| POST | `/api/admin/services/:id/disable` | 禁用 |

### 9.6 Instance 管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/instances` | Instance 列表 |
| GET | `/api/admin/instances/:id` | Instance 详情 |
| POST | `/api/admin/instances/register` | 注册 Instance |
| PATCH | `/api/admin/instances/:id` | 更新 Instance |
| DELETE | `/api/admin/instances/:id` | 注销 Instance |
| POST | `/api/admin/instances/:id/enable` | 允许接收流量 |
| POST | `/api/admin/instances/:id/disable` | 禁止接收流量 |
| POST | `/api/admin/instances/:id/drain` | 进入 Connection Draining |
| POST | `/api/admin/instances/:id/undrain` | 取消 Draining |
| GET | `/api/admin/instances/:id/health` | 健康状态 |

### 9.7 Route / Cache 查看

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/routes` | 当前生效 Route 列表 |
| GET | `/api/admin/routes/domain/:hostname` | 按 Domain 查路由 |
| POST | `/api/admin/routes/reload` | 重新构建 Route Cache |
| GET | `/api/admin/cache/status` | Route Cache 状态 |
| GET | `/api/admin/cache/stats` | Hit / Miss 统计 |

> 正常情况下配置变更自动同步，Cache API 仅用于排障与手动重建。

### 9.8 统一响应与错误

```text
成功: { "code": 0, "message": "ok", "data": ..., "meta": {分页} }
失败: { "code": "VALIDATION_ERROR", "message": "xxx", "data": null }
```

| 错误码 | HTTP 状态码 | 说明 |
|--------|-----------|------|
| UNAUTHORIZED | 401 | 未登录 / Token 无效或过期 |
| FORBIDDEN | 403 | 无权限 |
| VALIDATION_ERROR | 400 | 参数校验失败 |
| NOT_FOUND | 404 | 资源不存在 |
| CONFLICT | 409 | 冲突（hostname 重复、删除被引用对象） |
| RATE_LIMITED | 429 | 请求过于频繁（Phase 2 启用） |
| SYSTEM_ERROR | 500 | 系统异常 |

### 9.9 边界状态处理

| 场景 | 处理 |
|------|------|
| 未携带 Token 访问 `/api/admin/*` | 401 |
| Token 无效 | 401，前端清除登录态（Phase 2 前端实现） |
| hostname 已存在 | 409 Conflict |
| 删除有 Domain 的 Tenant | 400，提示先解绑 |
| 删除有 Instance 的 Service | 400，提示先注销实例 |
| 创建 Tenant 但 slug 冲突 | 409 |

### 9.10 验证

- [ ] 登录成功返回 Token，携带 Token 可访问受保护接口
- [ ] Tenant / Domain / Service / Instance 完整 CRUD
- [ ] 创建 Domain 后路由表实时新增该 Host
- [ ] 禁用 Tenant 后该 Host 请求被拦截
- [ ] 所有修改操作写入 AuditLog

------------------------------------------------------------------------

## 十、S6 — 多租户路由引擎与边界处理

> 目标：把 S2 的静态路由升级为**基于 Route Cache 的动态路由**，并补齐 Host 规范化等边界。

### 10.1 模块与文件

| 文件 | 说明 |
|------|------|
| `internal/router/router.go` | 路由引擎入口（每请求调用） |
| `internal/router/resolver.go` | Host → Route Entry 解析 |
| `internal/router/matcher.go` | 状态与匹配检查 |
| `internal/domain/normalize.go` | Host 规范化 |
| `internal/security/host.go` | Host 校验与非法 Host 拒绝 |
| `internal/security/trusted_proxy.go` | 可信代理 / Client IP 解析 |

### 10.2 路由流程

```text
Request
   ↓
Normalize Host
   ↓
Host 校验（拒绝非法 Host）
   ↓
Domain Lookup（Route Cache）
   ↓
Tenant 状态检查（active 放行）
   ↓
Service 定位
   ↓
Healthy Instance Pool
   ↓
Load Balancer（S7 接入）
   ↓
Reverse Proxy
```

### 10.3 必须处理

| 场景 | 处理 |
|------|------|
| Host 大小写 | 统一小写后查找 |
| Host + Port | 剥离端口再匹配 |
| IDN / Punycode | 归一化为 punycode 后匹配 |
| www / apex 独立 | 视为不同 hostname，按绑定关系路由 |
| 未绑定域名 | 返回 404 或默认站点 |
| Disabled Domain | 拒绝该 Host |
| Suspended / Disabled Tenant | 返回 403 页 / 503 |
| Service 不存在 | 返回 404 |
| 无 Healthy Instance | 返回 503，不转发 |
| Route Cache Miss | 记录 miss（本阶段不落库查询，走 rebuild 提示） |
| 空 Host / 畸形 Host | 拒绝 |

### 10.4 验证

- [ ] shop-a.com 与 shop-b.com 各自路由到对应测试容器
- [ ] 域名大小写、带端口访问均正确路由
- [ ] 未绑定域名返回 404
- [ ] 禁用 Domain / Tenant 后对应 Host 被拦截
- [ ] Cache Miss 路径不崩溃且有日志

------------------------------------------------------------------------

## 十一、S7 — 负载均衡、健康检查与故障摘除

> 目标：一个 Service 有多个健康实例时能分配流量，实例故障自动摘除、恢复后重新加入。

### 11.1 模块与文件

| 文件 | 说明 |
|------|------|
| `internal/loadbalancer/balancer.go` | 实例选择接口 |
| `internal/loadbalancer/round_robin.go` | Round Robin |
| `internal/loadbalancer/weighted.go` | Weighted Round Robin |
| `internal/health/checker.go` | 主动健康检查调度 |
| `internal/health/http.go` | HTTP 健康检查 |
| `internal/health/tcp.go` | TCP 健康检查 |
| `internal/health/state.go` | 状态机与阈值 |
| `internal/failover/service.go` | 摘除 / 恢复逻辑 |

### 11.2 负载均衡策略

MVP：

- Round Robin
- Weighted Round Robin（按 Instance weight）

Phase 2：

- Sticky Session
- Consistent Hash
- Least Connections

选择规则：

> Load Balancer 只从 `healthy + enabled + 非 draining` 的 Instance 中选择。

### 11.3 健康检查

```text
Maple Gateway
     ↓（按 interval 主动探测）
GET {path} / TCP Connect
     ↓
Container
```

状态机：

```text
Unknown
  ↓
Healthy
  ↓
Unhealthy（连续 failureThreshold 次）
  ↓
Recovering（连续 successThreshold 次）
  ↓
Healthy
```

| 配置 | 说明 |
|------|------|
| Interval | 检查间隔 |
| Timeout | 单次探测超时 |
| Failure Threshold | 连续失败次数达到后摘除 |
| Success Threshold | 连续成功次数达到后恢复 |
| Grace Period | 注册后宽限期，避免启动抖动误摘除 |

原则：**避免单次失败立即摘除实例**。

### 11.4 故障摘除 / 恢复

```text
Instance A 异常
  ↓ 连续失败达阈值
标记 Unhealthy → 移出路由池 → 权重 0
  ↓
其他 Healthy Instance 承接流量
  ↓
A 恢复（连续成功达阈值）→ 重新加入路由池
```

- 健康检查判定 Instance 自身失败（连不上 / 超时 / 5xx 统计）由被动检查辅助确认（Phase 1 以主动检查为主）。
- 无任何 Healthy Instance 时返回 503 + 可配置错误页。
- Instance 的 enable/disable/drain 状态由 Management API / Internal API 控制。

### 11.5 验证

- [ ] 同一 Service 两个实例按 RR 交替转发
- [ ] 权重 2:1 的实例流量比例接近 2:1
- [ ] 停掉一个实例，请求自动全部落到健康实例
- [ ] 恢复该实例后重新进入池
- [ ] 全挂时返回 503
- [ ] 单次探测失败不会立即摘除（阈值生效）

------------------------------------------------------------------------

## 十二、S8 — 部署、测试与验证收尾

### 12.1 Docker 部署

- [ ] `server/Dockerfile` 多阶段构建（Go 编译 + scratch/distroless 运行）
- [ ] `docker-compose.yml` 编排 gateway + postgres + redis（可选）+ 测试容器
- [ ] 数据平面 80 与 Management API 4000 端口分离
- [ ] 生产注意：4000 / 5432 / 6379 不对公网开放

### 12.2 Linux 原生部署

```text
/usr/local/bin/maple-gateway
/etc/maple-gateway/config.yaml
/var/lib/maple-gateway/
/var/log/maple-gateway/
```

- [ ] 提供 systemd unit（Start / Stop / Restart / Auto Restart / Boot Startup）
- [ ] 开发与生产环境均不被 Docker 强绑定

### 12.3 测试与质量

- [ ] `go test ./...` 全绿
- [ ] `go test -race ./...` 无数据竞争
- [ ] 基础单元测试：Host 规范化、路由解析、LB 选择、健康状态机、错误码
- [ ] 关键集成测试：注册实例 → 创建 Domain → 请求命中 → 摘除 → 恢复

### 12.4 性能冒烟（数据平面必须从 MVP 开始测）

| 指标 | 说明 |
|------|------|
| RPS | 每秒请求数 |
| P50 / P95 / P99 | 延迟分位 |
| Error Rate | 失败率 |
| Memory / CPU | 资源占用 |
| 长时间运行 | 稳定性（内存不涨、连接不泄漏） |

冒烟场景：

- 单 Upstream 直连转发
- 2+ Instance 负载均衡
- WebSocket / SSE 长连接
- 大响应体
- 长时间运行观察连接数

### 12.5 验证

- [ ] docker compose 一键启动全部服务
- [ ] Linux binary 可独立部署并托管 systemd
- [ ] 端到端：两个租户域名各自访问各自容器，故障可自动切换

------------------------------------------------------------------------

## 十三、Phase 1 API 清单汇总

| 模块 | 接口数 | 所属阶段 | 优先级 |
|------|--------|----------|--------|
| Auth | 3 | S5 | P0 |
| 系统接口 | 3 | S5 | P0 |
| Tenant 管理 | 9 | S5 | P0 |
| Domain 管理 | 8 | S5 | P0 |
| Service 管理 | 8 | S5 | P0 |
| Instance 管理 | 10 | S5 | P0 |
| Route / Cache | 5 | S5 | P0 |
| Health Check 配置 | 预留 | S7 | P0 |
| **合计** | **约 46 个** | | |

> Health Check 配置 CRUD 可随 S7 一并提供，规模与本表同量级。
> 内部 Instance 注册接口（`/api/internal/*`）在 S7 联调时提供最小集：
> `POST /api/internal/instances/register`、`PATCH /api/internal/instances/:id`、`DELETE /api/internal/instances/:id`。

------------------------------------------------------------------------

## 十四、技术依赖清单

### 后端 Go 包

```text
github.com/gofiber/fiber/v3            # Management API
github.com/jackc/pgx/v5                # PostgreSQL 驱动
github.com/redis/go-redis/v9           # Redis（可选）
github.com/golang-jwt/jwt/v5           # Admin Token 签发校验
golang.org/x/crypto                    # bcrypt
github.com/google/uuid
go.uber.org/zap                        # 日志
github.com/go-playground/validator/v10 # 参数校验
gopkg.in/yaml.v3                       # 配置解析
github.com/joho/godotenv               # 开发环境变量
github.com/golang-migrate/migrate/v4   # 迁移（或用自研迁移）
```

> 数据库访问用 pgx 手写 SQL + migrations 目录（Phase 1 规模足够），SQLC 可在引入复杂查询时再评估。

### 开发 / 测试工具

```text
Docker + Docker Compose                # 环境与测试容器
go test -race                          # 并发正确性
hey / wrk / k6                         # 数据平面压测（S8）
```

### 环境变量

```env
# 数据库
MAPLE_DATABASE_URL=postgresql://maple:maple@localhost:5432/maple

# Redis（Phase 1 可关闭）
MAPLE_REDIS_URL=redis://localhost:6379

# 管理员与认证
MAPLE_ADMIN_TOKEN=your-admin-bearer-token
MAPLE_ADMIN_EMAIL=admin@maple.local
MAPLE_ADMIN_PASSWORD=admin123

# 日志
MAPLE_LOG_LEVEL=info

# 端口覆盖（开发时常用）
MAPLE_GATEWAY_HTTP_ADDR=:8000
MAPLE_MANAGEMENT_ADDR=:4000
```

------------------------------------------------------------------------

## 十五、Docker Compose 服务编排

```yaml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: maple
      POSTGRES_USER: maple
      POSTGRES_PASSWORD: maple
    volumes: [postgres-data:/var/lib/postgresql/data]
    ports: ["5432:5432"]

  redis:                      # Phase 1 辅助，可不开
    image: redis:7-alpine
    ports: ["6379:6379"]

  maple-gateway:
    build: ./server
    depends_on: [postgres]
    environment:
      MAPLE_DATABASE_URL: postgresql://maple:maple@postgres:5432/maple
      MAPLE_ADMIN_TOKEN: dev-admin-token
    ports:
      - "80:80"               # 数据平面
      - "4000:4000"           # Management API

  # 测试容器（仅开发验证用，模拟真实租户实例）
  shop-a:
    image: nginx:alpine        # 或 tiny http echo 镜像
  shop-b:
    image: nginx:alpine
  shop-canary:
    image: nginx:alpine        # Phase 2 金丝雀测试用
```

生产环境中的 Tenant Container 由 Container Manager 管理，不写死在 Maple Gateway 的 compose 中。

------------------------------------------------------------------------

## 十六、关键设计决策

### 16.1 端口与链路分离

```text
数据平面   :80/:443   → 独立 net/http listener
控制平面   :4000      → Fiber v3 Management API
```

- 代理流量不经过 Fiber Business Handler。
- 4000 / 5432 / 6379 / 9090 生产环境不暴露公网。

### 16.2 路由 Key

路由主 Key 是 **Host（hostname）**，不是路径。

```text
hostname → tenant → service → healthy instance set → proxy
```

Path 维度多 Service 分发（如一个域名下 /api 与 / 分流）归 Phase 2 高级路由。

### 16.3 Route Cache 设计

- 内存路由表按 hostname 建索引，查找 O(1) / 低开销。
- 变更通过事件驱动增量更新；启动与异常后全量重建。
- 表结构更新采用原子替换，避免读取到半更新状态。

### 16.4 Gateway 无状态（为 HA 预留）

- Session 不存本地业务状态
- Route Cache 可随时重建
- Instance 状态可重新同步
- 配置带版本号（Phase 2 深化）

### 16.5 ID 策略

- 所有表使用 UUID v4 主键。
- Tenant slug 作为可读唯一标识（如 `tenant_1001`）。

### 16.6 安全基线（Phase 1）

| 项 | 说明 |
|----|------|
| 数据平面 / 管理面分离 | 独立端口 + 独立中间件 |
| 管理面认证 | Bearer Token，所有 `/api/admin/*` 必须认证 |
| Internal API | 独立 Token，不对公网开放 |
| Host 校验 | 非法 / 空 Host 直接拒绝 |
| SSRF 防护 | Instance Upstream 地址入库时校验（仅允许内网段/受控地址） |
| Header / Body 限制 | 可配置上限 |
| 超时 | 全局 + 可覆盖 |
| 可信代理 | 仅信任 Cloudflare / 已知代理来源的 X-Forwarded-* |
| 日志脱敏 | 不记录 Token / 密码 / Cloudflare 凭据 |
| 审计 | 所有管理操作写 AuditLog |

> 尤其要避免：租户可通过配置让 Gateway 代理到任意公网/内部敏感地址——Instance 注册的 address 必须做白名单/网段校验。

### 16.7 日志分类

- Access Log（请求摘要）
- Error Log（代理/系统错误）
- Routing Log（路由命中 / miss / 拒绝原因）
- Health Log（检查结果变化）
- Security Log（鉴权失败、非法 Host、SSRF 拦截）

------------------------------------------------------------------------

## 十七、开发顺序建议

建议按以下顺序推进后端开发，每层做透再做上层：

```text
server/
├── cmd/server/main.go        → S1 先打通启动链路
├── pkg/                      → S1 工具层
│   ├── database.go / redis.go / response.go
│   ├── logger.go / validator.go / errors.go
├── internal/gateway/         → S1/S2 生命周期
├── internal/proxy/           → S2 数据平面（先做，最核心）
├── internal/router/          → S2 静态 → S6 动态
├── internal/cache/           → S4 路由缓存
├── internal/tenant/          → S3/S5
├── internal/domain/          → S3/S5
├── internal/service/         → S3/S5
├── internal/instance/        → S3/S5
├── internal/auth/            → S5
├── internal/loadbalancer/    → S7
├── internal/health/          → S7
├── internal/failover/        → S7
├── internal/security/        → S6/S7（Host 校验、SSRF 防护）
├── internal/logging/         → 伴随各阶段
├── internal/api/             → S5 路由组装
└── internal/system/          → S5 健康接口

frontend/                     → Phase 2 再建（本阶段不生成代码）
```

> 完整模块结构与后续模块（discovery / node / deployment / traffic / canary / ratelimit / metrics / api 增强）见 `项目目录结构.md`。

------------------------------------------------------------------------

## 十八、Phase 1 交付标准

### 功能交付

1. 两个以上租户可各自绑定域名，请求按 Host 正确路由到各自 Service。
2. 一个 Service 支持多个健康实例，Round Robin / Weighted 分配流量。
3. 实例故障自动摘除、恢复自动加入，全挂时优雅返回 503。
4. HTTP/1.1、WebSocket、SSE、流式响应在数据平面完整可用。
5. 通过 Management API 可完成 Tenant / Domain / Service / Instance 的增删改查，改动实时同步 Route Cache，无需重启。
6. Admin Token 认证生效，未授权请求被拒绝。

### 质量要求

1. 所有 API 有统一响应格式与错误码。
2. 请求主链路不查询 PostgreSQL，读内存路由表。
3. 数据平面与 Management API 端口/代码解耦。
4. Instance 注册地址有 SSRF 防护校验。
5. 所有管理操作写入 AuditLog。
6. Docker Compose 一键启动；Linux binary 可独立部署。
7. `go test -race ./...` 无数据竞争。

### 非交付（Phase 2）

- Canary / Blue-Green / Rolling Update 发布流量控制
- Traffic Policy（Header / Cookie / Percentage 匹配）
- 多 Node + Container Manager / Node Agent 集成
- Deployment / DeploymentVersion / Node 管理 API
- Rate Limit
- Prometheus Metrics 与监控大盘
- Admin Dashboard（React frontend）
- 完整 RBAC、Cloudflare for SaaS 集成、Gateway HA

------------------------------------------------------------------------

## 十九、Phase 2 衔接（预告）

Phase 1 收尾后，Multi-Tenant 核心闭环已可用。Phase 2 在此基础上叠加：

```text
现有链路（Phase 1）
Host → Tenant → Service → Healthy Instance → Proxy
        ↓ 扩展为
Domain → Tenant → Service → Deployment → Version → Instance → Node
        ↓ 加入
Container Manager 状态同步 + Traffic Policy + Canary / Rolling / Blue-Green
        ↓ 加入
Rate Limit + Metrics + Admin Dashboard
```

Phase 1 阶段保持 Gateway Stateless、路由可重建、实例状态可重同步，即为多 Gateway HA（Phase 3）预留空间。
