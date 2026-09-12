# Container Manager + Agent 开发计划任务

> 项目定位：Maple Gateway 的容器基础设施层  
> 核心架构：Container Manager（控制面） + Container Agent（节点执行面） + Docker / Docker Compose（运行时）  
> 开发原则：先完成稳定可用的多节点容器管理 MVP，不重新实现 Kubernetes、不自研容器运行时、不引入复杂集群共识机制。

---

# 1. 项目目标

Container Manager 用于统一管理多台 Linux Node，并通过部署在每台 Node 上的 Container Agent 完成：

- Node 注册与认证
- Agent 在线状态管理
- 主机资源监控
- Docker Engine 信息采集
- Container 管理
- Image 管理
- Network 管理
- Volume 管理
- Docker Compose Application 部署
- 容器日志查看
- Docker Event 采集
- 异步 Task 任务系统
- 基础 Deployment 管理
- 与 Maple Gateway 对接 Upstream

最终目标：

```text
Maple Gateway
      │
      │ Internal API
      ▼
Container Manager
      │
      │ HTTPS / WebSocket
      ▼
Container Agent
      │
      ▼
Docker Engine
      │
      ▼
Application / Containers
```

---

# 2. 项目边界

## 2.1 Container Manager 负责

- 用户操作入口
- Node 管理
- Agent 注册认证
- Agent 长连接管理
- Application 管理
- Deployment 管理
- Container 管理
- Image / Network / Volume 管理
- Task 管理
- 状态聚合
- 调度决策
- 任务下发
- 审计日志
- 与 Maple Gateway 对接

## 2.2 Container Agent 负责

- 主动连接 Container Manager
- 节点注册
- Heartbeat
- 主机信息采集
- Docker API 调用
- Docker Compose 操作
- Container 日志读取
- Docker Event 监听
- Task 执行
- Task 状态上报

## 2.3 Agent 不负责

禁止在 Agent 中实现：

- 用户系统
- 租户系统
- 计费
- 全局调度
- TLS
- 域名管理
- 跨节点决策
- 数据库
- Web 管理后台
- 任意 Shell 执行接口
- Kubernetes 类控制逻辑

---

# 3. 推荐技术栈

## 3.1 Container Manager

后端：

- Go 1.24+
- Fiber v3
- PostgreSQL 16
- Redis 7
- WebSocket
- JWT Bearer
- Docker 数据模型独立封装

前端：

- React 19
- TypeScript
- Vite
- TailwindCSS
- ECharts

## 3.2 Container Agent

- Go 1.24+
- 单二进制
- systemd 管理
- Docker SDK for Go
- Docker Compose CLI / Compose API
- WebSocket Client
- 本地轻量配置文件
- 无数据库

---

# 4. 推荐项目结构

```text
maple-platform/
├── apps/
│   ├── container-manager/
│   │   ├── cmd/
│   │   ├── internal/
│   │   │   ├── api/
│   │   │   ├── auth/
│   │   │   ├── node/
│   │   │   ├── agent/
│   │   │   ├── application/
│   │   │   ├── deployment/
│   │   │   ├── container/
│   │   │   ├── image/
│   │   │   ├── network/
│   │   │   ├── volume/
│   │   │   ├── task/
│   │   │   ├── scheduler/
│   │   │   └── gateway/
│   │   └── migrations/
│   │
│   └── web/
│       └── container-manager/
│
├── agents/
│   └── container-agent/
│       ├── cmd/
│       ├── internal/
│       │   ├── config/
│       │   ├── client/
│       │   ├── docker/
│       │   ├── compose/
│       │   ├── heartbeat/
│       │   ├── executor/
│       │   ├── metrics/
│       │   ├── logs/
│       │   └── events/
│       └── packaging/
│
└── shared/
    ├── protocol/
    └── models/
```

---

# 5. 开发阶段规划

---

# Phase 0：基础工程

优先级：P0

## 后端任务

- [ ] 初始化 Container Manager Go 项目
- [ ] 初始化 Fiber v3
- [ ] PostgreSQL 连接
- [ ] Redis 连接
- [ ] 配置加载
- [ ] 日志系统
- [ ] API 错误结构
- [ ] Request ID
- [ ] Graceful Shutdown
- [ ] 数据库 Migration
- [ ] JWT Bearer 基础认证

## Agent 任务

- [ ] 初始化 Agent Go 项目
- [ ] 配置文件加载
- [ ] 日志系统
- [ ] Docker Socket 检测
- [ ] Docker Engine 连接测试
- [ ] Agent Version 定义
- [ ] Graceful Shutdown
- [ ] systemd service 文件

## 验收标准

- Manager 可以正常启动
- Agent 可以正常启动
- Agent 可以检测 Docker Engine
- PostgreSQL / Redis 正常连接
- 服务退出时可以优雅关闭

---

# Phase 1：Node 与 Agent 注册

优先级：P0

## 目标

实现：

```text
Manager 创建 Enrollment Token
        ↓
Agent 使用 Token 注册
        ↓
Manager 创建 Node
        ↓
发放 Node Credential
        ↓
Agent 保存 Credential
```

## Manager 任务

- [ ] Node 数据表
- [ ] Enrollment Token 数据表
- [ ] Node Credential 数据表
- [ ] 创建 Enrollment Token API
- [ ] Agent 注册 API
- [ ] Token 一次性使用
- [ ] Token 过期时间
- [ ] Node ID 自动生成
- [ ] Node Secret 自动生成
- [ ] Agent 重复注册保护
- [ ] Agent 凭证吊销

## Agent 任务

- [ ] `--server`
- [ ] `--token`
- [ ] 首次注册逻辑
- [ ] 保存 Node ID
- [ ] 保存 Node Secret
- [ ] Credential 文件权限控制
- [ ] 注册失败重试
- [ ] 已注册状态识别

## API

```text
POST /api/v1/enrollment-tokens

POST /api/v1/agents/register

DELETE /api/v1/nodes/{node_id}/credential
```

## 验收标准

- 新节点可以通过 Enrollment Token 注册
- Enrollment Token 使用后失效
- Agent 获得独立 Node Credential
- Agent 重启后不需要重新注册
- Manager 可以吊销节点凭证

---

# Phase 2：Agent 长连接与 Heartbeat

优先级：P0

## 目标

采用：

```text
Agent
  │
  └──── 主动连接 ────► Manager
```

不要求 Node 暴露 Agent API 端口。

## Manager 任务

- [ ] WebSocket Endpoint
- [ ] Node Credential 鉴权
- [ ] Agent Session 管理
- [ ] 在线 Node Registry
- [ ] 单 Node 单 Active Session
- [ ] 断线处理
- [ ] 重连处理
- [ ] Heartbeat 超时检测
- [ ] Node Online / Unstable / Offline 状态机

## Agent 任务

- [ ] WebSocket Client
- [ ] 自动连接
- [ ] 自动重连
- [ ] 指数退避
- [ ] Heartbeat
- [ ] Ping/Pong
- [ ] 网络中断恢复

## Heartbeat 建议

```text
Heartbeat Interval: 15 秒

0 ~ 30 秒:
Online

30 ~ 90 秒:
Unstable

> 90 秒:
Offline
```

## 消息结构

```json
{
  "type": "heartbeat",
  "request_id": "req_xxx",
  "timestamp": 0,
  "payload": {}
}
```

## 验收标准

- Agent 可以主动连接 Manager
- Manager 可以显示 Agent 在线状态
- Agent 网络断开后自动重连
- Manager 可以正确判断 Offline
- 不需要开放 Node 入站 Agent 端口

---

# Phase 3：Node 主机信息与 Metrics

优先级：P0

## Agent 采集内容

- [ ] Hostname
- [ ] OS
- [ ] Kernel
- [ ] Architecture
- [ ] CPU Model
- [ ] CPU Cores
- [ ] CPU Usage
- [ ] Memory Total
- [ ] Memory Used
- [ ] Disk Total
- [ ] Disk Used
- [ ] Load Average
- [ ] Uptime
- [ ] Docker Version
- [ ] Docker API Version

## Manager 任务

- [ ] Node Info 更新
- [ ] Node Metrics 接收
- [ ] Node Metrics 短期存储
- [ ] Node Overview API
- [ ] Metrics API

## 建议

实时数据：

```text
Redis
```

长期数据：

MVP 暂不保存高频历史指标。

## 验收标准

Node 页面可以显示：

- CPU
- RAM
- Disk
- Load
- Uptime
- Docker Version
- Agent Version

---

# Phase 4：Task 任务系统

优先级：P0

这是 Container Manager 的核心基础设施。

## 状态

```text
pending
dispatching
running
success
failed
cancelled
timeout
```

## Manager 任务

- [ ] deployment_tasks 表
- [ ] Task 创建服务
- [ ] Task Queue
- [ ] Task Dispatcher
- [ ] Agent Session 查找
- [ ] Task 下发
- [ ] Task Ack
- [ ] Running 状态
- [ ] Success 状态
- [ ] Failed 状态
- [ ] Timeout
- [ ] Cancel
- [ ] Retry
- [ ] Error Message 保存

## Agent 任务

- [ ] Task Receiver
- [ ] Task Executor
- [ ] Task Ack
- [ ] Task 状态上报
- [ ] Task 超时
- [ ] Task 并发限制
- [ ] Request ID 幂等处理

## Task 消息

Manager：

```json
{
  "type": "task.execute",
  "request_id": "req_xxx",
  "payload": {
    "task_id": "task_xxx",
    "action": "container.start",
    "params": {}
  }
}
```

Agent：

```json
{
  "type": "task.result",
  "request_id": "req_xxx",
  "payload": {
    "task_id": "task_xxx",
    "status": "success"
  }
}
```

## 验收标准

用户操作 Container 时：

```text
HTTP 请求
   ↓
创建 Task
   ↓
返回 task_id
   ↓
Agent 执行
   ↓
Manager 更新状态
```

API 不需要长时间阻塞等待 Agent。

---

# Phase 5：Docker Container 管理

优先级：P0

## Agent 功能

- [ ] Container List
- [ ] Container Inspect
- [ ] Container Create
- [ ] Container Start
- [ ] Container Stop
- [ ] Container Restart
- [ ] Container Remove
- [ ] Container Stats
- [ ] Container Rename

## Task Action

```text
container.list
container.inspect
container.create
container.start
container.stop
container.restart
container.remove
container.stats
```

## Manager API

```text
GET    /api/v1/nodes/{node_id}/containers
GET    /api/v1/containers/{id}

POST   /api/v1/containers
POST   /api/v1/containers/{id}/start
POST   /api/v1/containers/{id}/stop
POST   /api/v1/containers/{id}/restart

DELETE /api/v1/containers/{id}
```

## 安全限制

Agent 不暴露：

```text
shell.exec
host.exec
command.exec
```

不允许 Manager 下发任意 Host Shell。

## 验收标准

Manager 可以完成 Container：

- 创建
- 启动
- 停止
- 重启
- 删除
- 查看详情
- 查看资源状态

---

# Phase 6：Container Logs

优先级：P0

## Agent

- [ ] 获取历史日志
- [ ] Follow Logs
- [ ] stdout
- [ ] stderr
- [ ] 时间戳
- [ ] Tail
- [ ] Since
- [ ] 日志流关闭

## Manager

- [ ] Logs API
- [ ] Logs WebSocket / SSE
- [ ] 日志连接代理
- [ ] 权限验证
- [ ] Session 生命周期

## API

```text
GET /api/v1/containers/{id}/logs

GET /api/v1/containers/{id}/logs/stream
```

## 验收标准

前端可以：

- 查看最近 100 / 500 / 1000 行日志
- 实时 Follow
- 停止 Follow
- Node 断线时正常结束日志流

---

# Phase 7：Image 管理

优先级：P1

## Agent

- [ ] Image List
- [ ] Image Pull
- [ ] Image Inspect
- [ ] Image Remove
- [ ] Pull Progress

## Task Action

```text
image.list
image.pull
image.inspect
image.remove
```

## API

```text
GET    /api/v1/nodes/{node_id}/images
POST   /api/v1/nodes/{node_id}/images/pull
DELETE /api/v1/nodes/{node_id}/images/{image_id}
```

## 验收标准

支持：

```text
nginx:latest
postgres:16
redis:7
```

正常 Pull 和删除。

---

# Phase 8：Network 管理

优先级：P1

## Agent

- [ ] Network List
- [ ] Network Inspect
- [ ] Network Create
- [ ] Network Remove
- [ ] Container Connect
- [ ] Container Disconnect

## MVP 支持

```text
bridge
```

暂不开发：

```text
overlay
vxlan
跨 Node Network
CNI
```

## API

```text
GET    /api/v1/nodes/{node_id}/networks
POST   /api/v1/nodes/{node_id}/networks
DELETE /api/v1/nodes/{node_id}/networks/{network_id}
```

---

# Phase 9：Volume 管理

优先级：P1

## Agent

- [ ] Volume List
- [ ] Volume Inspect
- [ ] Volume Create
- [ ] Volume Remove

## API

```text
GET    /api/v1/nodes/{node_id}/volumes
POST   /api/v1/nodes/{node_id}/volumes
DELETE /api/v1/nodes/{node_id}/volumes/{volume_name}
```

## MVP 限制

第一版只管理：

```text
Docker Local Volume
```

暂不支持：

- Ceph
- NFS 自动编排
- Longhorn
- Distributed Storage

---

# Phase 10：Application 模型

优先级：P0

Container Manager 不应该只围绕单个 Container 设计。

核心资源：

```text
Application
```

## 示例

```text
Shopirea-001

├── frontend
├── backend
├── nginx
├── postgres
└── redis
```

## 数据表

```text
applications
application_versions
application_specs
```

## applications

字段建议：

```text
id
tenant_id
name
description
status
created_at
updated_at
```

## application_versions

```text
id
application_id
version
spec
created_at
```

## Application Spec

MVP 使用 Docker Compose YAML。

例如：

```yaml
services:

  frontend:
    image: example/frontend:latest

  backend:
    image: example/backend:latest

  postgres:
    image: postgres:16

  redis:
    image: redis:7
```

## 验收标准

用户可以创建一个 Application，并保存 Compose Spec。

---

# Phase 11：Docker Compose 部署

优先级：P0

## Agent

- [ ] Compose Version 检测
- [ ] Compose Validate
- [ ] Compose Pull
- [ ] Compose Up
- [ ] Compose Down
- [ ] Compose Restart
- [ ] Compose Ps
- [ ] Compose Logs

## Manager

- [ ] Application Deploy API
- [ ] Deployment 创建
- [ ] 选择 Node
- [ ] Compose Spec 下发
- [ ] Task 创建
- [ ] Deployment 状态更新
- [ ] Container 同步

## Task Action

```text
application.validate
application.deploy
application.stop
application.restart
application.remove
```

## 验收标准

一个 Compose Application 可以：

```text
Deploy
Stop
Restart
Remove
```

并能够自动识别 Application 下所有 Containers。

---

# Phase 12：Deployment 模型

优先级：P0

## 数据表

```text
deployments
deployment_containers
deployment_tasks
```

## deployments

字段建议：

```text
id
application_id
node_id

version
status

started_at
completed_at

error_message

created_at
updated_at
```

## 状态

```text
pending
deploying
running
degraded
failed
stopped
removing
removed
```

## 验收标准

一个 Application 可以拥有 Deployment 历史记录。

---

# Phase 13：基础 Scheduler

优先级：P1

第一版 Scheduler 不需要复杂。

## Node 筛选条件

- Online
- Docker Ready
- CPU 可用
- Memory 可用
- Disk 可用
- Agent Version 满足要求

## 简单策略

```text
Least Applications
```

或：

```text
Lowest Memory Usage
```

## 禁止第一版开发

- Pod 调度
- Affinity
- Anti-Affinity
- Taints
- Tolerations
- StatefulSet
- DaemonSet

## 验收标准

当用户没有指定 Node 时，Manager 可以自动选一个可用 Node。

---

# Phase 14：Docker Events

优先级：P1

## Agent

监听：

```text
Docker Events API
```

事件：

- start
- stop
- die
- destroy
- create
- restart
- health_status

## Manager

- [ ] 接收 Event
- [ ] 更新 Container 状态
- [ ] 更新 Deployment 状态
- [ ] Event 去重
- [ ] Event 时间排序

## 价值

避免 Manager 频繁轮询 Docker。

---

# Phase 15：Node 管理后台

优先级：P0

## 页面

```text
Nodes
```

列表字段：

- Name
- Hostname
- Status
- IP
- CPU
- Memory
- Disk
- Containers
- Docker Version
- Agent Version
- Last Seen

## Node Detail

```text
Overview
Containers
Images
Networks
Volumes
Metrics
Events
```

---

# Phase 16：Application 管理后台

优先级：P0

## 页面

```text
Applications
```

Application Detail：

```text
Overview
Deployments
Containers
Logs
Configuration
```

## 操作

- Create
- Edit
- Deploy
- Restart
- Stop
- Remove
- Redeploy

---

# Phase 17：Task 管理后台

优先级：P1

## 页面

```text
Tasks
```

字段：

- Task ID
- Action
- Node
- Application
- Status
- Created At
- Started At
- Duration
- Error

## 功能

- 查看详情
- Retry
- Cancel
- 查看 Payload
- 查看 Result

---

# Phase 18：Maple Gateway 集成

优先级：P1

## 目标

Container Manager 部署成功后：

```text
Application
     ↓
Container
     ↓
Internal IP : Port
     ↓
Maple Gateway Upstream
```

## Manager 负责

- 获取 Application Service Endpoint
- 调用 Maple Gateway Internal API
- 创建 Upstream
- 创建 Route
- 更新 Upstream
- 删除 Upstream
- Health 状态同步

## 示例

```text
shop.example.com
        ↓
Maple Gateway
        ↓
10.20.1.15:3000
```

## 建议 API

```text
POST   /internal/upstreams
PUT    /internal/upstreams/{id}
DELETE /internal/upstreams/{id}

POST   /internal/routes
DELETE /internal/routes/{id}
```

## 验收标准

Application 部署成功后，可自动注册到 Maple Gateway。

---

# Phase 19：安全强化

优先级：P0 / P1

## 必须完成

- [ ] Agent Credential 独立
- [ ] Enrollment Token 一次性
- [ ] Enrollment Token 过期
- [ ] TLS
- [ ] Node Credential 吊销
- [ ] API 权限检查
- [ ] Task Action 白名单
- [ ] Agent 禁止任意 Shell
- [ ] Agent 配置文件权限
- [ ] Docker Socket 不暴露网络

## 后续升级

- [ ] mTLS
- [ ] Node Certificate
- [ ] Certificate Rotation
- [ ] Agent 自动更新签名验证

---

# Phase 20：审计日志

优先级：P1

记录：

```text
谁
在什么时间
对什么资源
执行了什么操作
结果是什么
```

## audit_logs

建议字段：

```text
id
user_id
action
resource_type
resource_id
node_id
ip
request_id
status
metadata
created_at
```

---

# Phase 21：Agent 安装体验

优先级：P1

目标安装方式：

```bash
curl -fsSL https://example.com/install-agent.sh | sh
```

然后：

```bash
maple-agent install \
  --server https://manager.example.com \
  --token mg_enroll_xxx
```

安装脚本负责：

- 下载 Agent
- 创建用户
- 创建配置目录
- 创建 systemd service
- 启动 Agent
- 设置开机启动

目录建议：

```text
/usr/local/bin/maple-agent

/etc/maple-agent/
    config.yaml
    credential.json

/var/lib/maple-agent/

/var/log/maple-agent/
```

---

# Phase 22：Agent 升级机制

优先级：P2

第一版允许手动升级。

后期支持：

```text
Manager
   ↓
通知 Agent
   ↓
下载新版本
   ↓
签名校验
   ↓
替换二进制
   ↓
Restart
```

必须有：

- SHA256
- 数字签名
- Rollback

---

# 6. 核心数据表清单

MVP 推荐：

```text
nodes

node_credentials

enrollment_tokens

applications

application_versions

deployments

deployment_containers

deployment_tasks

containers

container_images

container_networks

container_volumes

audit_logs
```

Metrics 第一版可以使用 Redis，不强制进入 PostgreSQL。

---

# 7. Agent Protocol

所有 Agent 消息统一结构：

```json
{
  "type": "task.execute",
  "request_id": "req_xxx",
  "timestamp": 0,
  "payload": {}
}
```

## type 分类

```text
agent.hello
agent.ready

heartbeat

node.info
node.metrics

task.execute
task.ack
task.progress
task.result

docker.event

logs.open
logs.data
logs.close
```

---

# 8. Agent Action 白名单

MVP：

```text
system.info

docker.info

container.list
container.inspect
container.create
container.start
container.stop
container.restart
container.remove
container.stats

image.list
image.inspect
image.pull
image.remove

network.list
network.inspect
network.create
network.remove

volume.list
volume.inspect
volume.create
volume.remove

application.validate
application.deploy
application.stop
application.restart
application.remove
application.ps

logs.read
```

禁止：

```text
shell.exec
host.exec
command.exec
```

---

# 9. API 规划

## Nodes

```text
GET    /api/v1/nodes
GET    /api/v1/nodes/{id}
DELETE /api/v1/nodes/{id}

POST   /api/v1/nodes/{id}/disable
POST   /api/v1/nodes/{id}/enable
```

## Enrollment

```text
POST   /api/v1/enrollment-tokens
GET    /api/v1/enrollment-tokens
DELETE /api/v1/enrollment-tokens/{id}
```

## Applications

```text
GET    /api/v1/applications
POST   /api/v1/applications
GET    /api/v1/applications/{id}
PUT    /api/v1/applications/{id}
DELETE /api/v1/applications/{id}

POST   /api/v1/applications/{id}/deploy
POST   /api/v1/applications/{id}/stop
POST   /api/v1/applications/{id}/restart
```

## Deployments

```text
GET /api/v1/deployments
GET /api/v1/deployments/{id}
```

## Containers

```text
GET    /api/v1/containers
GET    /api/v1/containers/{id}

POST   /api/v1/containers/{id}/start
POST   /api/v1/containers/{id}/stop
POST   /api/v1/containers/{id}/restart

DELETE /api/v1/containers/{id}
```

## Tasks

```text
GET  /api/v1/tasks
GET  /api/v1/tasks/{id}

POST /api/v1/tasks/{id}/retry
POST /api/v1/tasks/{id}/cancel
```

---

# 10. MVP 管理后台菜单

```text
Dashboard

Nodes

Applications

Containers

Images

Networks

Volumes

Deployments

Tasks

Audit Logs

Settings
```

---

# 11. MVP 开发优先级

## P0：必须完成

- Node 注册
- Agent Credential
- Agent WebSocket
- Heartbeat
- Node 状态
- Host Metrics
- Docker Info
- Task 系统
- Container 管理
- Logs
- Application
- Docker Compose Deployment
- Deployment 状态
- Node 页面
- Application 页面
- JWT Bearer
- 基础安全

## P1：第二阶段

- Image 管理
- Network 管理
- Volume 管理
- Docker Events
- Scheduler
- Audit Logs
- Maple Gateway 集成
- Agent 安装脚本
- Metrics 图表

## P2：后续增强

- mTLS
- Agent 自动升级
- Deployment Rollback
- Application Version
- 多 Node Application
- 更高级调度
- 高可用 Container Manager

---

# 12. 明确不进入 MVP 的功能

以下功能不应进入第一版：

```text
Kubernetes

Pod

Overlay Network

Service Mesh

CNI

CSI

etcd

Raft

Swarm

跨 Node Docker Network

自研 Container Runtime

自研 Image Runtime

自研 Overlay Storage

复杂 Scheduler

任意 Shell Remote Exec
```

核心原则：

> Container Manager 管理 Docker，而不是重新实现 Docker 或 Kubernetes。

---

# 13. MVP 完成标准

Container Manager MVP 完成时，应满足以下完整流程：

```text
管理员登录
   ↓
创建 Enrollment Token
   ↓
Linux Node 安装 Agent
   ↓
Agent 注册
   ↓
Node Online
   ↓
显示 CPU / RAM / Disk
   ↓
显示 Docker 状态
   ↓
创建 Application
   ↓
提交 Docker Compose
   ↓
选择 Node
   ↓
创建 Deployment
   ↓
生成 Task
   ↓
Agent 收到任务
   ↓
docker compose up -d
   ↓
Container 启动
   ↓
Manager 同步状态
   ↓
Application Running
   ↓
查看 Container
   ↓
查看实时 Logs
```

做到这里，就已经具备一个真正可用的多节点 Container Manager。

---

# 14. 与 Maple Gateway 的最终关系

```text
                     Maple Platform

        ┌─────────────────────────────┐
        │        Maple Gateway        │
        │                             │
        │ Domain / TLS / Routing      │
        │ Traffic / Upstream          │
        └──────────────┬──────────────┘
                       │
                       │ Internal API
                       │
        ┌──────────────▼──────────────┐
        │      Container Manager      │
        │                             │
        │ Node / App / Deployment     │
        │ Container / Task / Schedule │
        └───────┬───────────┬─────────┘
                │           │
           WebSocket    WebSocket
                │           │
         ┌──────▼───┐ ┌────▼──────┐
         │  Agent   │ │   Agent   │
         │  Node A  │ │   Node B  │
         └──────┬───┘ └────┬──────┘
                │           │
             Docker      Docker
                │           │
           Application Application
```

---

# 15. 后续演进方向

MVP 完成后，再考虑：

1. Application Version
2. Deployment Rollback
3. Blue / Green Deployment
4. Rolling Update
5. 自动 Health Check
6. 自动故障迁移
7. 多副本 Deployment
8. Node Label
9. 简单 Placement Rule
10. Maple Gateway 自动服务发现
11. Agent mTLS
12. Manager HA

仍然坚持：

> 优先保持系统扁平、简单、可维护，不演变成 Kubernetes 替代品。

---

# 16. 第一阶段实际开发顺序

建议实际编码顺序：

```text
01 基础工程

02 Node 数据模型

03 Enrollment Token

04 Agent Register

05 Node Credential

06 WebSocket

07 Heartbeat

08 Node Info

09 Metrics

10 Task System

11 Docker Info

12 Container List

13 Container Start / Stop / Restart

14 Container Create / Remove

15 Container Logs

16 Application 数据模型

17 Compose Validate

18 Compose Deploy

19 Deployment

20 Application UI

21 Node UI

22 Container UI

23 Task UI

24 Docker Events

25 Image / Network / Volume

26 Scheduler

27 Maple Gateway Integration

28 Security Hardening
```

这个顺序可以避免前期把时间浪费在高级功能上，同时确保 Agent、Task、Application 三条核心链路尽早打通。
