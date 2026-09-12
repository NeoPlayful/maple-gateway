# Container Manager 集成契约（Phase 7）

三进程、两条通道的接口契约与运行约定。三个进程同仓库、同 Go module，独立部署：

```
Maple Gateway（cmd/server）      数据面 + 管理面；路由权威，不碰 Docker
        │  控制下行（Gateway → CM）         ▲  状态上行（CM → Gateway）
        ▼                                  │
Container Manager（cmd/containermanager）  编排器；不碰 Docker，向各节点 Agent 下发指令
        │  容器操作（CM → Agent）           ▲  容器/资源状态（Agent → CM 拉取）
        ▼                                  │
Node Agent（cmd/nodeagent，每节点一个）    唯一直连本机 Docker Engine
        │
        ▼
Docker Engine
```

- **控制下行、状态上行，方向不混用**：部署意图自 Gateway 向下（→CM→Agent→Docker）；运行状态自 Docker 向上（→Agent→CM→Gateway）。
- **唯一 Docker 触点**：仅 Node Agent 持 Docker socket；CM 与 Gateway 均不直连 Docker。
- **实例锚点**：容器标签 `maple.instance_id` = Gateway `instances.id`（同 UUID），CM 生成并注入，保证一一对应。

---

## 一、令牌与网络

| 通道 | 令牌 | 环境变量 | 说明 |
|------|------|----------|------|
| CM → Gateway（状态上行） | Gateway 内部令牌 | `MAPLE_INTERNAL_TOKEN` | CM 侧配置 `cm.gateway_token` |
| Gateway → CM（控制下行） | CM 令牌 | `MAPLE_CM_TOKEN` | Gateway 侧配置 `cm.token` |
| CM → Agent（容器操作） | Agent 令牌 | `MAPLE_AGENT_TOKEN` | CM 节点配置 `agent_token` |

- 三进程内部端口均**不对公网开放**；Agent 仅绑内网（`allowed_cidrs` 可进一步限定来源网段）。
- 日志一律不打印令牌明文。

---

## 二、状态上行：CM → Gateway 内部 API

规范路径 `/api/internal/*`，认证 `Authorization: Bearer <MAPLE_INTERNAL_TOKEN>`。
响应统一 `{code, message, data}`。旧 `/api/internal/discovery/*` 保留一个版本过渡。

| 方法 | 路径 | 体 | 说明 |
|------|------|----|------|
| POST | `/api/internal/nodes/register` | `{name, host, region, labels}` | 注册节点；返回 `data.id`（Gateway 节点 UUID） |
| PATCH | `/api/internal/nodes/:id` | `{host?, region?, labels?, weight?, status?}` | 更新节点 |
| DELETE | `/api/internal/nodes/:id` | — | 注销节点，级联清空其上实例 `node_id` |
| POST | `/api/internal/nodes/:id/heartbeat` | — | 节点心跳；非 disabled 节点心跳即恢复 online |
| POST | `/api/internal/instances/register` | `{id?, service_id, deployment_id?, version_id?, node_id?, version?, address, port, protocol?, weight?}` | 注册实例；`id` 显式指定时与容器标签对齐 |
| PATCH | `/api/internal/instances/:id` | `{address?, port?, protocol?, weight?, status?, health?}` + 挂载字段 | 更新实例；上报即刷新 `last_seen_at` |
| DELETE | `/api/internal/instances/:id` | — | 注销实例（容器消失） |
| POST | `/api/internal/instances/:id/heartbeat` | — | 实例心跳，刷新 `last_seen_at` |
| POST | `/api/internal/instances/:id/health` | `{health: unknown\|healthy\|unhealthy\|recovering}` | 上报健康 |
| POST | `/api/internal/instances/:id/drain` | — | 置为 draining（停止接收新流量） |
| POST | `/api/internal/sync` | `{nodes:[{name, host, region?, labels?, weight?, instances:[…]}]}` | 全量快照对账（存在即刷新，不存在即注册） |

- **幂等**：register 以主键/唯一约束保证；sync 按 `节点名` 与 `service_id+address:port` 对账；heartbeat/drain 天然幂等。
- **`last_seen_at` 语义**：最后被 CM 看见；由 register（注册即写入）、sync 命中、heartbeat、PATCH 刷新，健康检查不写。
- **错误码**：404 未找到、409 冲突、422 校验失败、500 系统错误。

**典型时序（容器出现 → 可路由）**

```
CM 观测到新容器 ──register/heartbeat──▶ Gateway 写入/刷新实例
                                        Gateway 重建路由表 → 新实例可路由
```

---

## 三、控制下行：Gateway → CM 内部 API

Gateway（Leader）**推送**期望态给 CM（贴合 Gateway → CM 链路语义）。
认证 `Authorization: Bearer <MAPLE_CM_TOKEN>`。未配置 `cm.base_url` 时为 no-op，Gateway 行为与未接入 CM 完全一致。

| 方法 | 路径 | 体 | 说明 |
|------|------|----|------|
| POST | `/api/internal/deployments` | 见下 | 下发/更新部署期望态 |
| POST | `/api/internal/deployments/:id/stop` | — | 停止部署（摘除该部署容器） |
| GET | `/api/internal/deployments/:id/status` | — | 查询编排进度 `{status, message}` |
| GET | `/api/internal/stats` | — | CM 集成健康：节点可用数/纳管容器数/上次上报时间/错误 |

**期望态请求体**

```json
{
  "deployment_id": "uuid",
  "service_id": "uuid",
  "version_id": "uuid",
  "version": "v2",
  "status": "stable",
  "image": "nginx:alpine",
  "replicas": 3,
  "port": 8080,
  "env": { "KEY": "VALUE" },
  "resources": { "memory": "256m" },
  "health_path": "/healthz",
  "node_selector": { "region": "cn-east" },
  "strategy": "rolling"
}
```

- 触发时机：admin 改 deployment/version 或点"部署"，由 Gateway Leader 推送。
- 多 Gateway 实例时仅 Leader 推送，避免重复下发。
- `deployment_versions` 表已含 `replicas/port/env/resources/health_path/node_selector`（迁移 0020），即期望态来源。

---

## 四、Agent 操作 API：CM → Node Agent

认证 `Authorization: Bearer <MAPLE_AGENT_TOKEN>`；来源可用 `allowed_cidrs` 限定。
Agent 仅操作带 `maple.managed=true` 标签的容器。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/internal/node/info` | 节点资源与容量 `{id,name,cpus,memory_bytes,containers,docker_version}` |
| GET | `/api/internal/containers` | 列受管容器 |
| POST | `/api/internal/containers` | 创建并启动（幂等键 = `instance_id`） |
| POST | `/api/internal/containers/:id/start` | 启动（`id` 可为容器 ID 或 instance_id） |
| POST | `/api/internal/containers/:id/stop` | 优雅停止（超时强杀） |
| DELETE | `/api/internal/containers/:id?force=` | 删除（可强制） |
| POST | `/api/internal/images/pull` | `{image}`，受 `allowed_images` 前缀清单约束 |

**容器标签规范**（CM 注入，供映射回 Gateway）

```
maple.managed        = "true"
maple.instance_id    = <uuid>     与 Gateway instances.id 一一对应
maple.service_id     = <uuid>
maple.deployment_id  = <uuid>
maple.version_id     = <uuid>
```

**端口分配**：本期由 Agent 动态分配本机端口（`-p 0:container_port`）后回报，CM 记录并作为实例 `address:port` 上报。
`create` 返回 `{id, instance_id, host_port}`。

---

## 五、发布形态（容器侧）

CM 对账循环保持"先起后停"（surge before drain），保证最低可用数不被击穿：

| 形态 | CM 容器侧动作 | 与 Gateway 配合 |
|------|---------------|-----------------|
| Rolling | 先扩新版本容器、就绪后逐步停止旧版本 | 新容器上报 → 自动入池；旧容器先 drain 再删 |
| Blue/Green | 整组起好 standby 版本容器 | 切换仍由 Gateway blue-green API 触发 |
| Canary | canary 版本按目标副本数伸缩 | 权重仍由 Gateway traffic policy / canary 控制 |

**删除顺序**：先经 Gateway `POST /api/internal/instances/:id/drain`（先摘流量），再优雅停止 → 删除容器（后停容器）。

---

## 六、运行与部署

### 6.1 启动顺序

1. postgres / redis
2. Maple Gateway（迁移 + 管理面）
3. Node Agent（每节点；需 Docker socket）
4. Container Manager（依赖 Gateway 与各 Agent 可达）

### 6.2 独立重启语义

三进程可分别重启/升级互不崩溃：
- Gateway 重启：CM 上报会重试；实例 `last_seen_at` 短暂滞后后恢复。
- CM 重启：期望态重放（当前进程内存储，重启后由 Gateway 重新推送）；实际态下一轮观测重建。
- Agent 重启：无状态，Docker 为准。

### 6.3 降级（不部署 CM/Agent）

不配置 `cm.base_url` 与 `cm.nodes` 时：Gateway 与未接入 CM 的行为完全一致（回归基线）；CM/Agent 未运行不影响既有路由。

### 6.4 配置速查

| 进程 | 令牌/地址 | 关键环境变量 |
|------|-----------|--------------|
| Gateway | 内部令牌、CM 出口 | `MAPLE_INTERNAL_TOKEN`、`MAPLE_CM_BASE_URL`、`MAPLE_CM_TOKEN` |
| CM | 监听、上行目标、节点清单 | `MAPLE_CM_LISTEN`、`MAPLE_CM_TOKEN`、`MAPLE_CM_GATEWAY_BASE_URL`、`MAPLE_CM_GATEWAY_TOKEN`；节点见 `configs/containermanager.yaml` 的 `cm.nodes` |
| Agent | 监听、Docker、令牌 | `MAPLE_AGENT_LISTEN`、`MAPLE_AGENT_TOKEN`、`MAPLE_AGENT_DOCKER_HOST`、`MAPLE_AGENT_MANAGED_LABEL`、`MAPLE_AGENT_ALLOWED_IMAGES`、`MAPLE_AGENT_ALLOWED_CIDRS` |
