# Maple Gateway 项目级 IP 池开发计划任务

## 1. 文档目标

为 Maple Gateway / Container Manager 设计并实现一套可长期扩展的项目级 IPAM（IP Address Management）系统。

该系统负责：

- 为每个 Project 自动分配独立的 Docker bridge IPv4 网段。
- 默认每个 Project 分配一个 `/24` 网段。
- 每个 Node 使用 Node-local IPAM。
- 不同 Node 之间允许重复使用相同 CIDR。
- 同一 Node 内所有项目网段必须唯一。
- 每个 Node 支持多个 Network Pool。
- Network Pool 支持优先级、扩容、停用、排空、冲突检测和耗尽状态。
- 当前 Pool 用完后自动切换到下一个可用 Pool。
- 已有 Pool 不需要因为扩容而修改或迁移。
- Container Manager 数据库作为 IPAM 状态权威。
- Node Agent / Docker Engine 作为网络执行层。
- 普通容器 IP 继续由 Docker 自动分配。
- 不实现跨 Node 的容器三层互通。

---

# 2. 最终网络架构

整体架构：

```text
                    Maple Gateway
                 / Container Manager
                         │
                         │
                  Central Control Plane
                         │
            ┌────────────┼────────────┐
            │            │            │
          Agent        Agent        Agent
         Node A       Node B       Node C
            │            │            │
       Docker Engine Docker Engine Docker Engine
            │            │            │
       Node-local    Node-local    Node-local
          IPAM          IPAM          IPAM
            │            │            │
        多个 Pool     多个 Pool     多个 Pool
            │            │            │
       /24 Projects  /24 Projects  /24 Projects
```

核心原则：

```text
IPAM Scope:
Node-local

Cross-node Container Routing:
Disabled

CIDR Global Uniqueness:
Not Required

CIDR Node-local Uniqueness:
Required
```

---

# 3. 默认地址池

系统默认创建：

```text
Pool Name:
default

Address Pool:
10.128.0.0/9

Project Prefix:
/24
```

地址范围：

```text
10.128.0.0
~
10.255.255.255
```

理论项目容量：

```text
2^(24 - 9)
= 32768
```

即：

```text
32768 个 /24 Project Network / Node
```

---

# 4. 项目网段分配示例

默认 Pool：

```text
10.128.0.0/9
```

项目分配：

```text
Project 00001 → 10.128.0.0/24
Project 00002 → 10.128.1.0/24
Project 00003 → 10.128.2.0/24

...

Project 00256 → 10.128.255.0/24
Project 00257 → 10.129.0.0/24

...

Project 32768 → 10.255.255.0/24
```

---

# 5. 多 Node 允许重复 CIDR

Node A：

```text
Project A1 → 10.128.1.0/24
```

Node B：

```text
Project B1 → 10.128.1.0/24
```

这是合法的。

因为 Node A 和 Node B：

```text
没有 Container L3 Routing
```

Docker bridge 只在各自宿主机本地使用。

因此唯一约束必须是：

```text
UNIQUE(node_id, subnet)
```

不能：

```text
UNIQUE(subnet)
```

---

# 6. 一个 Node 支持多个 Network Pool

最终设计必须支持：

```text
Node A
├── Pool 1
│   └── 10.128.0.0/9
│
├── Pool 2
│   └── 10.64.0.0/10
│
└── Pool 3
    └── 172.20.0.0/14
```

每个 Pool 都可以：

```text
Project Prefix = /24
```

这样未来扩容不需要修改原 Pool。

---

# 7. 扩容原则

禁止把扩容理解成：

```text
修改已有 Pool CIDR
```

例如不推荐：

```text
10.128.0.0/9
↓
直接扩大成
10.0.0.0/8
```

正确方式：

```text
新增 Pool
```

例如：

```text
Pool 1
10.128.0.0/9
Capacity = 32768

Pool 2
10.64.0.0/10
Capacity = 16384
```

Node 总容量：

```text
32768 + 16384
= 49152 Projects
```

未来还可以继续增加：

```text
Pool 3
172.20.0.0/14
Capacity = 1024 Projects
```

---

# 8. Network Pool 状态

Network Pool 必须支持以下状态：

```text
active
draining
disabled
exhausted
conflict
```

说明：

## active

允许：

```text
新项目分配
```

---

## draining

表示：

```text
已有项目继续使用
新项目禁止分配
```

适用于：

- 地址池准备退役
- 准备迁移旧网段
- 不希望新项目继续进入该 Pool

---

## disabled

完全禁止使用。

已有项目记录仍然保留。

---

## exhausted

地址池已经没有可用 `/24`。

系统自动跳过并进入下一个 Pool。

---

## conflict

检测到与：

- Host Route
- Docker Network
- VPN
- WireGuard
- Tailscale
- VPC
- LAN

发生地址重叠。

禁止新项目分配。

---

# 9. Pool 优先级

每个 Pool 增加：

```text
priority
```

数值越小优先级越高。

例如：

```text
Pool 1
priority = 10

Pool 2
priority = 20

Pool 3
priority = 30
```

分配顺序：

```text
Pool 1
↓
Pool 1 无可用 subnet
↓
Pool 2
↓
Pool 2 无可用 subnet
↓
Pool 3
```

---

# 10. 数据模型

## 10.1 cm_node_network_pools

一个 Node 可以拥有多个 Pool。

字段建议：

```text
id

node_id

name
description

address_pool
project_prefix

priority

status

next_index

reuse_enabled
reuse_delay_seconds

is_system_default

last_checked_at
last_conflict_reason

created_at
updated_at
```

不允许：

```text
UNIQUE(node_id)
```

建议约束：

```text
UNIQUE(node_id, name)
UNIQUE(node_id, address_pool)
```

注意：

即使 CIDR 不完全相同，也必须在业务层检测 Pool 之间是否 overlap。

---

# 11. cm_project_networks

用于记录项目实际使用的网络。

字段建议：

```text
id

node_id
project_id
pool_id

subnet_index

subnet
gateway

docker_network_name

status

allocated_at
released_at
reuse_after

last_error

created_at
updated_at
```

状态：

```text
reserved
creating
active
deleting
released
failed
conflict
drift
```

数据库约束：

```text
UNIQUE(node_id, subnet)
UNIQUE(project_id)
```

MVP 默认：

```text
一个 Project
=
一个 Project Network
```

未来如果允许一个项目多个业务网络，再调整成：

```text
UNIQUE(project_id, network_role)
```

---

# 12. 可选 network_role

预留字段：

```text
network_role
```

MVP：

```text
default
```

未来可能支持：

```text
frontend
backend
database
internal
public
```

当前不实现多网络逻辑。

只保留扩展能力。

---

# 13. System Default Network Pool

系统配置保存：

```text
Default Address Pool:
10.128.0.0/9

Default Project Prefix:
/24

Default Allocation Mode:
Sequential

Default Reuse:
Enabled

Default Reuse Delay:
600 seconds
```

新 Node 注册时，可以自动创建：

```text
default
10.128.0.0/9
```

也可以选择：

```text
inherit system default
```

---

# 14. Node Override

Node 支持自定义多个 Network Pool。

例如：

```text
Node A
└── default
    10.128.0.0/9

Node B
├── primary
│   10.128.0.0/9
│
└── expansion
    10.64.0.0/10
```

Node C：

```text
10.128.0.0/9
```

如果和宿主网络冲突，可以不用默认池：

```text
Node C
└── custom
    10.64.0.0/10
```

---

# 15. 项目分配总流程

```text
Create Project
      ↓
Scheduler 选择 Node
      ↓
获取 Node 所有 Network Pools
      ↓
过滤 status = active
      ↓
按 priority ASC 排序
      ↓
尝试 Pool 1
      ↓
是否存在可用 /24？
      │
      ├─ Yes
      │    ↓
      │  Reserve Subnet
      │
      └─ No
           ↓
        标记 exhausted
           ↓
        尝试 Pool 2
           ↓
        ...
```

成功后：

```text
Project
↓
pool_id
↓
subnet
↓
gateway
↓
docker_network_name
```

---

# 16. 分配策略

MVP：

```text
Sequential
```

不要使用随机分配。

每个 Pool 维护：

```text
next_index
```

例如：

```text
Pool:
10.128.0.0/9

next_index:
125
```

下一次优先尝试：

```text
10.128.125.0/24
```

---

# 17. Subnet Index 算法

对于：

```text
Pool:
10.128.0.0/9

Project Prefix:
/24
```

映射：

```text
index 0
→ 10.128.0.0/24

index 1
→ 10.128.1.0/24

index 255
→ 10.128.255.0/24

index 256
→ 10.129.0.0/24

...

index 32767
→ 10.255.255.0/24
```

必须实现：

```text
pool CIDR + subnet_index
→ subnet
```

以及：

```text
subnet
→ subnet_index
```

要求：

- 不允许字符串硬拼。
- 使用标准 IPv4 数值计算。
- 严格验证边界。
- 防止整数溢出。
- subnet 必须完全落在 pool 内。

---

# 18. Pool Capacity

统一计算：

```text
capacity = 2^(project_prefix - pool_prefix)
```

例如：

```text
10.128.0.0/9
/24

capacity:
32768
```

例如：

```text
10.64.0.0/10
/24

capacity:
16384
```

例如：

```text
172.20.0.0/14
/24

capacity:
1024
```

---

# 19. Pool Available 计算

建议维护实时统计：

```text
capacity
allocated
reserved
released_waiting
available
```

其中：

```text
available
=
capacity
- allocated
- reserved
- unreusable released subnet
```

不能单纯依赖：

```text
capacity - cm_project_networks count
```

---

# 20. 数据库先占坑

错误：

```text
查空闲 subnet
↓
创建 Docker Network
↓
写数据库
```

高并发会出现重复分配。

正确：

```text
BEGIN

锁定 Pool

计算 Candidate Subnet

INSERT cm_project_networks
status = reserved

更新 next_index

COMMIT
```

然后：

```text
↓
发送 Agent Task
↓
Docker Network Create
```

---

# 21. PostgreSQL 并发锁

推荐：

```text
SELECT *
FROM cm_node_network_pools
WHERE id = ?
FOR UPDATE
```

流程：

```text
BEGIN
↓
锁 Pool
↓
读取 next_index
↓
查候选 subnet
↓
INSERT cm_project_networks
↓
UPDATE next_index
↓
COMMIT
```

同时必须保留：

```text
UNIQUE(node_id, subnet)
```

作为最终并发保护。

不能只使用进程内 mutex。

原因：

未来可能：

```text
多实例 Container Manager
```

---

# 22. Pool 自动切换

如果当前 Pool：

```text
无可用 subnet
```

则：

```text
status = exhausted
```

然后继续：

```text
下一个 active Pool
```

如果所有 Pool 都无法分配：

返回：

```text
IPAM_ALL_POOLS_EXHAUSTED
```

---

# 23. released subnet 回收

项目删除：

```text
active
↓
deleting
↓
Docker Network 删除
↓
released
```

记录：

```text
released_at
reuse_after
```

默认：

```text
reuse_delay_seconds = 600
```

即：

```text
10 分钟
```

等待后才允许重新进入分配候选。

---

# 24. 释放后的分配策略

顺序分配推荐：

第一阶段：

```text
从 next_index 向后找
```

当走到 Pool 尾部：

```text
回到 index 0
```

然后扫描：

```text
已经 released
并且 reuse_after <= now()
```

的网段。

这样避免每次都扫描完整 Pool。

---

# 25. Pool 扩容流程

管理员：

```text
Node
↓
Network Pools
↓
Add Pool
```

例如新增：

```text
Name:
expansion-01

Address Pool:
10.64.0.0/10

Project Prefix:
/24

Priority:
20

Status:
active
```

保存前：

1. 校验 CIDR。
2. 校验和同 Node 已有 Pool 是否 overlap。
3. 调用 Agent 检查 Host Route。
4. 检查 Docker Networks。
5. 检查系统网络冲突。
6. 通过后启用。

无需：

- 修改已有项目
- 修改已有 Docker Network
- 重启 Docker
- 修改 daemon.json

---

# 26. Pool Draining

管理员可以：

```text
active
↓
draining
```

效果：

```text
已有项目继续运行

新项目：
禁止进入该 Pool
```

当：

```text
active projects = 0
```

后允许：

```text
disabled
```

---

# 27. 不允许直接修改有项目的 Pool CIDR

如果 Pool 已经存在：

```text
active / reserved cm_project_networks
```

禁止直接修改：

```text
address_pool
project_prefix
```

应该返回：

```text
IPAM_POOL_IN_USE
```

正确操作：

```text
新增 Pool
↓
旧 Pool 设置 draining
↓
新项目进入新 Pool
↓
旧项目后续逐步迁移
```

---

# 28. Project Network Migration

MVP 暂不自动实现。

未来可以增加：

```text
Migrate Project Network
```

流程：

```text
停止 Project
↓
申请新 Pool Subnet
↓
删除旧 Docker Network
↓
创建新 Docker Network
↓
重新启动 Compose
↓
验证服务
↓
释放旧 Subnet
```

必须作为独立功能。

不能隐式发生。

---

# 29. Docker Network 创建

Agent 执行等价命令：

```bash
docker network create \
  --driver bridge \
  --subnet 10.128.15.0/24 \
  --gateway 10.128.15.1 \
  maple-31c6635dffc5
```

---

# 30. Docker Network Name

统一格式：

```text
maple-{short_id}
```

其中 `{short_id}` 为 Project / Application ID 去连字符后的前 12 位，与容器名、Compose 项目名共用同一规则（见 `pkg.ShortID`），避免多处各自截断产生漂移。

例如：

```text
maple-31c6635dffc5
```

要求：

- 与 Compose 项目名同形态（均为 `maple-<12>`），二者取值一致，便于对照排查
- 唯一
- 可定位 Project
- 不使用用户输入名称直接拼接
- 避免特殊字符
- 控制长度
- 不依赖 Tenant 显示名称

---

# 31. Compose 集成

推荐：

```text
Container Manager
先创建 Docker Network
↓
Compose 引用 external network
```

Compose：

```yaml
services:
  frontend:
    image: ...
    networks:
      - project_network

  backend:
    image: ...
    networks:
      - project_network

  postgres:
    image: ...
    networks:
      - project_network

  redis:
    image: ...
    networks:
      - project_network

networks:
  project_network:
    external: true
    name: maple-31c6635dffc5
```

禁止继续依赖：

```text
<compose-project>_default
```

自动创建网络。

---

# 32. Gateway 地址

默认：

```text
subnet + first usable
```

例如：

```text
Network:
10.128.15.0/24

Gateway:
10.128.15.1
```

MVP 不允许用户自定义 Gateway。

---

# 33. Container IP

普通容器：

```text
Docker automatic IPAM
```

例如：

```text
frontend
backend
postgres
redis
worker
```

不由 Maple Gateway 分配具体 IP。

内部通信使用：

```text
Docker DNS
Service Name
```

例如：

```text
backend:4000
postgres:5432
redis:6379
```

不要依赖：

```text
10.128.x.x
```

固定容器地址。

---

# 34. 静态容器 IP

MVP：

```text
不实现
```

未来高级设置：

```text
Static Container IPv4
```

只适用于：

- 特殊网络服务
- 防火墙白名单
- 老系统依赖
- 网络设备类容器

---

# 35. Node 注册时网络检查

Node Agent 第一次注册时扫描：

```text
Host Routes
Docker Networks
Network Interfaces
```

额外关注：

- WireGuard
- Tailscale
- OpenVPN
- ZeroTier
- Cloud VPC
- LAN
- Podman
- Existing Docker Networks

---

# 36. Host Route 冲突

如果：

```text
10.128.0.0/9
```

和 Node 已有路由重叠：

Pool：

```text
status = conflict
```

记录：

```text
last_conflict_reason
```

禁止新项目使用。

---

# 37. Pool 之间冲突

同一个 Node：

```text
Pool A:
10.128.0.0/9

Pool B:
10.128.0.0/10
```

禁止。

因为存在 overlap。

必须检测：

```text
Pool A overlaps Pool B
```

不同 Node：

允许完全相同。

---

# 38. Docker Existing Network 冲突

新增 Pool 时必须检查 Node 当前 Docker Networks。

例如已有：

```text
10.64.20.0/24
```

而新增：

```text
10.64.0.0/10
```

则 Pool 本身可以保存为：

```text
conflict
```

或者直接拒绝启用。

MVP 建议：

```text
拒绝启用
```

除非管理员明确处理冲突。

---

# 39. Network Pool API

## GET

```text
GET /api/nodes/:nodeId/network-pools
```

返回：

```json
[
  {
    "id": "pool-1",
    "name": "default",
    "addressPool": "10.128.0.0/9",
    "projectPrefix": 24,
    "priority": 10,
    "status": "active",
    "capacity": 32768,
    "allocated": 120,
    "available": 32648
  }
]
```

---

# 40. Add Pool API

```text
POST /api/nodes/:nodeId/network-pools
```

示例：

```json
{
  "name": "expansion-01",
  "addressPool": "10.64.0.0/10",
  "projectPrefix": 24,
  "priority": 20
}
```

---

# 41. Update Pool API

```text
PATCH /api/nodes/:nodeId/network-pools/:poolId
```

允许修改：

```text
name
description
priority
status
reuse_enabled
reuse_delay_seconds
```

Pool 已在使用时禁止修改：

```text
address_pool
project_prefix
```

---

# 42. Delete Pool API

```text
DELETE /api/nodes/:nodeId/network-pools/:poolId
```

仅允许：

```text
没有 active
没有 reserved
没有 failed-but-retained
没有 deleting
```

项目的 Pool 删除。

否则：

```text
IPAM_POOL_IN_USE
```

---

# 43. Recheck Pool

```text
POST /api/nodes/:nodeId/network-pools/:poolId/check
```

执行：

- Host Route Check
- Docker Network Check
- Pool Overlap Check
- Gateway Validation
- Capacity Calculation

---

# 44. System Default API

```text
GET /api/system/container-network
PUT /api/system/container-network
```

系统默认只影响：

```text
未来新 Node / 新默认 Pool
```

不能自动修改现有 Node Pool。

---

# 45. Project Network API

```text
GET /api/projects/:projectId/network
```

返回：

```json
{
  "nodeId": "node-a",
  "poolId": "pool-1",
  "networkName": "maple-31c6635dffc5",
  "subnet": "10.128.15.0/24",
  "gateway": "10.128.15.1",
  "status": "active"
}
```

---

# 46. Agent Tasks

新增：

```text
network.check
network.create
network.delete
network.inspect
network.list_managed
```

---

# 47. network.create

请求：

```json
{
  "networkName": "maple-31c6635dffc5",
  "driver": "bridge",
  "subnet": "10.128.15.0/24",
  "gateway": "10.128.15.1"
}
```

Agent：

1. 检查同名 Network。
2. 如果不存在则创建。
3. 如果存在，inspect。
4. 参数完全一致则幂等成功。
5. 参数不同则 conflict。
6. 创建后再次 inspect。
7. 返回 Network ID。

---

# 48. Agent 幂等

重复发送：

```text
network.create
```

不得创建重复 Network。

如果：

```text
Name 相同
Subnet 相同
Gateway 相同
```

返回：

```text
success
already_exists = true
```

如果：

```text
Name 相同
Subnet 不同
```

返回：

```text
NETWORK_CONFLICT
```

---

# 49. Project 创建状态

推荐：

```text
Project Created
↓
Subnet Reserved
↓
Network Creating
↓
Network Active
↓
Compose Deploying
↓
Running
```

Project Network 生命周期独立于单次 Deployment。

---

# 50. Redeploy

以下操作：

```text
Restart
Redeploy
Upgrade
Recreate
Rollback
```

必须继续使用：

```text
原 cm_project_networks.subnet
```

禁止重新申请。

---

# 51. Deployment Failure

如果：

```text
Subnet Reserved
↓
Docker Network Created
↓
Compose Failed
```

不要自动立即释放 subnet。

因为用户可能：

```text
Retry Deployment
```

应该保留：

```text
Project Network
```

只有 Project 真正删除后才释放。

---

# 52. Project 删除

```text
Delete Project
↓
Stop Compose
↓
Remove Containers
↓
确认 Network 无关联容器
↓
Delete Docker Network
↓
cm_project_networks = released
↓
设置 reuse_after
```

如果 Network 删除失败：

```text
status = failed
```

不能回收 subnet。

---

# 53. Project Move Node

未来支持：

```text
Node A
→
Node B
```

因为 Node-local IPAM：

目标 Node 重新申请 subnet。

可以尝试：

```text
优先复用相同 subnet
```

但不能保证。

MVP：

```text
目标 Node 正常申请新的 /24
```

应用不得依赖固定容器 IP。

---

# 54. 对账

Agent 定期上报：

```text
Managed Docker Networks
```

Container Manager 对比数据库。

---

# 55. Drift Case A

数据库：

```text
active
```

Docker：

```text
存在
```

结果：

```text
healthy
```

---

# 56. Drift Case B

数据库：

```text
active
```

Docker：

```text
不存在
```

结果：

```text
missing
```

生成：

```text
network drift event
```

---

# 57. Drift Case C

数据库：

```text
不存在
```

Docker：

```text
存在 maple-* network
```

结果：

```text
orphan
```

默认：

```text
不要自动删除
```

---

# 58. Drift Case D

数据库 subnet：

```text
10.128.15.0/24
```

Docker subnet：

```text
10.128.16.0/24
```

结果：

```text
conflict / drift
```

禁止自动覆盖。

---

# 59. 后台系统 UI

路径建议：

```text
Settings
└── Container
    └── Network
```

显示：

```text
Default IPv4 Pool
10.128.0.0/9

Project Prefix
/24

Allocation
Sequential

Reuse Released Subnets
Enabled

Reuse Delay
10 minutes
```

---

# 60. Node Network Pool UI

Node 页面：

```text
Node
└── Network Pools
```

展示表格：

```text
Name
CIDR
Prefix
Priority
Status
Capacity
Allocated
Available
Usage
Conflict
```

按钮：

```text
Add Pool
Edit
Set Draining
Disable
Recheck
Delete
```

---

# 61. Add Pool UI

字段：

```text
Name

IPv4 Pool

Project Prefix

Priority

Reuse Released Subnets

Reuse Delay
```

实时显示：

```text
Capacity
```

例如：

```text
10.64.0.0/10
/24

16384 Projects
```

---

# 62. Project Network UI

Project 页面：

```text
Container Network
────────────────────────
Node
node-01

Pool
default

Network
maple-31c6635dffc5

Subnet
10.128.15.0/24

Gateway
10.128.15.1

Status
Active
```

---

# 63. Pool Usage

例如：

```text
Pool:
10.128.0.0/9

Capacity:
32768

Allocated:
128

Reserved:
2

Reuse Waiting:
3

Available:
32635
```

使用率：

```text
0.40%
```

---

# 64. Pool Exhaustion Alert

建议告警：

```text
80%
90%
95%
100%
```

例如：

```text
IPAM Pool usage reached 80%.
Consider adding another Network Pool.
```

100%：

```text
status = exhausted
```

---

# 65. Pool Draining UI

设置 draining 时提示：

```text
Existing projects will continue running.

New projects will not be assigned to this pool.
```

---

# 66. Pool 删除规则

允许删除：

```text
allocated = 0
reserved = 0
deleting = 0
failed-retained = 0
```

否则禁止。

---

# 67. CIDR 校验

统一实现 Network Validation Service。

校验：

- IPv4 CIDR 合法
- Network address 对齐
- Prefix 合法
- project_prefix > pool_prefix
- project_prefix <= 30
- Subnet 完全位于 Pool 内
- Gateway 位于 Subnet 内
- Gateway 不是 network address
- Gateway 不是 broadcast
- Pool 和本 Node 其他 Pool 不 overlap

---

# 68. 禁止地址范围

默认禁止：

```text
0.0.0.0/8
127.0.0.0/8
169.254.0.0/16
224.0.0.0/4
240.0.0.0/4
```

---

# 69. 特殊地址提示

对于：

```text
100.64.0.0/10
```

允许配置但警告：

```text
This range is commonly used by CGNAT and overlay/VPN software.
```

默认不推荐。

---

# 70. RFC1918

推荐只使用：

```text
10.0.0.0/8
172.16.0.0/12
192.168.0.0/16
```

默认：

```text
10.128.0.0/9
```

---

# 71. 已有 Docker Network 兼容

升级后：

只自动管理符合 Maple Gateway managed metadata / name 规则的网络。

例如：

```text
maple-*
```

第三方 Docker Networks：

```text
只参与冲突检测
不自动删除
不自动接管
```

---

# 72. Legacy Project Import

如果现有项目已经存在：

```text
<compose>_default
```

提供 migration/import 流程。

扫描：

```text
Project
↓
Node
↓
Docker Network
↓
Subnet
```

写入：

```text
cm_project_networks
```

允许现有 subnet：

```text
不在 10.128.0.0/9
```

继续运行。

不强制迁移。

---

# 73. IPAM 审计日志

记录：

```text
node.network_pool.created
node.network_pool.updated
node.network_pool.draining
node.network_pool.disabled
node.network_pool.exhausted
node.network_pool.conflict

project.network.reserved
project.network.created
project.network.failed
project.network.deleted
project.network.released

project.network.drift_detected
project.network.orphan_detected
```

字段：

```text
node_id
pool_id
project_id
subnet
network_name
task_id
operator
timestamp
error
```

---

# 74. Metrics

建议：

```text
maple_ipam_pool_capacity

maple_ipam_pool_allocated

maple_ipam_pool_reserved

maple_ipam_pool_available

maple_ipam_pool_usage_ratio

maple_ipam_allocation_total

maple_ipam_allocation_failed_total

maple_ipam_release_total

maple_ipam_conflict_total

maple_ipam_drift_total

maple_ipam_orphan_total
```

---

# 75. Prometheus 标签

允许：

```text
node_id
pool_id
status
```

不要：

```text
project_id
subnet
```

作为高基数标签。

---

# 76. 错误码

建议：

```text
IPAM_POOL_NOT_FOUND

IPAM_POOL_DISABLED

IPAM_POOL_DRAINING

IPAM_POOL_INVALID

IPAM_POOL_IN_USE

IPAM_POOL_EXHAUSTED

IPAM_ALL_POOLS_EXHAUSTED

IPAM_POOL_OVERLAP

IPAM_SUBNET_CONFLICT

IPAM_SUBNET_RESERVED

IPAM_HOST_ROUTE_CONFLICT

IPAM_DOCKER_NETWORK_CONFLICT

IPAM_NETWORK_CREATE_FAILED

IPAM_NETWORK_DELETE_FAILED

IPAM_NETWORK_DRIFT

IPAM_REUSE_DELAY_ACTIVE

IPAM_INVALID_PREFIX

IPAM_INVALID_CIDR
```

---

# 77. 单元测试

必须覆盖：

## CIDR

```text
10.128.0.0/9
```

合法。

---

## Capacity

```text
/9 → /24
= 32768
```

---

## Index

```text
0
→ 10.128.0.0/24

255
→ 10.128.255.0/24

256
→ 10.129.0.0/24

32767
→ 10.255.255.0/24

32768
→ out of range
```

---

# 78. Multi Pool 测试

Node A：

```text
Pool 1
10.128.0.0/9

Pool 2
10.64.0.0/10
```

Pool 1 模拟耗尽。

下一项目必须：

```text
自动进入 Pool 2
```

---

# 79. Priority 测试

```text
Pool A priority = 20

Pool B priority = 10
```

即使 Pool A 先创建，也必须优先：

```text
Pool B
```

---

# 80. Draining 测试

Pool：

```text
status = draining
```

已有项目：

```text
继续正常
```

新项目：

```text
不得进入
```

---

# 81. Disabled 测试

```text
status = disabled
```

不能分配。

---

# 82. Conflict 测试

Pool 和 Host Route overlap：

```text
status = conflict
```

不能分配。

---

# 83. Pool Overlap 测试

同 Node：

```text
10.128.0.0/9
```

新增：

```text
10.128.0.0/10
```

必须拒绝。

不同 Node：

必须允许。

---

# 84. 并发测试

同一个 Node / Pool：

并发：

```text
100
1000
```

项目创建。

要求：

```text
没有重复 subnet
```

---

# 85. 多 Container Manager 测试

启动多个 CM 实例：

```text
CM-1
CM-2
CM-3
```

同时分配。

验证：

```text
PostgreSQL Lock
+
Unique Constraint
```

有效。

---

# 86. Release 测试

删除：

```text
Project A
```

Subnet：

```text
released
```

在：

```text
reuse_after
```

之前不得重新分配。

---

# 87. Agent 幂等测试

相同 network.create：

重复 10 次。

结果：

```text
只有一个 Docker Network
```

---

# 88. Restart 测试

依次重启：

- Container Manager
- Agent
- Docker
- Node

确保：

```text
Project → Pool → Subnet
```

绑定不丢失。

---

# 89. Drift 测试

模拟：

- Network 手工删除
- Network subnet 不一致
- orphan network
- DB 缺记录
- Agent 离线
- Docker 重启

要求：

```text
正确检测
不危险自动删除
```

---

# 90. Phase 1：Network Core

任务：

- [ ] IPv4 CIDR Parser
- [ ] CIDR Normalize
- [ ] Prefix Validation
- [ ] Capacity Calculator
- [ ] CIDR Overlap
- [ ] CIDR Contains
- [ ] Subnet Index Calculator
- [ ] Gateway Calculator
- [ ] Reserved Range Validator
- [ ] 单元测试

---

# 91. Phase 2：数据库模型

任务：

- [ ] 创建 `cm_node_network_pools`
- [ ] 支持 Node → Many Pools
- [ ] 删除 `UNIQUE(node_id)`
- [ ] 增加 `UNIQUE(node_id, name)`
- [ ] 增加 Pool 状态
- [ ] 增加 `priority`
- [ ] 增加 `next_index`
- [ ] 创建 `cm_project_networks`
- [ ] 增加 `(node_id, subnet)` 唯一索引
- [ ] 增加 `pool_id`
- [ ] 增加 migration
- [ ] repository/service

---

# 92. Phase 3：IPAM Allocator

任务：

- [ ] 获取 Node Active Pools
- [ ] 按 priority 排序
- [ ] 实现 Sequential Allocation
- [ ] 实现 next_index
- [ ] 实现 Pool Exhausted
- [ ] 自动切换下一个 Pool
- [ ] 实现 Released Reuse
- [ ] 实现 Reuse Delay
- [ ] 实现 PostgreSQL `FOR UPDATE`
- [ ] 并发测试
- [ ] 多 CM 测试

---

# 93. Phase 4：Network Pool 管理

任务：

- [ ] Create Pool
- [ ] Update Pool
- [ ] Set Active
- [ ] Set Draining
- [ ] Set Disabled
- [ ] Auto Exhausted
- [ ] Conflict 状态
- [ ] Delete Pool
- [ ] Pool In Use Validation
- [ ] Node 内 Pool overlap 检测

---

# 94. Phase 5：Agent Network

任务：

- [ ] `network.check`
- [ ] `network.create`
- [ ] `network.delete`
- [ ] `network.inspect`
- [ ] `network.list_managed`
- [ ] Docker 幂等创建
- [ ] 删除安全检查
- [ ] Inspect 验证
- [ ] 标准错误映射

---

# 95. Phase 6：Project Lifecycle

任务：

- [ ] Create Project 自动申请 subnet
- [ ] Network Reserved
- [ ] 创建 Docker Network
- [ ] Compose External Network
- [ ] Deployment Retry 保持 subnet
- [ ] Redeploy 保持 subnet
- [ ] Upgrade 保持 subnet
- [ ] Delete Project 删除 Network
- [ ] Release subnet
- [ ] reuse_after

---

# 96. Phase 7：Conflict Detection

任务：

- [ ] 获取 Host Routes
- [ ] 获取 Docker Networks
- [ ] 获取主要 Interface Networks
- [ ] Host Route Overlap
- [ ] Existing Docker Network Overlap
- [ ] Pool-vs-Pool Overlap
- [ ] 注册 Node 时检测
- [ ] Pool 新增时检测
- [ ] Manual Recheck

---

# 97. Phase 8：System Settings

任务：

- [ ] Default Pool 设置
- [ ] Default Prefix
- [ ] Default Allocation Mode
- [ ] Default Reuse
- [ ] Default Reuse Delay
- [ ] 新 Node 自动初始化
- [ ] 不修改现有 Pool
- [ ] UI validation

---

# 98. Phase 9：Node Pool UI

任务：

- [ ] Pool List
- [ ] Add Pool
- [ ] Edit Pool
- [ ] Priority
- [ ] Draining
- [ ] Disable
- [ ] Recheck
- [ ] Delete
- [ ] Capacity
- [ ] Usage
- [ ] Conflict Reason
- [ ] Exhausted Status

---

# 99. Phase 10：Project UI

任务：

- [ ] 展示 Pool
- [ ] 展示 Subnet
- [ ] 展示 Gateway
- [ ] 展示 Docker Network
- [ ] 展示 Network Status
- [ ] 展示 Error
- [ ] Recheck
- [ ] Repair

---

# 100. Phase 11：Reconciliation

任务：

- [ ] Agent 定期上报
- [ ] missing network
- [ ] orphan network
- [ ] subnet mismatch
- [ ] gateway mismatch
- [ ] managed/unmanaged 区分
- [ ] Drift Event
- [ ] Repair Task
- [ ] 默认禁止危险自动删除

---

# 101. Phase 12：Observability

任务：

- [ ] Audit Log
- [ ] Metrics
- [ ] Pool Usage
- [ ] Pool Exhaustion Warning
- [ ] Conflict Warning
- [ ] Drift Warning
- [ ] Allocation Failure Alert

---

# 102. MVP 必须实现

MVP：

- Node-local IPAM
- Node 多 Pool
- 默认 `10.128.0.0/9`
- Project `/24`
- Sequential Allocation
- Pool Priority
- Pool Exhausted 自动切换
- Pool Active / Draining / Disabled / Exhausted / Conflict
- PostgreSQL 原子预留
- Docker bridge Network
- Compose External Network
- Project 删除回收
- Released Delay
- Host Route Conflict Detection
- Docker Network Conflict Detection
- Pool overlap
- System Default
- Node 自定义 Pool
- Agent Idempotency
- Basic Reconciliation
- Basic UI
- 并发测试

---

# 103. MVP 暂不实现

不实现：

- Cross-node L3
- Overlay Network
- VXLAN
- BGP
- Global IPAM
- Kubernetes CNI
- IPv6
- Container IPAM
- Static Container IP
- Project 多 Network
- 自动 Project Network Migration
- 跨 Node 保留相同 subnet
- 自动重编号

---

# 104. 最终默认配置

```text
IPAM Scope:
Node-local

Default Pool:
10.128.0.0/9

Project Prefix:
24

Default Pool Capacity:
32768 Projects

Node Pools:
Multiple

Allocation Unit:
Project

Allocation Strategy:
Sequential

Pool Selection:
Priority ASC

Pool Exhaustion:
Automatically try next active pool

Gateway:
First usable IP

Container IP:
Docker automatic

Cross-node Routing:
Disabled

Global CIDR Uniqueness:
Not Required

Node CIDR Uniqueness:
Required

Pool Expansion:
Add new pool

Pool Retirement:
Draining → Disabled

Released Subnet Reuse:
Enabled

Reuse Delay:
600 seconds

State Authority:
Container Manager Database
```

---

# 105. 最终原则

Maple Gateway 的项目网络模型最终固定为：

```text
Node
└── Multiple Network Pools
    ├── Pool A
    │   ├── /24 Project
    │   ├── /24 Project
    │   └── /24 Project
    │
    └── Pool B
        ├── /24 Project
        └── /24 Project
```

扩容：

```text
Add Pool
```

而不是：

```text
Resize Existing Pool
```

项目：

```text
Project → 一个 /24
```

容器：

```text
Docker 自动分配 IP
```

节点之间：

```text
不做三层互通
允许重复 CIDR
```

同一节点：

```text
所有 Pool 和 Project Subnet 必须互不冲突
```

这套方案作为 Maple Gateway / Container Manager 项目级 IPAM 的正式最新版架构。
