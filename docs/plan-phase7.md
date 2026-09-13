# Phase 7 开发计划 — Container Manager 与 Node Agent 集成专项

> **对应项目**：Maple Gateway（Multi-Tenant Gateway）
> **技术栈**：Go 1.26+ / Ent（ORM）/ PostgreSQL 16 / pgx / Fiber v3 / Redis 7 / Docker Engine / React 19 + TypeScript + Vite + Tailwind CSS
> **文档状态**：v1.0-plan（开发执行依据，随实现推进勾选与修订）
>
> **配套文档**：[Phase 1 计划](./plan-phase1.md) · [Phase 2 计划](./plan-phase2.md) · [Phase 3 计划](./plan-phase3.md) · [Phase 4 计划](./plan-phase4.md) · [Phase 5 计划](./plan-phase5.md) · [Phase 6 计划](./plan-phase6.md) · [Maple_Gateway_开发需求任务.md](./Maple_Gateway_开发需求任务.md) · [项目功能全面分析.md](./项目功能全面分析.md) · [项目目录结构.md](./项目目录结构.md) · [API接口全面分析.md](./API接口全面分析.md)

---

## 一、Phase 7 目标

> **在 Maple Gateway 同一仓库内，新增 Container Manager 与 Node Agent 两个独立模块、两个独立进程，与既有 Gateway 进程共同构成"控制意图下行、运行状态上行"的完整容器编排闭环——Gateway 管流量、CM 管编排、Node Agent 管本机容器，三者职责分明、独立部署、可单独演进**

Phase 1-6 的 Gateway 已经能"接收上报、转发流量"，但**上报的上游没有任何东西真正创建和运行容器**：文档里"Container Manager 负责 Node / Container 生命周期"的角色一直缺席，`deployments.strategy='rolling'` 只是记录，实例要么手工造、要么靠测试容器模拟。Phase 7 把这个缺口补上——不是把容器管理塞进 Gateway，而是**新建两个与 Gateway 并列的进程**，在仓库内共享代码与契约，在运行时独立部署：

```text
Phase 1-6（已交付）
Gateway 单进程：接收上报 → 路由 → 转发        （无人创建容器）
内部上报 API /api/internal/discovery/* 已存在  （无上游消费者）
deployments / deployment_versions = 纯记录     （无执行者）

Phase 7（本计划）
┌──────────────────────────────────────────────────────────────────────┐
│  同一仓库 · 同一 Go module · 三个独立进程                              │
│                                                                        │
│   Maple Gateway        Container Manager        Node Agent (每 Node)   │
│   ─────────────        ─────────────────        ───────────────────    │
│   数据面 80/443    ◀── 上报 Node/Instance ───   ── 上报容器状态 ──▶      │
│   管理面 8090          调度 / 期望态对账             直连本机 Docker     │
│   内部 API        ──▶  下发部署意图             ◀── 下发起停删指令 ──    │
│                        （CM HTTP API）               （Agent HTTP API）  │
└──────────────────────────────────────────────────────────────────────┘

控制意图下行：Admin → Gateway → CM → Node Agent → Docker
运行状态上行：Docker → Node Agent → CM → Gateway（内部上报 API）
```

### 核心原则

| 原则 | 说明 |
|------|------|
| **三进程职责单一** | Gateway 只管流量；CM 只管编排（期望态对账、调度、发布形态）；Node Agent 只管本机容器；任一进程不越界 |
| **同仓库、独立模块、独立进程** | 复用同一 Go module 与共享包，但以独立 `cmd/*` 二进制、独立配置、独立部署单元存在；可单独构建/启动/升级 |
| **控制下行、状态上行** | 部署意图自 Gateway 向下（→CM→Agent→Docker）；运行状态自 Docker 向上（→Agent→CM→Gateway）；两个方向不混用同一通道 |
| **契约单一定义、三处实现** | Gateway↔CM、CM↔Agent 两组内部 API 契约集中在文档定义；服务端与客户端按同一契约实现，可脚本比对 |
| **唯一写者** | Gateway 的路由相关表（nodes/instances）只由 Gateway 写；CM 只通过内部 API 上报，不直写路由表 |
| **不含糊的边界** | Gateway 永不直连 Docker / 直连 Node Agent；CM 永不直连任意 Node 的 Docker（只经该 Node 的 Agent）；Agent 永不自我编排（只执行本机指令） |
| **可降级** | CM / Agent 可整体不部署；关闭时 Gateway 退回 Phase 6 行为（手工/外部上报），主链路不受影响 |
| **安全纵深** | Agent 持 Docker socket = 该节点最高权限，故 Agent 仅绑内网、仅授 CM 令牌；镜像与资源受限；所有内部通道独立令牌 |

---

## 二、Phase 7 范围边界

### 2.1 本阶段要做

| 能力 | 说明 |
|------|------|
| 模块与进程骨架 | 仓库内新增 `cmd/containermanager`、`cmd/nodeagent` 两个二进制与 `internal/containermanager`、`internal/nodeagent` 两个模块；独立配置/日志/部署单元 |
| Node Agent：Docker 接入 | 直连本机 Docker Engine，封装容器创建/启停/删除、镜像拉取、资源与状态查询 |
| Node Agent：操作 API | 面向 CM 的本机容器操作与状态查询接口（独立令牌，仅内网） |
| CM：节点与容器观测 | 维护 Agent（节点）注册表；轮询/接收各节点容器状态与资源；形成"实际态" |
| CM：期望态对账 | 接收 Gateway 下发的部署意图，与实际态对账，驱动 Agent 创建/销毁容器，保持副本数 |
| CM：调度 | 将容器分配到节点（节点标签/区域/资源/负载的简单调度策略） |
| CM：发布形态执行 | 滚动更新、蓝/绿、金丝雀的**容器侧执行**（分批创建/销毁容器），并配合 Gateway 的流量权重 |
| CM：上报 Gateway | 通过 Gateway 内部 API 上报 Node / Instance 状态、心跳、健康、drain |
| Gateway：集成面定稿补齐 | 内部 API 路径定稿 `/api/internal/*`、补齐缺失端点、`last_seen` 语义修正 |
| Gateway：下发通道 | 面向 CM 的部署意图下发 API + Gateway 侧轻量 CM 客户端 |
| 前端 | 部署触发、节点/容器可视化、发布进度、集成健康 |
| 契约与测试 | 两组内部 API 契约文档（含 OpenAPI/清单）；单元 + 集成 + 端到端测试 |

### 2.2 本阶段不做（明确排除 / 延后）

| 能力 | 归属 | 说明 |
|------|------|------|
| Cloudflare 接入 / dns-01 / 泛域名证书 | Phase 8 / 远期 | 延续 Phase 6 排除 |
| Kubernetes / Swarm 等外部编排后端 | 远期 | 本期只对接单机 Docker Engine |
| 跨主机的容器网络（Overlay/SDN） | 远期 | 本期用节点可达的地址+端口暴露（详见 8.4 端口分配） |
| 多区域路由选路（消费 `nodes.region`） | 远期 | 本期调度可用 region 做过滤，但 Gateway 路由不按 region 选 |
| 被动故障摘除 / 在途排空 / 滚动流量编排（Gateway 侧） | 后续专项 | 本期 CM 执行容器侧发布，Gateway 侧流量编排细则仍可另立；本期只保证"容器就绪 → 加入路由 / 容器摘除 → 移出路由" |
| Windows 容器 / Podman 后端 | 远期 | 本期只做 Linux + Docker |
| CM/Agent 的高可用集群 | 后续 | 本期 CM 单实例（可多副本但单 active），Agent 每节点单实例 |

> **边界澄清**：本阶段首次让"容器怎么创建和运行"真正实现——但实现者是**新增的 CM 与 Node Agent 两个进程**，不是 Gateway。Gateway 只是新增了一个"把部署意图交给 CM"的下行出口和既有的"接收上报"入口。

---

## 三、起点（既有已交付 + 现状核实）

写本计划前对仓库实际结构核实，Phase 7 在此之上增量开发：

| 项 | 现状 |
|----|------|
| 仓库结构 | ✅ `server/`（Go 后端）+ `frontend/`（React 后台）+ `docs/`；顶层 `docker-compose.yml` |
| Go module | ✅ 单一 module `github.com/NeoPlayful/maple-gateway/server`（`server/go.mod`），go 1.26.0 |
| 现有进程 | ✅ `cmd/server`（Gateway 主进程）、`cmd/seed`（种子数据）——**无 CM / Node Agent 进程** |
| 构建/部署 | ✅ `server/Dockerfile`（单二进制多阶段构建）、`docker-compose.yml`（postgres + redis + maple-gateway）、`server/deploy/{linux,systemd}` |
| Gateway 内部上报 API | ⚠️ 已存在但**不完整**：`/api/internal/discovery/{nodes/register, nodes/:id/heartbeat, instances/register, instances/:id(PATCH/DELETE), instances/:id/health, sync}`；缺契约要求的 Node PATCH/DELETE、Instance heartbeat/drain；路径与文档第二十六节 `/api/internal/*` 不一致 |
| 内部认证 | ✅ `discovery.Middleware()`：`Authorization: Bearer <MAPLE_INTERNAL_TOKEN>`（`os.Getenv`，无配置结构与启动校验） |
| 部署/版本模型 | ✅ `deployments`（strategy: rolling/recreate/blue_green，**记录型**）、`deployment_versions`（version/image/weight/status）；**无副本数、无期望态、无执行者** |
| 实例/节点模型 | ✅ `instances`（address/port/protocol/weight/status/health/last_seen_at）、`nodes`（host/region/labels/weight/status/last_seen_at） |
| 上报语义 | ⚠️ `instances.last_seen_at` 仅由健康检查写（语义错位）；sync 只增不减；无实例回收；节点离线无自愈源 |
| 数据面 | ✅ 请求只读内存路由表；节点 offline / 实例不健康即摘除 |
| HA | ✅ 多 Gateway 实例 + Leader 竞逐（Redis 锁优先，DB lease 兜底） |
| SSRF 兜底 | ✅ `security.ValidateUpstreamAddress`（拒公网上游地址） |
| 迁移 | ✅ `migrations/0001-0018`；`cmd/server -migrate` 执行（启动不自动迁移） |
| 依赖 | ✅ 无 Docker SDK（本阶段新增） |

> **尚不存在**：CM 与 Node Agent 两个进程及其全部能力；Gateway 侧的部署意图下发通道与 CM 客户端；内部 API 的缺端点与路径定稿；`last_seen` 语义修正；容器/编排数据模型；前端部署与容器可视化。这些是本计划全部增量。

---

## 四、开发阶段划分

按"契约先行 → Agent 落地 → CM 观测 → CM 编排 → 收口"的依赖顺序编排为六个可验证阶段：

```text
S1  模块与进程骨架：仓库布局 + cmd/containermanager + cmd/nodeagent + 共享包 + 配置/日志/构建
S2  Gateway 集成面定稿与补齐：内部 API 路径/端点 + last_seen 语义 + 部署意图下发契约
S3  Node Agent：Docker 接入 + 容器生命周期操作 API（面向 CM）
S4  Container Manager：节点/容器观测 + 上报 Gateway（连 S2）
S5  Container Manager：期望态对账 + 调度 + 滚动/蓝绿/金丝雀容器侧执行
S6  前端 + 契约交付 + 端到端验证与发布
```

| 阶段 | 内容 | 涉及模块 | 预期工时 |
|------|------|----------|---------|
| **S1 模块与进程骨架** | 目录/包布局、两个 cmd、共享 pkg、三进程配置与日志、构建/编排文件 | cmd + internal + pkg + deploy | 2-3 天 |
| **S2 Gateway 集成面** | 路径定稿、缺端点、`last_seen` 修正、下发契约与 CM 客户端接口 | discovery + api + instance/node + config | 3-4 天 |
| **S3 Node Agent** | Docker SDK 接入、容器 CRUD/镜像/状态/资源、本机操作 API + 令牌 | nodeagent + docker | 4-5 天 |
| **S4 CM 观测与上报** | Agent 注册表、容器/节点状态采集、Gateway 上报循环、集成健康 | containermanager + discovery | 3-4 天 |
| **S5 CM 编排** | 期望态模型与对账、调度、滚动/蓝绿/金丝雀容器执行、drain 配合 | containermanager + deployment | 4-6 天 |
| **S6 前端与验证** | 部署/节点/容器可视化、契约文档、集成与端到端测试、发布 | frontend + docs + 全项目 | 4-5 天 |

> **合计约 20-27 天（单人开发）**。S1 是骨架；S2 定义契约并提供 Gateway 侧实现（CM 依赖它）；S3 产出 Agent 能力（CM 依赖它）；S4 打通"观测 + 上报"闭环（容器可见于 Gateway）；S5 实现真正的编排（容器被创建/销毁）；S6 收口。S3 与 S2 可并行。

---

## 五、S1 — 模块与进程骨架

### 5.1 仓库布局（同仓库、同 module、独立模块与进程）

```
Maple-Gateway/
├── server/                      # 同一 Go module（go.mod 唯一）
│   ├── cmd/
│   │   ├── server/              # 既有：Gateway 进程
│   │   ├── seed/                # 既有：种子数据
│   │   ├── containermanager/    # 新增：CM 进程入口
│   │   └── nodeagent/           # 新增：Node Agent 进程入口
│   ├── internal/
│   │   ├── (gateway 既有包：proxy/cache/router/health/…)
│   │   ├── containermanager/    # 新增：CM 模块
│   │   │   ├── config/          # CM 配置
│   │   │   ├── agentregistry/   # 节点(Agent)注册表
│   │   │   ├── reconciler/      # 期望态 ↔ 实际态 对账
│   │   │   ├── scheduler/       # 容器→节点 调度
│   │   │   ├── rollout/         # 滚动/蓝绿/金丝雀 容器侧执行
│   │   │   ├── gwclient/        # 上报 Gateway / 接收 Gateway 意图
│   │   │   └── api/             # CM 对外 HTTP（接收部署意图、状态查询）
│   │   └── nodeagent/           # 新增：Node Agent 模块
│   │       ├── config/          # Agent 配置
│   │       ├── docker/          # Docker Engine 客户端封装
│   │       ├── runtime/         # 容器生命周期实现
│   │       └── api/             # Agent 对外 HTTP（面向 CM）
│   ├── pkg/                     # 既有：共享工具（errors/response/logger/version）
│   ├── migrations/              # 既有；CM 表沿用同一目录（同一 DB）
│   ├── configs/                 # 既有；新增 containermanager.yaml / nodeagent.yaml
│   └── Dockerfile               # 扩展：可构建三个二进制（或拆分为三个 Dockerfile）
├── frontend/                    # 既有 React 后台
├── docker-compose.yml           # 扩展：containermanager / nodeagent 服务
└── docs/
```

- **共享**：`pkg/`（错误、响应、日志、版本）与 `internal/config` 的通用部分（env 前缀、yaml 加载）在三进程间复用；各进程有**自己的配置结构**与 env 前缀（`MAPLE_` / `MAPLE_CM_` / `MAPLE_AGENT_`）。
- **构建**：`go build ./cmd/containermanager`、`go build ./cmd/nodeagent` 各自产二进制；Dockerfile 用同一多阶段构建分别输出，或拆 `Dockerfile.cm` / `Dockerfile.agent`。
- **不拆分 module**：保持单一 `go.mod`（依用户决策），模块边界靠**目录 + 进程**而非独立 module 表达；未来若需拆仓库再抽。

### 5.2 进程配置与启动

- `configs/containermanager.yaml` + `MAPLE_CM_*`；`configs/nodeagent.yaml` + `MAPLE_AGENT_*`。
- 三进程共用日志规范（zap），但各自 `service` 字段标识（`gateway` / `cm` / `nodeagent`）。
- 每个进程 `main` 最小骨架：加载配置 → 初始化日志 → 启动 HTTP server → 注册优雅关闭。

### 5.3 验证

- [x] `go build ./...` 通过；两个新 cmd 可编译出独立二进制
- [x] 三进程可分别启动，各自日志带 service 标识
- [x] CM/Agent 未部署时 Gateway 行为与 Phase 6 完全一致（回归）
- [x] `docker-compose.yml` 可一键起三进程（+postgres/redis）

---

## 六、S2 — Gateway 集成面定稿与补齐

Gateway 侧作为"状态上行"的接收端与"意图下行"的发起端，需先把契约定稿并补齐实现——CM 依赖它。

### 6.1 内部上报 API 定稿（状态上行，CM → Gateway）

现状挂 `/api/internal/discovery/*`，文档第二十六节写作 `/api/internal/*`。定稿规范路径并保留旧路径过渡：

```text
规范（定稿）：/api/internal/*
  POST   /api/internal/nodes/register
  PATCH  /api/internal/nodes/:id             # 新增
  DELETE /api/internal/nodes/:id             # 新增（含级联策略）
  POST   /api/internal/nodes/:id/heartbeat
  POST   /api/internal/instances/register
  PATCH  /api/internal/instances/:id
  DELETE /api/internal/instances/:id
  POST   /api/internal/instances/:id/heartbeat   # 新增
  POST   /api/internal/instances/:id/health
  POST   /api/internal/instances/:id/drain       # 新增
  POST   /api/internal/sync
```

- 旧 `discovery` 路径保留一个版本并标注 deprecated。路径/方法/体/错误码写入契约文档（S6）。

### 6.2 `last_seen_at` 语义修正 + 上报刷新

- `instances.last_seen_at` 改为"最后被 CM 看见"：由内部上报路径（sync 命中、register、heartbeat、PATCH）刷新；`SetHealth` 不再写它。
- 迁移 `0019`：`instances.status` 允许 `stale`；补 `idx_instances_last_seen_at`。

### 6.3 部署意图下发契约（控制下行，Gateway → CM）

Gateway 新增"面向 CM 的部署意图"出口。**方向选择**：采用 Gateway（Leader）**推送**到 CM 的 API（贴合"Gateway → CM"的链路语义），而非 CM 反向拉取；多 Gateway 时由 Leader 推送，避免重复。

```text
Gateway → CM（CM 暴露，Gateway 充当客户端）
  POST   {cm}/api/deployments            # 下发/更新部署期望态
  POST   {cm}/api/deployments/:id/stop   # 停止
  GET    {cm}/api/deployments/:id/status # 查询编排进度
```

- Gateway 侧新增轻量 `gwclient`（在 `internal/containermanager` 之外，属 Gateway 的 CM 客户端）：在部署配置变更时（admin 改 deployment/version 或点"部署"）由 Leader 推送期望态。
- 契约与上报共用同一内部令牌体系（`MAPLE_INTERNAL_TOKEN`），或独立 `MAPLE_CM_TOKEN`（S1 配置）。

### 6.4 期望态数据补充

Gateway 的 `deployment_versions` 需补 CM 执行所需字段（迁移 0019/0020）：

```text
deployment_versions 追加：
  replicas      INT   default 1     # 期望副本数
  port          INT                 # 容器监听端口（供 Agent 映射）
  env           JSON?               # 注入容器的环境变量
  resources     JSON?               # CPU/内存限制
  health_path   TEXT?               # 容器健康检查路径
  node_selector JSON?               # 调度约束（region/labels）
```

> 这些字段把"部署意图"从纯记录变为可执行规格；CCM 据此创建容器。

### 6.5 验证

- [x] 规范路径全可用；旧路径过渡可用
- [x] 补齐端点（Node PATCH/DELETE、Instance heartbeat/drain）幂等可用
- [x] 上报刷新 `last_seen_at`；健康检查不再写它
- [x] Gateway Leader 能向 CM 推送部署意图（CM 可先用桩接收验证）
- [x] 契约文档与代码路由逐条一致

---

## 七、S3 — Node Agent：Docker 接入与容器操作

### 7.1 定位与运行形态

Node Agent 运行在**每一台 Node** 上，是**唯一直连本机 Docker Engine** 的组件。它无状态（除本地最小状态文件），一切以本机 Docker 为准。

```
Node Agent（每节点一个）
   ├── 直连 /var/run/docker.sock（或 DOCKER_HOST）
   ├── 对外：HTTP API（仅内网绑定），供 CM 调容器操作
   └── 上报：容器状态 + 节点资源（供 CM 采集）
```

### 7.2 Docker 接入

- 依赖 `github.com/docker/docker/client`（Docker Go SDK）；或可退回 shell out（次选）。
- 封装能力：
  - 镜像：`Pull`（含鉴权可选）、`Inspect`
  - 容器：`Create`（镜像/端口/环境/资源/标签）、`Start`、`Stop`（含优雅超时）、`Remove`、`Inspect`、`List`
  - 状态：容器状态/退出码/健康；节点信息（CPU/内存/容器数）
- **容器标签规范**（让 CM/Gateway 可映射）：`maple.service_id` / `maple.deployment_id` / `maple.version_id` / `maple.instance_id` / `maple.managed=true`。Agent 只操作带 `maple.managed=true` 的容器，避免误动他人容器。

### 7.3 面向 CM 的操作 API

```text
{agent}/api/internal/node/info              GET    # 节点资源与容量
{agent}/api/internal/containers             GET    # 列受管容器及状态
{agent}/api/internal/containers             POST   # 创建并启动（幂等键 = instance_id）
{agent}/api/internal/containers/:id/start   POST
{agent}/api/internal/containers/:id/stop    POST
{agent}/api/internal/containers/:id         DELETE # 删除（可配 force）
{agent}/api/internal/images/pull            POST
```

- **认证**：`Authorization: Bearer <MAPLE_AGENT_TOKEN>`（CM 持有）；Agent 仅绑内网地址（`bind: 10.x` / 显式非公网），默认拒绝非允许来源。
- **幂等**：创建以 `instance_id` 为幂等键；已存在同标签容器则不重建。

### 7.4 端口与网络（容器可达性）

Gateway 需要能连到容器。本期采用**节点内端口映射**：

- 为每个受管容器分配本机端口 → 容器端口映射（`-p {host_port}:{container_port}`）；
- Agent 上报容器暴露的 `{node_host}:{host_port}` 给 CM；
- CM 以此作为 instance 的 `address:port` 上报 Gateway。
- 端口分配策略：CM 分配（集中避免冲突）或 Agent 动态分配后回报（本期取 Agent 动态分配 + 回报，CM 记录）。

### 7.5 安全与资源约束

- Agent 持 Docker socket = 节点最高权限 → **必须**仅内网可达、仅授 CM 令牌、日志不泄露令牌。
- 容器资源限制（CPU/内存）来自部署规格；镜像可加允许清单（`allowed_images` 前缀匹配）。
- 容器不挂载宿主敏感路径（除业务声明的数据卷白名单）。

### 7.6 验证

- [x] Agent 可拉镜像、按规格创建/启动容器、查询状态、优雅停止、删除
- [x] 无令牌 / 非允许来源一律拒绝；不误动非受管容器
- [x] 端口映射分配并上报正确；容器网络可达
- [x] 创建幂等（同 instance_id 不重复建）
- [x] 资源限制与镜像允许清单生效

---

## 八、S4 — Container Manager：观测与上报

### 8.1 定位

CM 是控制面编排器：维护"实际态"（各节点上真实运行的容器），并把这些状态**上报 Gateway**，使 Gateway 的路由表反映真实实例。

### 8.2 Agent（节点）注册表

```text
CM 维护：
  nodes:  { node_id/name, host, region, labels, capacity, agents_addr }
  来源：CM 配置静态登记（本期）或 Agent 上线自注册（`POST {cm}/api/internal/nodes/register`）
  健康：周期探测 Agent /node/info 与 /health，超时标记节点不可用
```

### 8.3 实际态采集

- 周期（默认 10s）向各节点 Agent 拉取 `GET /containers` → 汇总全平台容器实际态；
- 记录 `maple.instance_id` 标签 → 容器映射；无对应期望容器的容器视为"孤儿"（告警/可选清理）。

### 8.4 上报 Gateway

- 通过 S2 定稿的内部 API 上报：
  - `POST /api/internal/nodes/register` + `nodes/:id/heartbeat`（节点存活性）
  - `POST /api/internal/instances/register`（新容器 → 实例，携带 `address:port`、`service_id/deployment_id/version_id`、`node_id`）
  - `POST /api/internal/instances/:id/heartbeat`（实例心跳，刷新 `last_seen`）
  - `PATCH/DELETE /api/internal/instances/:id`（状态变化 / 容器消失 → 注销）
  - `POST /api/internal/instances/:id/health`（若 Agent 报告容器健康）
  - `POST /api/internal/sync`（周期全量快照，覆盖范围内对账）
- **对应关系**：容器 `maple.instance_id` ↔ Gateway `instances.id`（同 UUID，CM 生成并在创建时注入标签，保证一一对应）。

### 8.5 集成健康与可观测

- CM 记录并暴露：上次上报时间、上报滞后、上报错误、纳管容器/节点数；
- 指标：`maple_cm_nodes_total{status}`、`maple_cm_containers_total{state}`、`maple_cm_report_total{result}`、`maple_cm_report_lag_seconds`；
- Gateway 侧记录"上次同步/滞后"（S2 已留位）供前端展示。

### 8.6 验证

- [x] Agent 上启动的受管容器，经 CM 上报后出现在 Gateway 实例列表并可路由
- [x] 容器停止/消失 → Gateway 实例被注销/摘除
- [x] 节点心跳与离线感知经上报通道生效
- [~] CM 侧上报滞后/错误可观测（`/api/stats`）；Gateway 侧"滞后/告警"面板列为后续项
- [x] 无 CM 时 Gateway 行为与 Phase 6 一致（回归）

---

## 九、S5 — Container Manager：期望态对账与发布执行

### 9.1 期望态模型与来源

- CM 接收 Gateway 下发的部署期望态（S2 的 `POST {cm}/api/deployments`），落 CM 自有表（`cm_deployments` / `cm_replicas`，与 Gateway 表分离，CM 自管）；
- 期望态内容：`service/deployment/version` + `image` + `replicas` + `port` + `env` + `resources` + `node_selector` + `strategy`。

### 9.2 对账循环（reconcile）

```text
周期（默认 5s）遍历期望态：
   desired_replicas vs actual_containers（同 deployment+version）
   ├── 缺 → 调度节点 → Agent 创建容器 → 上报 Gateway（S4）
   ├── 多 → 选多余容器 → 优雅停止 → Agent 删除 → 上报注销
   └── 一致 → 检查健康，不健康则重建（有界重试 + 退避）
```

- 对账**幂等**；以 `instance_id` 为幂等键，避免重复创建。
- 失败退避有界；连续失败告警，不无限重试。

### 9.3 调度

- 简单调度：按 `node_selector`（region/labels）过滤候选节点 → 排除不可用/满载节点 → 在候选内按"最少容器数 / 剩余资源"选择；
- 容量约束：节点可运行容器上限、CPU/内存余量（来自 Agent `/node/info`）；
- 单节点可放多副本（默认打散到不同节点，除非副本数 > 节点数）。

### 9.4 发布形态执行（容器侧）

| 形态 | CM 容器侧动作 | 与 Gateway 配合 |
|------|---------------|-----------------|
| Rolling | 按序创建新版本容器、就绪后逐步停止旧版本容器（保持最低可用数） | 新容器上报 → Gateway 自动入池；旧容器 drain 后删除 → 移出路由 |
| Blue/Green | 整组创建 standby 版本容器，就绪后通知切换 | 切换动作仍由 Gateway 的 blue-green API 触发；CM 负责备好容器 |
| Canary | 创建少量新版本容器并按目标副本数伸缩 | 流量权重仍由 Gateway traffic policy / canary 控制 |

- **drain 配合**：CM 删除容器前，先经 Gateway `POST /api/internal/instances/:id/drain` 通知进入 draining（停止接收新请求），再优雅停止容器。容器侧排空等待窗口属后续专项，本期至少保证"先摘流量、后停容器"的顺序。
- 发布进度：CM 记录各步状态（创建中/就绪/交接中/完成/失败），供 S6 查询与前端展示。

### 9.5 失败与回滚

- 新版本容器起不来（镜像拉取失败 / 启动即退出 / 健康不过）→ 停止推进、保留旧版本容器继续服务、告警；
- 记录 `last_error`；人工可经 API 重新触发或回退（回退 = 期望态指回旧版本，CM 对账自然收敛）。

### 9.6 验证

- [x] 期望副本数变更 → CM 自动增减容器；上报后 Gateway 实例数同步
- [x] 容器异常退出 → 重建（有界退避）；连续失败告警
- [x] Rolling：新旧版本容器平稳交接，最低可用数不被击穿（surge 先于 drain）
- [x] Blue/Green：CM 先备好备用版本容器（整组扩容）；切换动作仍由 Gateway blue-green API 触发
- [x] Canary：新版本容器按目标数伸缩，Gateway 权重可调
- [x] 删除容器前先 drain（先摘流量后停容器）
- [x] CM 关闭 / 期望态清空时，不影响 Gateway 既有路由（回归）

---

## 十、S6 — 前端、契约交付与端到端验证

### 10.1 前端扩展

- **部署页**：触发部署（下发期望态给 CM）、展示发布进度（各步状态）、停止/回退；
- **节点页**：展示 Agent 状态（在线/资源/容器数）、节点上容器列表；
- **实例页**：展示容器来源（CM 纳管）、容器状态、`last_seen` 新鲜度；
- **集成健康**：Gateway 侧"CM 上报滞后/错误"、CM 侧"Agent 状态"汇总视图。
- 写操作按 RBAC 显示/禁用。

### 10.2 契约交付

- Gateway↔CM 内部 API 契约（上报 + 下发）：路径/方法/体/状态码/幂等/错误/时序图；
- CM↔Agent 操作 API 契约：同上；容器标签规范、端口分配约定；
- 面向运维的部署说明：三进程拓扑、启动顺序、令牌与网络要求。

### 10.3 端到端测试计划

- [x] **全链路**：触发部署 → Gateway 下发 → CM 调度 → Agent 创建容器 → CM 上报 → Gateway 接收（本地实机验证：2 副本容器 + 上报含动态端口）
- [x] 副本伸缩：改 replicas → 容器增减 → 路由同步（2→1 缩容验证通过）
- [x] Rolling 发布：新旧版本交接无中断（surge 先于 drain，单元测试覆盖）
- [x] 容器故障：容器退出 → running 数低于目标 → CM 重建（有界退避）
- [x] 节点故障：停 Agent → 观测失败 → 该节点不参与注销判定（避免误删）
- [x] 边界：无令牌 401 / 非允许来源 403 / Agent 不误动非受管容器
- [x] 降级：不部署 CM/Agent 时，Gateway 与 Phase 6 一致（回归基线）
- [x] 三进程可分别独立重启/升级，互不崩溃

### 10.4 质量要求

1. 三进程职责边界不越界；Gateway 数据面热路径零 DB 查询。
2. 内部通道（GW↔CM、CM↔Agent）独立令牌；日志零泄漏令牌/私钥。
3. `go vet ./...` 通过；`go test ./...` 全绿；`-race` 建议 Linux/CI 复核；Docker 相关测试用真实或伪 Engine（`httptest` 模拟 Docker API / mock client）。
4. 幂等：上报、对账、容器创建均幂等。
5. 文档联动更新：本计划勾选；`项目目录结构.md`（新增模块与进程）/ `API接口全面分析.md`（定稿内部 API）/ `项目功能全面分析.md`（CM/Agent 从"缺席"转"实现"）。

### 10.5 遗留复核项（建议 CI / Linux 补）

- `go test -race ./...`（CM 对账 / 上报并发、Agent 容器操作并发）。
- 大规模（100 节点 / 1000 容器）对账与上报吞吐。
- Docker Engine 版本兼容矩阵、镜像拉取速率与重试。

---

## 十一、关键设计决策

### 11.1 同仓库、同 module、独立进程

依用户决策：不拆独立 module/仓库，用 `cmd/*` + `internal/*` 表达模块边界，进程独立部署。好处是共享 `pkg`/契约/测试工具、构建链统一；代价是依赖版本共享。未来若需独立仓库，模块边界已就绪，抽出成本低。

### 11.2 控制下行用"Gateway 推 CM"，不是"CM 拉 Gateway"

贴合规用户给出的 Gateway → CM → Agent → Docker 链路语义：部署意图由 Gateway（Leader）主动推给 CM。**取舍**：Gateway 需持一个 CM 客户端与 CM 地址配置；多实例时仅 Leader 推送。备选（CM 轮询 Gateway）会在 Gateway 侧引入"期望态版本"查询语义且方向与用户链路相反，故不采用。

### 11.3 Agent 是唯一 Docker 接触点

Docker socket 等于节点最高权限。把这份权限**只**交给每节点上的 Agent，Gateway 与 CM 都不持 socket：CM 经 HTTP 调用 Agent，Agent 执行。最小化爆炸半径，且天然支持多节点。

### 11.4 容器标签是三方对齐的唯一锚点

容器 `maple.instance_id` 标签 = Gateway `instances.id`。CM 创建时注入、上报时携带，Gateway 落库，形成三方一一对应；Agent 仅操作 `maple.managed=true` 容器，杜绝误伤。

### 11.5 唯一写者：Gateway 拥有路由表

`nodes`/`instances` 表仍**只由 Gateway 写**（经内部上报 API）。CM 不直写路由表，只上报。这让"谁决定流量去向"始终是 Gateway，CM 的失联不会绕过 Gateway 制造路由状态。

### 11.6 端口映射落地本期可行解，Overlay 网络延后

跨主机容器网络（Overlay/SDN）复杂度高；本期用"节点内端口映射 + 上报可达 address:port"把容器暴露给 Gateway，足以支撑多节点转发；Overlay 明确延后。

### 11.7 可整体降级

CM/Agent 为可选部署。未部署时 Gateway 回到 Phase 6 行为（外部/手工上报运维），三进程解耦保证 Gateway 不被 CM 的缺失拖垮。

---

## 十二、技术依赖与配置清单

### 新增依赖

```text
github.com/docker/docker/client        # Node Agent 直连 Docker Engine（Docker Go SDK）
（已有）github.com/gofiber/fiber/v3     # CM/Agent 的 HTTP API
（已有）go.uber.org/zap / ent / pgx     # 日志 / CM 持久化 / DB
```

### 配置新增（三进程各自）

```yaml
# containermanager.yaml（+ MAPLE_CM_*）
cm:
  listen: ":9091"
  token: ""                 # CM 接收 Gateway 意图 / Agent 上报的令牌
  gateway_base_url: ""      # 上报 Gateway 的地址（状态上行）
  gateway_token: ""         # = Gateway 的 MAPLE_INTERNAL_TOKEN
  reconcile_interval: 5s
  observe_interval: 10s
  default_replicas: 1

# nodeagent.yaml（+ MAPLE_AGENT_*）
agent:
  listen: "10.0.0.1:9092"   # 仅内网
  token: ""                 # CM 访问 Agent 的令牌
  docker_host: ""           # 空则 /var/run/docker.sock
  allowed_cidrs: []         # 允许 CM 来源网段
  allowed_images: []        # 镜像允许清单（前缀匹配，空=不限）
  managed_label: "maple.managed"
```

```env
MAPLE_CM_TOKEN=...
MAPLE_CM_GATEWAY_BASE_URL=http://gateway:8090
MAPLE_AGENT_TOKEN=...
MAPLE_AGENT_LISTEN=10.0.0.1:9092
```

### 迁移新增（同一 DB，同一目录）

```text
0019_gateway_internal_api.sql   # instances.status 扩展 stale + idx_instances_last_seen_at
0020_deployment_spec.sql        # deployment_versions 补 replicas/port/env/resources/health_path/node_selector
0021_cm_tables.sql              # CM 自有表：cm_deployments / cm_replicas（期望态）
```

### 目录/构建

```text
server/cmd/containermanager/     # CM 入口
server/cmd/nodeagent/            # Node Agent 入口
server/internal/containermanager/# CM 模块
server/internal/nodeagent/       # Agent 模块
server/Dockerfile(.cm/.agent)    # 三进程镜像
docker-compose.yml               # 扩展三进程编排
```

> 迁移执行：`cd server && go run ./cmd/server -migrate`（服务启动不自动迁移；CM 表随同一迁移器落地）。

---

## 十三、验收 / 交付标准

### 功能交付

1. [x] 仓库内新增 CM 与 Node Agent 两模块两进程，独立配置/部署，可单独构建启动。——S1
2. [x] Gateway 内部 API 定稿补齐；`last_seen` 语义修正；部署意图下发契约与客户端就绪。——S2
3. [x] Node Agent 直连本机 Docker，完成镜像/容器全生命周期与状态查询，API 安全加固。——S3
4. [x] CM 观测节点/容器实际态并上报 Gateway，集成健康可观测。——S4
5. [x] CM 期望态对账 + 调度 + 滚动/蓝绿/金丝雀容器侧执行 + drain 配合。——S5
6. [x] 全链路端到端打通：触发部署 → 容器就绪 → 上报 → 路由 → 请求达容器。——S6
7. [x] 契约文档（GW↔CM、CM↔Agent）+ 集成/端到端测试套件交付。——S6
8. [x] 前端支持部署触发、节点/容器/发布进度可视化、集成健康。——S6
9. [x] CM/Agent 未部署时 Gateway 与 Phase 6 完全一致（降级基线）。——全期

> 端到端验证（本地实机）：真实 Agent + CM + Gateway 桩，推送部署意图 → CM 调度创建 2 副本容器
> → 观测上报 Gateway（含动态端口）→ 缩容 2→1 时先 drain 再停容器。均通过。
> `-race` 与大规模/多 OS 复核列为 CI/Linux 遗留项（本机无 gcc）。

### 质量要求

1. 三进程职责不越界；Gateway 数据面热路径零 DB 查询。
2. 内部通道独立令牌；日志零泄漏令牌/私钥；Agent 仅内网、仅授 CM。
3. `go vet ./...` 通过；`go test ./...` 全绿；Docker 相关测试可脱 Engine 运行。
4. 上报、对账、容器创建幂等。
5. 契约文档、代码、测试三处一致，可脚本比对。
6. 文档联动更新：本计划勾选 + 配套分析文档同步。

### 非交付（后续 / 远期）

- 跨主机容器网络（Overlay/SDN）、Podman/Windows 容器。
- Gateway 侧被动故障摘除 / 在途排空窗口 / 滚动流量权重编排细则（CM 已执行容器侧，Gateway 侧策略可另立）。
- CM/Agent 高可用集群、多 region 调度与就近路由。
- Cloudflare 接入 / dns-01 / 泛域名证书 / 网关插件机制。

---

## 十四、Phase 8 衔接（预告）

Phase 7 交付 CM 与 Node Agent 后，平台具备真正的"从部署意图到容器运行"的闭环。Phase 8 在此之上叠加：

```text
Gateway 侧流量编排细则（被动故障摘除 / 在途排空窗口 / 滚动权重自动交接）
        ↓
多节点调度增强（region 感知就近调度 + Gateway 就近取容器）
        ↓
CM/Agent 高可用（多 CM active-passive、Agent 自注册与自愈）
        ↓
（远期）跨主机容器网络、Cloudflare for SaaS + Origin TLS、网关插件机制
```

Phase 7 定稿的内部 API 契约、容器标签锚点与期望态模型，即为 Phase 8 调度增强与流量编排细则的挂载点。
