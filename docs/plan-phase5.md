# Phase 5 开发计划 — Direct TLS 与动态 SNI 证书专项

> **对应项目**：Maple Gateway（Multi-Tenant Gateway）
> **技术栈**：Go 1.26+ / Ent（ORM）/ PostgreSQL 16 / pgx / Fiber v3 / Redis 7 / React 19 + TypeScript + Vite + Tailwind CSS
> **文档状态**：v1.0-plan（开发执行依据，随实现推进勾选与修订）
>
> **配套文档**：[Phase 1 计划](./plan-phase1.md) · [Phase 2 计划](./plan-phase2.md) · [Phase 3 计划](./plan-phase3.md) · [Phase 4 计划](./plan-phase4.md) · [Maple_Gateway_开发需求任务.md](./Maple_Gateway_开发需求任务.md) · [项目功能全面分析.md](./项目功能全面分析.md) · [项目目录结构.md](./项目目录结构.md) · [API接口全面分析.md](./API接口全面分析.md)

---

## 一、Phase 5 目标

> **Gateway 从"单张静态证书 + Host 路由"走向"每租户独立证书 + SNI 动态选择的 Direct TLS 模式"——租户域名可直接解析到 Maple Gateway，网关依据 TLS ClientHello 的 SNI 选中对应证书完成 TLS 终结，再按 HTTP Host 走既有租户路由链路**

Phase 1 交付多租户转发闭环；Phase 2 交付发布控制与可视化管理；Phase 3 把控制面数据访问切到 Ent ORM；Phase 4 交付多实例 HA / 自动 Canary / 高级路由 / 可观测与 RBAC。此前的 HTTPS 能力停留在**网关自身配置一张全局静态证书**：`gateway.https.cert/key`，证书归属运营方而非租户，且无 SNI 动态选择。Phase 5 把 HTTPS 接入升级为 **Direct TLS 模式**，补齐需求文档第二章"Direct TLS 模式"与第六~八章 Certificate / SNI 相关能力：

```text
Phase 1/2/3/4（已交付）
Domain → Tenant → Service → Deployment → Version → Instance → Node
数据面 = HTTP(S) + Memory Route Cache（请求不查库）│ HTTPS = 单张全局静态证书
多实例 HA / 自动 Canary / 高级路由 / OTel + Request ID / RBAC

Phase 5（本计划）
证书成为一等公民：Certificate Ent Schema + 私钥加密存储
   ↓
Certificate Service：校验 / 存储 / 内存缓存 / 热加载
   ↓
tls.Config.GetCertificate 动态 SNI 选择 + 缓存未中不落库
   ↓
SNI / Host 一致性校验（421 Misdirected）│ 证书隔离（单租户失败不影响他人）
   ↓
Direct TLS 管理面 API + Domains 前端 TLS 区块 │ 安全与可观测收口
```

### 核心原则

| 原则 | 说明 |
|------|------|
| **TLS Handshake 不访问 PostgreSQL** | 证书查找只走内存缓存；缓存未中直接握手失败，绝不在握手路径查询 Ent/DB |
| **Route Cache 与 Certificate Cache 完全分离** | SNI 选证书走证书缓存，Host 路由走既有路由缓存，两套缓存两类失效事件 |
| **SNI 管证书、Host 管路由** | SNI 解决 TLS 证书选择，Host 解决租户路由；两者不一致返回 421 而非 404/400 |
| **私钥加密存储、永不回显** | Private Key 落库前加密；Admin API / 日志永不返回 private_key 明文或密文 |
| **单租户证书故障隔离** | 某域名证书解析失败仅影响该域名握手，不拖垮整条监听链路与其他租户 |
| **数据平面核心不污染** | TLS/证书逻辑收敛到独立 `tls` 包，Reverse Proxy / Tenant Routing 核心逻辑保持不动 |
| **Cloudflare 模式不受影响** | Direct TLS 是独立接入能力；单全局证书回退路径保留，Cloudflare SaaS（远期）仍可叠加 |

> 范围依据：[Maple_Gateway_开发需求任务.md](./Maple_Gateway_开发需求任务.md) 二/三/六/七/八/十/十三/十五/十六/十八/二十/二十一/二十五 各章。本期只做 **Direct TLS 模式**；Cloudflare SaaS / Origin TLS / ACME 不在本期范围。

---

## 二、Phase 5 范围边界

### 2.1 本阶段要做

| 能力 | 说明 |
|------|------|
| Certificate 数据模型 | Ent Schema：certificates 表，含 encrypted private key / source / status / 有效期等 |
| Certificate Service | 上传校验（PEM 解析、证书与私钥匹配、SAN 覆盖、未过期）、加密存储、CRUD |
| Certificate Memory Cache | 内存缓存 hostname → tls.Certificate；缓存未中不查库，直接握手失败 |
| Dynamic SNI | `tls.Config.GetCertificate` 按 `ClientHello.ServerName` 动态选证书 |
| 证书热加载 | 上传/替换证书 → 更新缓存 → 新连接即刻用新证书，无需重启、在途连接不受影响 |
| SNI / Host 一致性 | Direct TLS 下 SNI 与 Host 不一致默认返回 421 Misdirected Request |
| Domain 扩展 | domains 增 tls_mode / certificate 关联等字段，状态机补充 certificate 维度 |
| Direct TLS 管理面 API | 证书 CRUD / 上传 / 重载 / 状态查询；private key 永不返回 |
| Domains 前端 TLS 区块 | Domain Detail 展示 TLS Mode / Certificate 状态 / 有效期；上传与替换交互 |
| 安全与可观测 | 私钥加密、SNI/Hostname 校验、TLS 日志分类、证书指标、缓存未中日志 |

### 2.2 本阶段不做（延后 / 远期）

| 能力 | 归属 | 说明 |
|------|------|------|
| Cloudflare for SaaS / Origin TLS | 远期 | 与 Direct TLS 互不依赖；需求文件标注为默认生产模式，另立计划推进 |
| Managed / ACME / 自动签发 | Phase 6 | 本期先做 Manual Certificate + 动态 SNI，Provider 抽象预留接口不落地 ACME |
| 证书自动续期引擎 | Phase 6 | 本期检测过期/临期并告警，不自动签发续期 |
| TLS Passthrough | 远期 | 需求文件已明确暂不实现 |
| Origin IP 访问限制 / 防绕过 | Cloudflare 模式范畴 | Direct TLS 无 CDN 回源场景，本期不做 |

> 范围依据：需求文档"二十四、Phase 2 最小上线范围"明确 Direct TLS 第一版 = Manual Certificate / Certificate Validation / Encrypted Private Key / Memory Certificate Cache / Dynamic SNI / Hot Reload / SNI-Host Match / Expiry Detection，先不自动签发。

---

## 三、起点（Phase 1/2/3/4 已交付 + 现状核实）

写本计划前对仓库实际代码核实，Phase 5 在此之上增量开发：

| 项 | 现状 |
|----|------|
| 数据面 HTTPS | ✅ HTTP/HTTPS 并行监听，**但 HTTPS 用单张全局静态证书**：`listener{server, tls:true, cert, key}` 在 `NewDataPlane` 里把 `cfg.CertFile/KeyFile` 直接塞进 listener，`Start()` 调用 `ListenAndServeTLS(cert,key)`（`internal/gateway/listener.go`）。无 `tls.Config`、无 `GetCertificate`、无 SNI 分支 |
| 启用条件 | ✅ `main.go:223` 三重 gate：`HTTPS.Enabled && Cert != "" && Key != ""`；HTTP 默认 `Enabled:true`、HTTPS 默认 `Enabled:false`（不对称），config `Validate()` 在 HTTPS 开启时强校验 cert/key 非空 |
| 配置 | ✅ `config.go` 的 `HTTPListener{Address, Enabled, Cert, Key}`，`MAPLE_GATEWAY_HTTPS_ADDR/ENABLED/CERT/KEY` env 覆盖。证书路径为单一全局字段，**无 per-domain 证书配置** |
| Domain 数据模型 | ✅ `domains` 表：id / tenant_id / hostname(UNIQUE) / service_id / status(active\|disabled\|pending) / verified_at——**无 tls_mode、无 certificate 关联、无多级验证状态机**（`server/ent/schema/domain.go`） |
| Domain 管理 API | ✅ CRUD + Enable/Disable（`internal/domain/handler.go`），**缺** `/:id/status`、`/:id/verification`、`/:id/sync` 子资源接口 |
| 路由解析 | ✅ Host → `NormalizeHost` → Route Cache（DB-backed）或 StaticResolver；请求不查库 |
| 缓存基础设施 | ✅ `internal/cache` 路由缓存 + 版本号 + AutoRebuild；`internal/redis` 可用于证书变更广播/失效（Phase 4 已建 Redis 依赖） |
| HA | ✅ 多实例 Leader / 心跳 / `gateway_instances` 表（Phase 4）——证书缓存的热加载天然需要多实例一致 |
| 日志 / 指标 | ✅ `internal/logs`（环形缓冲 + DB audit）、`internal/metrics` 自研 registry + dashboard；可加 TLS/证书分类 |
| 前端 | ✅ React 19 管理台，`pages/admin/DomainsPage.tsx` 已存在，可扩展 TLS 区块 |
| 安全 | ✅ RBAC 三角色、`admin_token`、trusted_proxies 骨架（`internal/security`）——私钥加密需复用/扩展 |
| 迁移 | ✅ `0001_init.sql` – `0013_admins_role.sql` 已冻结为历史（Ent 权威） |

> 版本发布：当前分支 `main-remote`，`pkg.Version` = `0.1.20`；Phase 5 完成以 `v0.2.0` 为发布版本（新增面向租户的证书体系，属 minor 语义递增）。

---

## 四、开发阶段划分

对照需求文档"二十三、建议实际开发顺序"的 Step 7-10，按依赖编排为六个可验证阶段：

```text
S1  Certificate 数据基座：certificates 表 + 私钥加密 + Domain 扩展 + 迁移
S2  Certificate Service：校验 / 存储 / 缓存 / CRUD 管理面 API
S3  Dynamic SNI：tls.Config.GetCertificate + 双监听接线 + 缓存未中处理
S4  Direct TLS 域名流程：状态机 + SNI/Host 一致 + 证书热加载 + 隔离
S5  安全与可观测：TLS/证书日志 + 指标 + 审计 + 失败隔离回归
S6  前端 + 端到端验证与发布
```

| 阶段 | 内容 | 涉及模块 | 预期工时 |
|------|------|----------|---------|
| **S1 证书数据基座** | certificates ent schema + 私钥加密 + domain 扩展 + 迁移 + generated code | ent + pkg(crypto) + migrations | 2-3 天 |
| **S2 Certificate Service** | 校验（PEM/匹配/SAN/过期）+ 存储 + 缓存 + 管理面 CRUD/上传/重载 | certificate + api + domain | 3-4 天 |
| **S3 Dynamic SNI** | `tls.Config.GetCertificate` + DataPlane 接线 + 握手期缓存查找 + 缓存未中处理 | gateway + certificate + proxy | 3-4 天 |
| **S4 Direct TLS 域名流程** | 状态机扩展 + SNI/Host 一致(421) + 证书热加载 + 多实例一致 | domain + certificate + ha + gateway | 3-4 天 |
| **S5 安全与可观测** | TLS/证书日志 + 指标 + 审计 + 故障隔离回归 | logs + metrics + audit + certificate | 2-3 天 |
| **S6 前端与收尾验证** | Domains TLS 区块 + 端到端/回归/性能复测 + 文档 | frontend + 全项目 + docs | 3-4 天 |

> **合计约 16-22 天（单人开发）**。S1 是数据基座，S2/S3 可部分并行（S3 的证书查找依赖 S1 缓存，S2 的 API 可先于 S3 落地），S4 依赖 S3，S5/S6 收口。

---

## 五、S1 — Certificate 数据基座

### 5.1 新增 Certificate Ent Schema

对齐需求文档"任务 0.3"的字段清单：

```text
server/ent/schema/certificate.go

Certificate
├── id                uuid
├── domain_id         uuid（可选；绑定域名后证书缓存键即域名 hostname）
├── hostname          string（冗余存一份，缓存/指标/检索免 join；hostname UNIQUE 索引候选）
├── source            cloudflare | acme | manual | origin（本期只落 manual；余为枚举预留）
├── status            active | pending | error | expiring | expired（与 Domain 证书状态分离）
├── certificate_pem   text
├── private_key_encrypted  text（AES-GCM；密钥来自配置/环境，绝不落明文）
├── issuer            string
├── serial_number     string
├── issued_at         time
├── expires_at        time（索引：临期扫描）
├── last_renewed_at   time（可空）
├── last_error        string（可空）
├── created_at / updated_at
```

### 5.2 Domain 扩展（对齐需求文档"任务 0.1"）

`domains` 表追加字段（全部可空 / 带默认，存量行无感）：

```text
tls_mode              cloudflare | managed | manual | disabled  默认 disabled
certificate_id        uuid（可空，指向 certificates）
cloudflare_hostname_id string（预留）
verification_status    string（预留；本期 Direct TLS 手动验证走 certificate 状态）
certificate_status     string（active/pending/error/expiring/expired）
```

> `tls_mode` 全枚举先落库（需求文档预留 `passthrough` 但不实现），本期可写值只开放 `manual` / `disabled`；`cloudflare` / `managed` 由后续 Phase 消费。

### 5.3 私钥加密

新增 `server/internal/certificate/encryption.go`（或并入 `pkg`）：

```text
AES-256-GCM
  数据密钥：从 MAPLE_CERT_ENC_KEY（base64 32 字节）读取
  无配置 → 启动失败（私钥加密不可降级为明文）
  每个私钥随机 nonce；密文 = nonce + ciphertext（base64 入库）
```

### 5.4 状态机（对齐需求文档"任务 0.2"）

```text
Domain（Routing 维度，沿用现有 active/disabled/pending）
   │
   └─ tls_mode=manual 时叠加 Certificate 维度：
        pending → certificate_pending → active
        失败：verification_failed / certificate_failed / disabled
```

> 两条原则：**Domain 路由状态与 Certificate 证书状态分离**（需求原则 13 反向解读：证书失败不自动把域名从"已路由"降级，除非显式配置）；**Certificate Pending 时不可进入 Active Route**（需求 1.10 验收、Direct TLS 同理）。

### 5.5 迁移与生成

```text
0014_certificates.sql            # S1：certificates 表 + 索引（hostname/expires_at/domain_id）
0015_domain_tls_fields.sql       # S1：domains.tls_mode / certificate_id / cloudflare_hostname_id / verification_status / certificate_status
```

新增字段进 `ent/schema/` 后 `go generate ./ent` 重新生成客户端；SQL 迁移作为历史/幂等基线（对齐 0013 以前结构）。数据库任务清单对应需求"十一"：新增 `certificates`，更新 `domains`。

### 5.6 验证

- [x] certificates 表可落 manual 证书（PEM + 密文私钥），密文字段不出现明文私钥
- [ ] domains 新增字段迁移后存量行无感（默认 disabled / 空证书）
- [x] 加密私钥可解密还原并用于构建 `tls.Certificate`（往返测试）
- [x] 缺 `MAPLE_CERT_ENC_KEY` 时启动明确报错，不允许明文私钥落库

---

## 六、S2 — Certificate Service 与存储

### 6.1 Service 职责

对齐需求文档"任务 2.1"模块划分，本期收敛为：

```text
server/internal/certificate/
├── model.go        # Certificate 领域模型 + New/Update 输入（private_key 仅存在于输入，不进输出）
├── repository.go   # ent 读写（list/get/create/update/delete、by-hostname、临期扫描）
├── service.go      # 业务编排：上传→校验→加密→落库→缓存
├── store.go        # 加密/解密私钥、tls.Certificate 组装
├── cache.go        # hostname → *tls.Certificate 内存缓存
├── loader.go       # 启动/变更时从 DB 装载到缓存（离线装载，非握手路径）
├── validation.go   # PEM 解析、证书/私钥匹配、SAN 覆盖、过期检查
├── sni.go          # GetCertificate 回调（S3）
├── encryption.go   # AES-GCM（S1）
└── errors.go       # 分类错误（证书未找到/不匹配/过期/损坏）
```

### 6.2 Upload Certificate（任务 2.4）

```text
POST /api/admin/domains/:id/certificate
  body: { certificate_pem, private_key_pem, chain_pem? }
  校验：PEM 格式 / 证书与私钥匹配 / SAN 含当前域名 hostname / 未过期 / chain 可解析
  → 私钥 AES-GCM 加密
  → 落 certificates 表（source=manual, status=active/pending 视过期与校验）
  → 更新 domains.certificate_id / tls_mode=manual / certificate_status
  → 更新内存 Certificate Cache
  → 返回不含私钥的证书详情
```

### 6.3 校验清单（安全任务"十三"对应项）

- Certificate PEM Validation
- Certificate / Key 匹配（`tls.X509KeyPair` + 校验 `crypto.PublicKey` 相等）
- Certificate Expiry Validation（notBefore ≤ now ≤ notAfter）
- SAN 覆盖当前域名（精确 + 父域 wildcard 均放行）
- Chain 可解析（intermediates 排序拼接）

### 6.4 Memory Cache 与"缓存未中不落库"

```text
PostgreSQL →（离线）Loader → Memory Cache → tls.Handshake 只读 Cache
```

- 缓存键：normalize 后的 hostname（复用 `router.NormalizeHost` 语义）
- 启动：全量装载；DB 变更：显式刷新（进程内事件 + Redis pub/sub 多实例，见 S4）
- **握手路径严禁 DB**：Cache Miss → 记录 TLS/安全日志 + 增加 `maple_tls_certificate_cache_misses_total` → 握手失败

### 6.5 管理面 API（任务 2.9 本期子集）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/certificates` | 列表（可按 domain/hostname 过滤） |
| GET | `/api/admin/certificates/:id` | 详情（不含 private_key 任何形态） |
| POST | `/api/admin/domains/:id/certificate` | 上传/替换 Manual Certificate |
| DELETE | `/api/admin/domains/:id/certificate` | 删除证书并清缓存 |
| POST | `/api/admin/certificates/:id/reload` | 强制重载（DB → 缓存） |
| POST | `/api/admin/certificates/:id/status` 的 GET | 证书状态 / 有效期 |

> `private_key` / `private_key_encrypted` 永不进入 API 输出结构体（`omitempty` 之外更彻底：输出模型不含该字段）。

### 6.6 验证

- [x] 上传合法 PEM（证书+匹配私钥）→ 落库密文、缓存命中、详情接口无私钥字段
- [x] 拒绝：私钥与证书不匹配 / SAN 不覆盖域名 / 已过期 / PEM 损坏（各自返回明确错误）
- [ ] 缓存未中不产生 DB 查询（单测用 mock/拦截计数断言）
- [ ] 删除证书 → 缓存同步移除，该域名握手走向未配置分支（S4 隔离语义）

---

## 七、S3 — Dynamic SNI

### 7.1 数据面接线改造

现状 `NewDataPlane` 直传 `CertFile/KeyFile`、`Start()` 走 `ListenAndServeTLS`。改为**单全局回退证书 + 动态 SNI**：

```text
main.go / gateway
  HTTPS 启用条件：保留原三重 gate（.enabled + cert + key）作为"回退证书"，兼容现有启动方式
  TLS 模式开关：tls_mode 配置（见下）决定：
     global   —— 原逻辑：ListenAndServeTLS(全局 cert/key)，行为不变（回归基线）
     direct   —— 构造 *tls.Config：
                    MinVersion = tls.VersionTLS12
                    GetCertificate = certificate.SNIGetter(...)   // 本期核心
                    Certificates = [回退证书]（SNI 未命中 / 无 SNI 时的兜底，可配置）
  用 tls.NewListener 或 http.Server.TLSConfig + ServeTLS 接好 tls.Config
```

关键点：**`tls.Config.GetCertificate` 优先于 `Certificates`**——ServerName 有值且缓存命中走动态证书；未命中/空 SNI 走回退证书（决定是否放行，见 S4 SNI/Host 一致）。

### 7.2 SNI Getter 逻辑（任务 2.3）

```text
ClientHello.ServerName
   → Normalize（小写 / 去端口 / IDN；复用 host 规范化）
   → Certificate Cache Lookup（hostname → tls.Certificate）
       命中 → 返回证书
       未中 → （不查 DB）返回 error → 握手失败；记日志 + 指标
```

### 7.3 缓存未中处理与隔离（原则 5 / 需求 2.3、18）

- Cache Miss 返回错误只影响**该 SNI 的握手**，`GetCertificate` 并发安全（读多写少，原子替换）。
- 回退证书仅在 `tls.mode=direct` 且开启 `fallback_cert` 时才兜底，避免裸 IP / 健康检查 TLS 请求失败。
- 隔离验证：给 100 个 hostname 装载证书、其中 1 个故意损坏 → 只该 hostname 握手失败，其余 99 个正常。

### 7.4 验证

- [x] 同一 443（HTTPS）监听下，`openssl s_client -servername shop-a.test` 返回 shop-a 证书、`shop-b.test` 返回 shop-b 证书
- [x] 未知 SNI：命中回退证书（若配）或握手失败，二者都不影响已知 hostname
- [x] 空 SNI（裸 IP / HTTP→HTTPS）走回退证书或拒绝，策略可配
- [ ] `tls_mode=global` 时行为与 Phase 4 完全一致（回归基线，回归测试通过）

---

## 八、S4 — Direct TLS 域名流程与一致性

### 8.1 Domain 生命周期（任务 2.8 Manual 分支）

```text
Create Domain（tls_mode=manual）→ Domain pending
   ↓
Upload Certificate → Validate → Save → Update Cache
   ↓
Domain active（此时才进入 Route Cache 可路由）
```

- **证书未就绪的 manual 域名不得进入 active 路由**（对应验收"Certificate Pending 时不可进入 Active Route"）。
- Domain 删除（沿用现有 `DELETE /api/admin/domains/:id`）要联动：停路由 → 清 Route Cache → 删证书/置 disabled → 清 Certificate Cache。

### 8.2 SNI / Host 一致性（任务 2.7）

Direct TLS 下握手成功后，HTTP Host 与 SNI 可能不一致：

```text
SNI = shop-a.com   Host = shop-a.com   → 正常转发
SNI = shop-a.com   Host = shop-b.com   → 421 Misdirected Request（默认）
```

配置项：

```yaml
tls:
  enforce_sni_host_match: true   # 默认开启；关闭则退化为纯 Host 路由（回退到 Cloudflare 语义）
```

> 实现位置：请求进入 proxy 前记录 SNI（`http.Request.TLS.ServerName`），若 `tls_mode=direct` 且启用校验且 Host≠SNI → 返回 421。这与 proxy 现有 `handleResolveError` 的 403/404/503/429 体系并列新增 421 分支，不污染核心路由逻辑。

### 8.3 证书热加载（任务 2.6）

```text
上传/替换证书 → 落库 → 刷新内存缓存（原子替换）→ 新 TLS 连接即用新证书
在途连接不受影响（tls.Conn 已按旧证书握手完毕）
```

单进程：进程内缓存直接替换；**多实例**（Phase 4 HA 已具备）通过 Redis pub/sub 广播 `CertificateChanged` / `DomainChanged`，各实例刷新本机缓存（对应需求"十二、Redis 任务"的 Phase 3 HA 通道）。

### 8.4 临期检测与告警

- 周期扫描（复用 health/AutoRebuild 节奏）`certificates.expires_at < now + 30d` → 状态置 `expiring`、写日志/告警（不自动续期，续期归 Phase 6）。
- `expired` 状态证书从缓存摘除，避免发过期证书。

### 8.5 验证

- [ ] manual 域名在证书就绪前不可路由；就绪后 Host 路由正常
- [x] SNI=shop-a、Host=shop-b → 421；关掉 `enforce_sni_host_match` 后同场景按 Host 正常路由
- [x] 上传替换证书 → 新握手立即拿到新证书，旧 keep-alive 连接不受影响
- [ ] 双实例同连一 DB+Redis：A 上传证书，B 在广播窗口内无需重启即可用新证书握手
- [ ] 删除域名后：Route Cache 与 Certificate Cache 同时摘除，请求/握手不再命中

---

## 九、S5 — 安全与可观测

### 9.1 安全收口（需求"十三、安全任务"Direct TLS 相关项）

| 项 | 落地 |
|----|------|
| Private Key 加密存储 | S1 AES-GCM；加密密钥仅来自配置/环境 |
| Admin API 不返回 Private Key | 输出模型不含 private_key 字段 |
| TLS 最低版本 | 回退证书与动态证书统一 `MinVersion: tls.VersionTLS12`（对齐 Go 安全默认） |
| Hostname / SNI Validation | 上传时 SAN 校验 + 握手时 SNI normalize；非法 hostname 拒绝 |
| SNI / Host Match | 8.2 的 421 校验 |
| Certificate / Key Match | S2 校验 |
| Certificate Expiry Validation | S2 校验 + S4 临期扫描 |
| Audit Log | 证书上传/替换/删除/重载/域名删除写 audit_logs（复用 Phase 4 审计） |

### 9.2 日志分类（需求"十六、日志任务"）

- TLS Log：握手成功/失败、SNI、协议/版本、缓存命中/未中。
- Certificate Log：上传/替换/删除/重载/临期/过期，携带 domain_id / hostname / certificate_id / status。
- 日志字段带 `request_id`（沿用 Phase 4）。
- **严禁**记录 private key / 私钥密文。

### 9.3 指标（需求"十五、Metrics 任务"）

```text
maple_tls_handshakes_total
maple_tls_handshake_errors_total
maple_tls_certificate_cache_hits_total
maple_tls_certificate_cache_misses_total
maple_tls_certificate_expiring            # 按临期计数，避免 hostname 高基数标签
maple_domain_verification_errors_total
```

> 指标按域名状态/错误分类聚合，不把 hostname 作为高基数 label（对齐需求提示）。

### 9.4 验证

- [x] 握手成功/失败与缓存命中/未中计数随请求变化正确
- [ ] 日志含 SNI/状态/错误码，全量检索不含 private_key 相关字段
- [x] 证书上传/替换/删除均落 audit；越权调用被 RBAC 拦截并写 DENY
- [ ] 损坏证书实例故障隔离：只该 hostname 握手失败，指标/日志能定位到 domain_id

---

## 十、S6 — 前端与收尾验证

### 10.1 Domains 前端扩展（任务 2.10 / 需求"二十"）

`DomainsPage.tsx` 现有 CRUD 之上：

- 列表列：TLS Mode / Certificate Status（+ Route Status 已有列区分）。
- Domain Detail / 侧滑面板新增 **TLS 区块**：TLS Mode、Verification、Certificate Source、Certificate Status、Issuer、Issued At、Expires At、Last Renewed、Last Error。
- Manual 上传交互：PEM 粘贴（certificate + private key + chain），前端不上传私钥以外的敏感字段；上传后状态刷新。
- RBAC 联动：viewer 只读证书详情、operator/super_admin 可上传/替换/删除（沿用 Phase 4 RBAC 授权）。

### 10.2 端到端回归 / 测试计划（需求"十八、Direct TLS 测试计划"对齐）

- [x] 单域名证书：`openssl s_client -servername` 校验证书正确
- [x] 多域名证书（覆盖 3+ hostname）各自命中
- [ ] 100 Domain SNI / 1000 Domain SNI：缓存查找性能与内存（批量装载脚本）
- [x] Unknown SNI → 回退/失败，不影响已知域名
- [x] Expired / Wrong / Key mismatch 上传被拒
- [x] SNI / Host mismatch → 421
- [ ] Certificate Hot Reload / Replace：新握手用新证书，在途不受影响
- [ ] Gateway Restart：缓存从 DB 重建
- [ ] Concurrent TLS Handshake：并发冒烟无 panic / 无数据竞争
- [ ] SSE / WebSocket over HTTPS：沿用 Phase 4 回归（stream/SSE 冒烟通过即视为覆盖）
- [ ] HTTP/2：`h2` 协商可用（`NextProtos` 配置核对）
- [ ] Cache Miss / Domain Disable / Tenant Disable / Certificate Expiry Warning 行为正确

### 10.3 质量要求

1. TLS Handshake 热路径零 DB 查询（S2/S3 单测断言）。
2. 证书故障单租户隔离，不拖垮监听与其余租户（S5 回归）。
3. `tls_mode=global` 与 Phase 4 行为一致（回归基线不破）。
4. `go vet ./...` 通过；`go test ./...` 全绿；`-race` 建议 Linux/CI 复核（对齐 Phase 4 遗留）。
5. 证书/审计/TLS 日志闭环，可追溯；私钥零回显。
6. 文档联动更新：本计划验收勾选；`API接口全面分析.md` / `项目功能全面分析.md` / `项目目录结构.md` 增补 Direct TLS 接口与目录。

### 10.4 遗留复核项（建议 CI / Linux 补）

- `go test -race ./...` 全量（Windows 本地未覆盖；并发握手/缓存替换建议 CI 复核）。
- HTTP/2 与 WebSocket 压测在 Linux 端复核。
- 性能基线：1000 证书内存占用与握手 RPS 需压测建立基线（指标 7.4/19 章）。

---

## 十一、关键设计决策

### 11.1 全局回退证书保留，作为 global 模式与 direct 模式兜底

`tls_mode=global` 完整保留 Phase 4 单静态证书行为（`ListenAndServeTLS` 语义不变），作为回归基线与无 per-tenant 证书场景的兜底；`tls_mode=direct` 在其上叠加 `GetCertificate`。这样既满足"Direct TLS 不影响 Cloudflare 模式/现有部署"，又给运维留了裸 IP/健康检查的回退出口。

### 11.2 证书缓存与路由缓存分离、双写双失效

两者语义不同（SNI 选证书 vs Host 选路由），缓存键同源（hostname）但生命周期与失效事件独立。删除域名/停租户时按 Domain ID 联动清两边缓存。

### 11.3 私钥加密不可降级

无 `MAPLE_CERT_ENC_KEY` 时拒绝启动 Direct TLS 证书存储，宁可失败不落明文——避免"功能可用但安全退化"的默认陷阱。

### 11.4 证书状态与 Domain 路由状态分离

证书失败把域名标记为 `certificate_failed`，是否继续按已缓存旧证书服务/摘除路由交给策略（本期默认：证书失败即摘除该域名 active 路由，防发错证书），但 Domain 记录本身保留，恢复后重入 active——与需求"Domain 状态与 Certificate 状态分离"一致。

### 11.5 热加载与多实例一致复用 Phase 4 HA 通道

证书变更经 Redis pub/sub 广播 `CertificateChanged`，各实例只刷本机缓存；无 Redis 时单机事件 + 周期对账兜底（对齐 Phase 4 "可降级"原则）。

---

## 十二、技术依赖与配置清单

### 新增依赖

```text
无第三方新增               # 加密用标准库 crypto/aes + crypto/cipher + crypto/x509 + crypto/tls
（可选）golang.org/x/crypto/acme   # 仅 Phase 6 ACME 引入，本期不落
```

### 环境变量 / 配置新增

```yaml
tls:
  mode: global                     # global（Phase 4 单证书兼容）| direct（动态 SNI）
  min_version: tls1.2
  enforce_sni_host_match: true     # Direct TLS：SNI 与 Host 不一致返回 421
  fallback_cert_enabled: true      # direct 模式下未知 SNI/裸 IP 是否用全局证书兜底
```

```env
MAPLE_CERT_ENC_KEY=...             # 私钥加密数据密钥（base64 32B）；缺失则证书存储拒绝启动
MAPLE_TLS_MODE=global|direct
MAPLE_TLS_ENFORCE_SNI_HOST_MATCH=true
```

### 迁移新增

```text
0014_certificates.sql              # S1
0015_domain_tls_fields.sql         # S1
```

### Ent 新增/修改

```text
ent/schema/certificate.go          # S1 新增
ent/schema/domain.go               # S1 追加 tls_mode/certificate 关联等字段
```

---

## 十三、验收 / 交付标准

### 功能交付

1. [ ] Manual Certificate 上传/校验/加密存储闭环，Admin API 与日志零私钥回显。——S1/S2
2. [ ] `tls.Config.GetCertificate` 动态 SNI 生效：同一 HTTPS 监听按 SNI 返回各域名证书；缓存未中不查库直接失败。——S3
3. [ ] Direct TLS 域名在证书就绪前不可路由；SNI/Host 不一致返回 421，可配置关闭。——S4
4. [ ] 证书热加载：上传替换后新握手即新证书，在途连接不受影响；多实例经广播收敛。——S4
5. [ ] 单租户证书故障隔离：损坏/临期/过期只影响该域名并触发指标与日志，其余租户零影响。——S5
6. [ ] `tls_mode=global` 行为与 Phase 4 完全一致（回归基线通过）。——S3/S6
7. [ ] Domains 前端展示 TLS Mode / Certificate 状态并提供上传替换交互，RBAC 联动。——S6

### 质量要求

1. TLS 握手热路径零 DB 查询；缓存查找并发安全。
2. 私钥加密不可降级；私钥明文/密文零 API 输出、零日志。
3. `go vet ./...` 通过；`go test ./...` 全绿；`-race` 建议 CI/Linux 复核。
4. 端到端覆盖 Direct TLS 测试计划主体项（S6 清单逐项勾选）。
5. 文档联动更新：本计划勾选 + 三份配套分析文档同步。

### 非交付（Phase 6 / 远期）

- Managed / ACME 自动签发与自动续期引擎（Provider 抽象已留位）。
- Cloudflare for SaaS 全自动化与 Origin TLS、Direct Origin 防绕过。
- TLS Passthrough、多区域证书策略。

---

## 十四、Phase 6 衔接（预告）

Phase 5 落地 Manual Certificate + Dynamic SNI 后，Gateway 具备面向租户的 Direct TLS 终结能力。Phase 6 在其上叠加：

```text
Managed Certificate Provider 抽象（Issue/Renew/Revoke/Status）
   ↓
ACME / Let's Encrypt 自动签发与续期引擎
   ↓
证书生命周期自动化（临期自动续期 / 失败重试 / 与 Cloudflare 模式统一 TLS 管理层）
```

Phase 5 预留的 `CertificateProvider` 接口、`certificates.source` 枚举与临期扫描循环即为 Phase 6 ACME 挂载点；`certificates` 与 `domains.tls_mode` 字段同时被远期 Cloudflare SaaS（`source=cloudflare`、`cloudflare_hostname_id`）复用。
