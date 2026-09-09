# Maple Gateway 开发计划任务

> 项目名称：Maple Gateway
> 项目定位：Multi-Tenant Gateway
> 核心目标：支持 Cloudflare SaaS 模式与 Direct TLS 模式
> 后端：Go 1.24+ + Fiber v3 + Ent + PostgreSQL 16 + Redis 7
> 数据平面：Go `net/http` + `httputil.ReverseProxy`
> 前端：React 19 + TypeScript + Vite + Tailwind CSS 4
> 架构：前后端分离，Data Plane / Control Plane 分离
> 默认生产模式：Cloudflare SaaS
> 扩展模式：Direct TLS + Dynamic SNI Certificate

------------------------------------------------------------------------

# 一、开发目标

Maple Gateway 需要支持两种 HTTPS 接入模式。

## 1. Cloudflare SaaS 模式

```text
Customer Domain
      ↓
Cloudflare for SaaS
      ↓
    HTTPS
      ↓
Maple Gateway
      ↓
Tenant Container
```

特点：

- Cloudflare 管理租户公网证书
- Cloudflare 完成公网 TLS Termination
- Maple Gateway 根据 HTTP Host 识别 Tenant
- Maple Gateway 与 Cloudflare Origin 之间使用统一 Origin TLS
- Maple Gateway 不需要为每个租户保存公网私钥
- 作为 Phase 1 默认模式

## 2. Direct TLS 模式

```text
Customer Domain
      ↓
TLS ClientHello
      ↓
     SNI
      ↓
Maple Gateway
      ↓
Dynamic Certificate
      ↓
HTTP Host
      ↓
Tenant Routing
      ↓
Tenant Container
```

特点：

- Maple Gateway 直接接收公网 HTTPS
- 根据 SNI 动态选择证书
- 每个 Tenant Domain 可以拥有独立 Certificate
- 支持 Managed Certificate / Manual Certificate
- Certificate 必须进入 Memory Certificate Cache
- TLS 握手不能实时查询 PostgreSQL
- 作为 Phase 2 能力

------------------------------------------------------------------------

# 二、总体架构

```text
                            Internet
                               │
              ┌────────────────┴────────────────┐
              │                                 │
              ▼                                 ▼
     Cloudflare SaaS Mode                Direct TLS Mode
              │                                 │
              │ TLS terminated                  │ TLS ClientHello
              ▼                                 ▼
        Cloudflare Edge                    SNI Resolver
              │                                 │
              │ HTTPS Origin                     ▼
              │                         Certificate Cache
              │                                 │
              └────────────────┬────────────────┘
                               ▼
                        Maple Gateway
                               │
                               ▼
                           HTTP Host
                               │
                               ▼
                             Domain
                               │
                               ▼
                             Tenant
                               │
                               ▼
                            Service
                               │
                               ▼
                         Traffic Policy
                               │
                               ▼
                         Load Balancer
                               │
                               ▼
                            Instance
                               │
                               ▼
                             Node
```

------------------------------------------------------------------------

# 三、开发原则

1. Cloudflare SaaS 是默认生产接入方式。
2. Direct TLS 是独立能力，不影响 Cloudflare 模式。
3. 两种模式共用 Domain / Tenant / Service / Route 模型。
4. TLS 模式属于 Domain 配置。
5. SNI 负责证书选择，Host 负责 Tenant 路由。
6. TLS Handshake 不访问 PostgreSQL。
7. Certificate 使用 Memory Cache。
8. Ent 负责 Certificate / Domain / TLS Config 持久化。
9. Private Key 不通过普通 Admin API 返回。
10. Cloudflare Token 不写入日志。
11. Cloudflare 模式不要求 Maple Gateway 保存租户公网证书。
12. Direct TLS 模式必须支持证书热加载。
13. Domain 状态与 Certificate 状态分离。
14. 证书失败不能影响其他 Tenant。
15. Data Plane 不依赖 React Frontend。

------------------------------------------------------------------------

# 四、Phase 0：基础模型准备

## 任务 0.1：扩展 Domain 数据模型

新增字段：

```text
Domain
├── id
├── tenant_id
├── service_id
├── hostname
├── tls_mode
├── status
├── certificate_id
├── cloudflare_hostname_id
├── verification_status
├── certificate_status
├── created_at
└── updated_at
```

`tls_mode`：

```text
cloudflare
managed
manual
disabled
```

预留：

```text
passthrough
```

但 Phase 1 / Phase 2 暂不实现 TLS Passthrough。

## 任务 0.2：Domain 状态机

建议状态：

```text
pending
   ↓
verifying
   ↓
certificate_pending
   ↓
active
```

失败状态：

```text
verification_failed
certificate_failed
failed
disabled
```

## 任务 0.3：Certificate 数据模型

新增 Ent Schema：

```text
server/
└── ent/
    └── schema/
        └── certificate.go
```

字段：

```text
Certificate
├── id
├── domain_id
├── hostname
├── source
├── status
├── certificate_pem
├── private_key_encrypted
├── issuer
├── serial_number
├── issued_at
├── expires_at
├── last_renewed_at
├── last_error
├── created_at
└── updated_at
```

`source`：

```text
cloudflare
acme
manual
origin
```

## 任务 0.4：Ent Schema

至少新增：

```text
ent/schema/
├── tenant.go
├── domain.go
├── service.go
├── certificate.go
└── gateway_config.go
```

关系：

```text
Tenant
  ↓
Domain
  ↓
Certificate
```

以及：

```text
Domain
  ↓
Service
```

------------------------------------------------------------------------

# 五、Phase 1：Cloudflare SaaS 模式

Phase 1 优先完成。

目标：

> 租户添加自定义域名后，由 Cloudflare for SaaS 管理公网证书，Maple Gateway 根据 Host 将流量路由到正确 Tenant Container。

## 任务 1.1：Cloudflare 配置模块

新增：

```text
server/internal/cloudflare/
├── client.go
├── service.go
├── hostname.go
├── validation.go
├── sync.go
└── errors.go
```

职责：

- Cloudflare API Client
- 创建 Custom Hostname
- 查询 Custom Hostname
- 删除 Custom Hostname
- 查询 Verification 状态
- 查询 Certificate 状态
- 同步 Domain 状态

## 任务 1.2：Cloudflare 配置项

新增配置：

```yaml
cloudflare:
  enabled: true
  account_id: ""
  zone_id: ""
  api_token: ""
  fallback_origin: "origin.example.com"
```

支持环境变量：

```text
MAPLE_CLOUDFLARE_ACCOUNT_ID
MAPLE_CLOUDFLARE_ZONE_ID
MAPLE_CLOUDFLARE_API_TOKEN
MAPLE_CLOUDFLARE_FALLBACK_ORIGIN
```

要求：

- API Token 不返回给 Frontend
- API Token 不写日志
- 配置校验必须在服务启动时完成

## 任务 1.3：添加 Tenant Domain

接口：

```text
POST /api/admin/domains
```

请求：

```json
{
  "tenant_id": "tenant_1001",
  "service_id": "storefront",
  "hostname": "shop-a.com",
  "tls_mode": "cloudflare"
}
```

处理流程：

```text
Frontend
   ↓
Management API
   ↓
Validate Hostname
   ↓
Ent
   ↓
Create Domain
   ↓
Cloudflare API
   ↓
Create Custom Hostname
   ↓
Save Cloudflare Hostname ID
   ↓
Domain = verifying
```

## 任务 1.4：Cloudflare Hostname 状态同步

实现：

```text
GET /api/admin/domains/:id/status
```

以及内部同步任务：

```text
Cloudflare
    ↓
Custom Hostname Status
    ↓
Maple Gateway
    ↓
Domain Status Update
```

需要同步：

- hostname verification
- certificate status
- SSL status
- last error

## 任务 1.5：Domain Verification 信息

接口：

```text
GET /api/admin/domains/:id/verification
```

Frontend 展示：

- 当前 Domain
- 验证状态
- DNS 验证信息
- Certificate 状态
- 最后同步时间
- 错误原因

## 任务 1.6：删除 Domain

接口：

```text
DELETE /api/admin/domains/:id
```

流程：

```text
Disable Route
   ↓
Remove Route Cache
   ↓
Delete Cloudflare Custom Hostname
   ↓
Delete / Archive Domain
```

## 任务 1.7：Cloudflare → Maple Gateway Origin TLS

默认：

```text
Cloudflare
    ↓
HTTPS
    ↓
origin.example.com
    ↓
Maple Gateway
```

开发任务：

- HTTPS Listener
- Origin Certificate 加载
- Private Key 加载
- TLS 版本配置
- 使用 Go 安全默认 Cipher
- Graceful Reload

## 任务 1.8：Trusted Proxy

新增：

```text
server/internal/security/trusted_proxy.go
```

处理：

- Cloudflare Origin IP 校验
- CF-Connecting-IP
- X-Forwarded-For
- X-Forwarded-Proto
- Host
- 防止伪造 Trusted Proxy Header

## 任务 1.9：Host Routing

Maple Gateway：

```text
Host
 ↓
Normalize
 ↓
Domain Cache
 ↓
Tenant
 ↓
Service
 ↓
Instance
```

Host Normalize：

- lowercase
- port stripping
- trailing dot
- IDN / Punycode
- invalid hostname
- unknown hostname

## 任务 1.10：Cloudflare SaaS MVP 验收

验收条件：

- 多租户域名 HTTPS 正常
- Cloudflare 证书有效
- Maple Gateway 不保存租户公网私钥
- Host Routing 正确
- 删除 Domain 后立即停止路由
- Disabled Tenant 不再接流量
- Certificate Pending 时不可进入 Active Route
- Cloudflare Origin HTTPS 正常

------------------------------------------------------------------------

# 六、Phase 2：Direct TLS 模式

目标：

> Customer Domain 可以直接解析到 Maple Gateway，由 Maple Gateway 根据 SNI 动态加载对应证书并完成 TLS Termination。

## 任务 2.1：Certificate 模块

新增：

```text
server/internal/certificate/
├── service.go
├── store.go
├── cache.go
├── loader.go
├── sni.go
├── validator.go
├── encryption.go
├── renew.go
└── errors.go
```

职责：

- Certificate CRUD
- Certificate Validation
- Private Key Encryption
- Certificate Memory Cache
- Dynamic SNI
- Certificate Hot Reload
- Renewal State

## 任务 2.2：Certificate Memory Cache

禁止：

```text
TLS Handshake
   ↓
Ent
   ↓
PostgreSQL
```

正确：

```text
PostgreSQL
   ↓
Certificate Loader
   ↓
Memory Certificate Cache
```

TLS：

```text
ClientHello
   ↓
SNI
   ↓
Memory Cache
   ↓
tls.Certificate
```

## 任务 2.3：Go Dynamic SNI

使用：

```text
tls.Config.GetCertificate
```

逻辑：

```text
ClientHello
    ↓
hello.ServerName
    ↓
Normalize Hostname
    ↓
Certificate Cache Lookup
    ↓
Certificate
```

Cache Miss：

- 不访问 PostgreSQL
- TLS Handshake Error
- 写 TLS / Security Log
- 增加 Metrics

## 任务 2.4：Manual Certificate

接口：

```text
POST /api/admin/domains/:id/certificate
```

支持：

- Certificate PEM
- Private Key PEM
- Certificate Chain

保存前验证：

- Certificate / Private Key 匹配
- SAN 包含当前 Domain
- Certificate 未过期
- PEM 格式正确
- Chain 可解析

保存：

```text
Private Key
   ↓
Encrypt
   ↓
PostgreSQL
   ↓
Certificate Sync
   ↓
Memory Certificate Cache
```

## 任务 2.5：Managed Certificate 抽象

Provider 接口：

```text
CertificateProvider
├── Issue()
├── Renew()
├── Revoke()
└── Status()
```

Provider：

```text
CloudflareProvider
ManualProvider
ACMEProvider
```

ACME 后续实现。

## 任务 2.6：Certificate 热更新

```text
API
 ↓
Ent
 ↓
PostgreSQL
 ↓
Certificate Event
 ↓
Memory Cache Replace
```

要求：

- 不重启 Maple Gateway
- Existing Connection 不受影响
- 新 TLS Connection 使用新证书

## 任务 2.7：SNI / Host 一致性

Direct TLS：

```text
SNI = shop-a.com
Host = shop-a.com
```

异常：

```text
SNI = shop-a.com
Host = shop-b.com
```

默认返回：

```text
421 Misdirected Request
```

配置：

```yaml
tls:
  enforce_sni_host_match: true
```

## 任务 2.8：Direct TLS Domain 流程

Manual：

```text
Create Domain
   ↓
Domain Pending
   ↓
Upload Certificate
   ↓
Validate Certificate
   ↓
Save Certificate
   ↓
Update Certificate Cache
   ↓
Domain Active
```

Managed：

```text
Create Domain
   ↓
Verify Ownership
   ↓
Issue Certificate
   ↓
Certificate Cache
   ↓
Domain Active
```

## 任务 2.9：Certificate Admin API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/certificates` | Certificate 列表 |
| GET | `/api/admin/certificates/:id` | Certificate 详情 |
| POST | `/api/admin/domains/:id/certificate` | 上传 Certificate |
| DELETE | `/api/admin/domains/:id/certificate` | 删除 Certificate |
| POST | `/api/admin/certificates/:id/reload` | 重载 Certificate |
| POST | `/api/admin/certificates/:id/renew` | 请求续期 |
| GET | `/api/admin/certificates/:id/status` | Certificate 状态 |

API 不返回：

```text
private_key
private_key_encrypted
```

## 任务 2.10：Certificate Frontend

建议集成到：

```text
DomainsPage.tsx
```

Domain Detail 展示：

- Domain
- TLS Mode
- Verification
- Certificate Source
- Certificate Status
- Issuer
- Issued At
- Expires At
- Last Renewed
- Last Error

------------------------------------------------------------------------

# 七、Phase 3：统一 TLS 管理层

两种模式统一进入：

```text
TLS Manager
```

逻辑：

```text
Domain
 ↓
tls_mode
 ├── cloudflare
 ├── managed
 ├── manual
 └── disabled
```

统一 Domain Service：

```text
CreateDomain()
VerifyDomain()
EnableDomain()
DisableDomain()
DeleteDomain()
SyncTLSStatus()
```

建议目录：

```text
server/internal/
├── domain/
├── cloudflare/
├── certificate/
└── tls/
    ├── manager.go
    ├── config.go
    └── listener.go
```

------------------------------------------------------------------------

# 八、Route Cache 与 Certificate Cache

必须分离。

Route Cache：

```text
hostname
   ↓
Tenant
   ↓
Service
   ↓
Instance
```

Certificate Cache：

```text
hostname
   ↓
tls.Certificate
```

流程：

```text
TLS ClientHello
    ↓
Certificate Cache
```

TLS 完成后：

```text
HTTP Host
    ↓
Route Cache
```

------------------------------------------------------------------------

# 九、统一运行流程：Cloudflare SaaS

```text
Browser
   ↓
HTTPS
   ↓
Cloudflare
   ↓
Cloudflare Certificate
   ↓
HTTPS Origin
   ↓
Maple Gateway
   ↓
Host = shop-a.com
   ↓
Route Cache
   ↓
Tenant A
   ↓
Service
   ↓
Healthy Instance
   ↓
Reverse Proxy
```

------------------------------------------------------------------------

# 十、统一运行流程：Direct TLS

```text
Browser
   ↓
TLS ClientHello
   ↓
SNI = shop-a.com
   ↓
Certificate Cache
   ↓
Certificate A
   ↓
TLS Handshake
   ↓
Host = shop-a.com
   ↓
SNI / Host Validate
   ↓
Route Cache
   ↓
Tenant A
   ↓
Service
   ↓
Healthy Instance
   ↓
Reverse Proxy
```

------------------------------------------------------------------------

# 十一、数据库任务

需要新增或更新：

```text
tenants
domains
services
certificates
gateway_configs
audit_logs
```

重点索引：

```text
domains.hostname UNIQUE
certificates.hostname
certificates.expires_at
domains.tenant_id
domains.service_id
```

------------------------------------------------------------------------

# 十二、Redis 任务

Phase 1：

Redis 不进入 TLS 热路径。

Phase 3 HA：

```text
CertificateChanged
DomainChanged
RouteChanged
```

可通过：

```text
Control Plane
   ↓
Redis Pub/Sub
   ↓
Gateway A
Gateway B
Gateway C
   ↓
Refresh Local Cache
```

------------------------------------------------------------------------

# 十三、安全任务

必须实现：

- Private Key 加密存储
- Admin API 不返回 Private Key
- Cloudflare Token 日志脱敏
- Hostname Validation
- SNI Validation
- SNI / Host Match
- Trusted Proxy
- Origin IP 访问限制
- TLS 最低版本配置
- Certificate PEM Validation
- Certificate / Key Match
- Certificate Expiry Validation
- Audit Log
- Rate Limit
- SSRF Prevention

------------------------------------------------------------------------

# 十四、Origin 防绕过

Cloudflare SaaS 模式：

```text
User
 ↓
Direct Origin IP
 ↓
Maple Gateway
```

默认不允许。

建议：

```text
Cloudflare Mode
     ↓
Only Trusted Cloudflare Origin Traffic
```

可通过：

- Firewall
- Cloudflare IP Allowlist
- Origin Authentication
- Secret Header
- mTLS（后期）

------------------------------------------------------------------------

# 十五、Metrics 任务

新增：

```text
maple_tls_handshakes_total
maple_tls_handshake_errors_total
maple_tls_certificate_cache_hits_total
maple_tls_certificate_cache_misses_total
maple_tls_certificate_expiring
maple_domain_verification_errors_total
maple_cloudflare_api_errors_total
```

注意避免 hostname 造成 Prometheus 高基数标签。

------------------------------------------------------------------------

# 十六、日志任务

新增：

```text
TLS Log
Certificate Log
Domain Verification Log
Cloudflare Sync Log
```

日志包含：

- domain_id
- tenant_id
- hostname
- tls_mode
- certificate_id
- status
- error_code

严禁包含：

- Private Key
- Cloudflare API Token

------------------------------------------------------------------------

# 十七、Cloudflare SaaS 测试计划

1. 添加 Domain
2. Cloudflare Custom Hostname 创建
3. Domain Verification
4. Certificate Pending
5. Certificate Active
6. HTTPS 成功
7. Host Routing
8. Tenant Disable
9. Domain Disable
10. Domain Delete
11. Cloudflare API Error
12. Certificate Provision Failure
13. Gateway 重启后 Route 恢复
14. Origin TLS
15. Direct Origin Block

------------------------------------------------------------------------

# 十八、Direct TLS 测试计划

1. 单 Domain Certificate
2. 多 Domain Certificate
3. 100 Domain SNI
4. 1000 Domain SNI
5. Unknown SNI
6. Expired Certificate
7. Wrong Certificate
8. Certificate / Key mismatch
9. SNI / Host mismatch
10. Certificate Hot Reload
11. Replace Certificate
12. Gateway Restart
13. Concurrent TLS Handshake
14. WebSocket over HTTPS
15. SSE over HTTPS
16. HTTP/2
17. Certificate Cache Miss
18. Domain Disable
19. Tenant Disable
20. Certificate Expiry Warning

------------------------------------------------------------------------

# 十九、性能测试

Cloudflare 模式：

- HTTPS Origin
- HTTP Keep-Alive
- Host Routing
- Route Cache
- Reverse Proxy

Direct TLS：

- TLS Handshake RPS
- Concurrent Handshake
- Certificate Cache Lookup
- Memory Usage
- Certificate Reload

指标：

- RPS
- P50
- P95
- P99
- TLS Handshake Latency
- CPU
- Memory
- GC
- Active Connections
- Certificate Cache Hit Rate
- Route Cache Hit Rate

------------------------------------------------------------------------

# 二十、前端开发任务

Domains 页面字段：

```text
Domain
Tenant
Service
TLS Mode
Verification Status
Certificate Status
Route Status
Created At
```

Domain Detail：

```text
Overview
DNS Verification
TLS
Routing
Traffic
Logs
```

TLS Mode：

```text
Cloudflare SaaS
Direct TLS - Managed
Direct TLS - Manual
Disabled
```

Phase 1：

```text
Cloudflare SaaS
Disabled
```

Phase 2：

```text
Direct TLS
```

------------------------------------------------------------------------

# 二十一、API 开发任务

Phase 1：

```text
POST   /api/admin/domains
GET    /api/admin/domains
GET    /api/admin/domains/:id
PATCH  /api/admin/domains/:id
DELETE /api/admin/domains/:id

GET    /api/admin/domains/:id/status
GET    /api/admin/domains/:id/verification
POST   /api/admin/domains/:id/sync

GET    /api/admin/cloudflare/settings
PATCH  /api/admin/cloudflare/settings
```

Phase 2：

```text
GET    /api/admin/certificates
GET    /api/admin/certificates/:id
POST   /api/admin/domains/:id/certificate
DELETE /api/admin/domains/:id/certificate
POST   /api/admin/certificates/:id/reload
POST   /api/admin/certificates/:id/renew
GET    /api/admin/certificates/:id/status
```

------------------------------------------------------------------------

# 二十二、开发阶段规划

| 阶段 | 内容 | 优先级 |
|------|------|--------|
| Phase 0 | Domain / Certificate / TLS 模型 | P0 |
| Phase 1.1 | Cloudflare API Client | P0 |
| Phase 1.2 | Custom Hostname 创建与同步 | P0 |
| Phase 1.3 | Domain Verification 状态机 | P0 |
| Phase 1.4 | Cloudflare Origin TLS | P0 |
| Phase 1.5 | Trusted Proxy / Host Routing | P0 |
| Phase 1.6 | Cloudflare SaaS 前端管理 | P0 |
| Phase 2.1 | Certificate Store | P1 |
| Phase 2.2 | Memory Certificate Cache | P1 |
| Phase 2.3 | Dynamic SNI | P1 |
| Phase 2.4 | Manual Certificate | P1 |
| Phase 2.5 | Certificate Hot Reload | P1 |
| Phase 2.6 | Direct TLS Frontend | P1 |
| Phase 3 | ACME Managed Certificate | P2 |
| Phase 3 | Multi-Gateway Certificate Sync | P2 |
| Phase 3 | Automatic Renewal | P2 |

------------------------------------------------------------------------

# 二十三、建议实际开发顺序

## Step 1

完成：

```text
Tenant
Domain
Service
Ent Schema
PostgreSQL
```

## Step 2

完成：

```text
Host → Domain → Tenant → Service → Instance
```

确认 HTTP 路由闭环。

## Step 3

实现 Cloudflare API Client：

```text
Create Custom Hostname
Get Custom Hostname
Delete Custom Hostname
```

## Step 4

实现 Domain 生命周期：

```text
pending
verifying
certificate_pending
active
failed
```

## Step 5

配置 Cloudflare Origin TLS：

```text
Cloudflare
   ↓
HTTPS
   ↓
Maple Gateway
```

完成 Cloudflare SaaS MVP。

## Step 6

完成 Admin Domains 页面。

做到这里即可先上线 Cloudflare SaaS 模式。

## Step 7

开始 Direct TLS：

```text
Certificate Ent Schema
Private Key Encryption
Certificate Validation
Certificate Cache
```

## Step 8

实现：

```text
tls.Config.GetCertificate
```

完成 Dynamic SNI。

## Step 9

实现 Certificate Hot Reload：

```text
Upload New Certificate
    ↓
Memory Cache Replace
```

无需 Gateway Restart。

## Step 10

增加 Managed Certificate Provider 抽象。

后续再实现 ACME。

------------------------------------------------------------------------

# 二十四、Phase 1 最小上线范围

第一版真正需要完成：

```text
Cloudflare for SaaS
Domain Creation
Custom Hostname
Domain Verification
Certificate Status
Origin TLS
Trusted Proxy
Host Routing
Domain Enable / Disable
Domain Delete
```

第一版不实现：

```text
ACME
Let's Encrypt
Dynamic SNI
Manual Certificate
TLS Passthrough
Certificate Renewal Engine
Multi-Gateway Certificate Sync
```

------------------------------------------------------------------------

# 二十五、Phase 2 最小上线范围

Direct TLS 第一版：

```text
Manual Certificate
Certificate Validation
Encrypted Private Key
Memory Certificate Cache
Dynamic SNI
Certificate Hot Reload
SNI / Host Match
Certificate Expiry Detection
```

先不自动签发。

------------------------------------------------------------------------

# 二十六、最终目标架构

```text
                         Customer Domain
                               │
              ┌────────────────┴────────────────┐
              │                                 │
              ▼                                 ▼
      Cloudflare SaaS                    Direct TLS
              │                                 │
      Cloudflare Cert                          SNI
              │                                 │
              │                         Certificate Cache
              │                                 │
              └───────────────┬─────────────────┘
                              ▼
                       Maple Gateway
                              │
                           HTTP Host
                              │
                              ▼
                         Route Cache
                              │
                              ▼
                            Tenant
                              │
                              ▼
                            Service
                              │
                              ▼
                       Traffic Policy
                              │
                              ▼
                       Load Balancer
                              │
                              ▼
                           Instance
                              │
                              ▼
                             Node
```

------------------------------------------------------------------------

# 二十七、项目最终原则

1. Cloudflare SaaS 是 Maple Gateway 第一优先级 TLS 模式。
2. Direct TLS 是独立的第二种接入能力。
3. Cloudflare 模式由 Cloudflare 管理租户公网 Certificate。
4. Direct TLS 模式由 Maple Gateway 根据 SNI 动态选择 Certificate。
5. SNI 解决 TLS Certificate 选择，Host 解决 Tenant Routing。
6. Certificate Cache 与 Route Cache 完全分离。
7. TLS Handshake 不访问 PostgreSQL。
8. Ent 负责 Certificate / Domain 持久化。
9. Direct TLS Certificate 支持热更新。
10. Private Key 必须加密保存。
11. Cloudflare 模式使用统一 Origin TLS。
12. Cloudflare API Token 和 Private Key 永不通过普通 API 返回。
13. Domain 生命周期统一管理 Verification、Certificate、Route 状态。
14. 两种 TLS 模式最终共享同一套 Tenant / Service / Instance 路由系统。
15. 第一阶段不要实现 ACME，优先把 Cloudflare SaaS 模式做稳定。
16. 第二阶段先实现 Manual Certificate + Dynamic SNI，再扩展 Managed ACME。
17. TLS 模块不能污染 Reverse Proxy 和 Tenant Routing 核心逻辑。
18. Maple Gateway 最终可以在 Cloudflare 模式与 Direct TLS 模式之间独立选择，而不会绑定单一 CDN。
