# Phase 6 开发计划 — Managed Certificate 与 ACME 自动签发续期专项

> **对应项目**：Maple Gateway（Multi-Tenant Gateway）
> **技术栈**：Go 1.26+ / Ent（ORM）/ PostgreSQL 16 / pgx / Fiber v3 / Redis 7 / React 19 + TypeScript + Vite + Tailwind CSS
> **文档状态**：v1.0-plan（开发执行依据，随实现推进勾选与修订）
>
> **配套文档**：[Phase 1 计划](./plan-phase1.md) · [Phase 2 计划](./plan-phase2.md) · [Phase 3 计划](./plan-phase3.md) · [Phase 4 计划](./plan-phase4.md) · [Phase 5 计划](./plan-phase5.md) · [Maple_Gateway_开发需求任务.md](./Maple_Gateway_开发需求任务.md) · [项目功能全面分析.md](./项目功能全面分析.md) · [项目目录结构.md](./项目目录结构.md) · [API接口全面分析.md](./API接口全面分析.md)

---

## 一、Phase 6 目标

> **证书从"手动上传"走向"自动签发、自动续期"：引入 `CertificateProvider` 抽象统一证书获取方式，落地 ACME（Let's Encrypt / 兼容 CA）自动签发与续期引擎，让 Direct TLS 域名在证书到期前自动换证、失败自动重试，全程无需人工替换私钥**

Phase 1 交付多租户转发闭环；Phase 2 交付发布控制与可视化管理；Phase 3 把控制面数据访问切到 Ent ORM；Phase 4 交付多实例 HA / 自动 Canary / 高级路由 / 可观测与 RBAC；Phase 5 交付 **Direct TLS 模式**——证书表、私钥 AES-GCM 加密、内存证书缓存、动态 SNI（`tls.Config.GetCertificate`）、SNI/Host 一致（421）、临期状态扫描（仅告警、不续期）。Phase 6 把 Phase 5 反复预告、依赖证书基座却尚未落地的能力逐项收敛：

```text
Phase 1/2/3/4/5（已交付）
Domain → Tenant → Service → Deployment → Version → Instance → Node
数据面 = HTTP(S) + Memory Route Cache（请求不查库）│ HTTPS = 全局证书 / 每域名 Manual 证书（动态 SNI）
证书来源 = 手工上传（source=manual）│ 临期仅标记 expiring/expired，不自动续期

Phase 6（本计划）
CertificateProvider 抽象（Issue / Renew / Revoke / Status）
   ↓
ACME 客户端 + 账户 + Challenge（http-01 优先）→ 自动签发
   ↓
自动续期调度器：临期触发 Renew → 校验 → 加密落库 → 原子刷新证书缓存
   ↓
失败重试与退避 + 生命周期自动化（签发/续期/撤销动作入审计）+ 前端 Managed 交互
```

### 核心原则

| 原则 | 说明 |
|------|------|
| **不接 Cloudflare（本期硬约束）** | 本期只做 Manual + ACME 两种来源；Cloudflare Provider / Cloudflare for SaaS / Origin TLS 一律不在范围（见 2.2） |
| **TLS Handshake 不访问 PostgreSQL** | 延续 Phase 5：续期是**离线流程**，只在缓存写入侧生效；`GetCertificate` 热路径绝不查库、绝不触发 ACME |
| **私钥加密不可降级** | ACME 签发出的私钥与手动证书同走 `certenc` AES-GCM，仍要求 `MAPLE_CERT_ENC_KEY`，无配置拒绝启动 |
| **Provider 抽象对外契约不变** | `Service.Upload/Update/Delete/Reload` 语义保留；Provider 只是"证书来源"的横向扩展，service/handler 调用面无感 |
| **单租户故障隔离** | 某域名 ACME 签发/续期失败只影响该域名，不拖垮监听链路与其他租户 |
| **自动化可降级** | 关掉 ACME 自动引擎时系统回到 Phase 5 行为（手动上传 + 临期告警），主链路不受影响 |
| **动作全审计** | 签发 / 续期 / 撤销 / 失败重试均写 `audit_logs`，与 Phase 4 审计体系一致 |
| **可观测贯穿** | 签发/续期成功失败、账户异常、Challenge 失败均出指标与分类日志，沿用 request_id 关联 |

---

## 二、Phase 6 范围边界

### 2.1 本阶段要做

| 能力 | 说明 |
|------|------|
| CertificateProvider 抽象 | 统一 `Issue / Renew / Revoke / Status` 接口；本期实现 `ManualProvider` + `ACMEProvider` |
| ACME 客户端 | 对接 ACME 目录（Let's Encrypt 生产/staging 或兼容 CA），账户注册与密钥管理 |
| Challenge 处理 | http-01 优先（网关在数据平面代答 `/.well-known/acme-challenge/`），TLS-ALPN-01 备选 |
| 自动签发 | 域名启用 Managed 后，无证书或证书不可用时自动向 CA 申请并落库、入缓存 |
| 自动续期引擎 | 临期（复用 Phase 5 扫描窗口）触发 Renew，成功则原子替换缓存并在途连接不受影响 |
| 失败重试与退避 | 有界重试 + 指数退避 + 状态落 `last_error`；避免触发 CA 速率限制 |
| 多实例协调 | 续期/签发动作在实例间不重复执行（复用 Phase 4 Leader / Redis 锁） |
| 生命周期自动化 | 签发/续期/撤销写审计；状态机与 `certificates.status`、`domains.certificate_status` 联动 |
| 管理面 API | 签发 / 续期 / 撤销 / 状态查询接口（补齐需求 2.9 的 `:id/renew`） |
| 前端 Managed 交互 | Domains / Certificates 页支持选择 Managed 来源、触发签发、查看续期状态与有效期 |

### 2.2 本阶段不做（明确排除 / 延后）

| 能力 | 归属 | 说明 |
|------|------|------|
| **Cloudflare Provider / Cloudflare for SaaS / Origin TLS** | **本期明确不做** | 用户已定：暂时不做任何 Cloudflare 对接。`source=cloudflare`、`cloudflare_hostname_id` 字段继续保留但不消费 |
| dns-01 Challenge / DNS Provider 集成 | Phase 7 / 远期 | 本期只做 http-01（+ TLS-ALPN-01 备选），不引入 Route53 / DNSPod / Cloudflare DNS 等 DNS API |
| TLS Passthrough | 远期 | 需求文档已明确暂不实现 |
| 多区域证书策略 / 边缘缓存 | 远期 | 与证书签发正交 |
| 证书撤销联动 CRL/OCSP 在线校验 | 远期 | 本期 `Revoke` 只做 CA 侧撤销 + 本地删除，不做在线吊销检查 |

> 说明：`Maple_Gateway_开发需求任务.md` 把 Cloudflare SaaS 列为"默认生产模式"，本计划依据用户最新决策，**将 ACME 独立于 Cloudflare 先行推进**，两者解耦——Cloudflare 相关能力另立计划或推迟。

---

## 三、起点（Phase 5 已交付 + 现状核实）

写本计划前对仓库实际代码核实，Phase 6 在此之上增量开发：

| 项 | 现状 |
|----|------|
| Certificate 数据模型 | ✅ `certificates` 表（migration 0014）：id / domain_id / hostname / source / status / certificate_pem / private_key_encrypted / issuer / serial_number / issued_at / expires_at / **last_renewed_at** / **last_error** / created_at / updated_at（`ent/schema/certificate.go`） |
| source 枚举 | ✅ `SourceManual / SourceCloudflare / SourceACME / SourceOrigin`（`certificate/model.go`）——`acme` 已预留，本期消费 |
| status 枚举 | ✅ `pending / active / error / expiring / expired`（`model.go`） |
| 私钥加密 | ✅ `internal/certificate/certenc`：AES-256-GCM，密钥来自 `MAPLE_CERT_ENC_KEY`（base64 32B），缺失返回 `ErrNoKey` |
| 证书 Service | ✅ `Service.Upload/Update/Delete/Reload/ReloadAll/ScanExpiring`（`service.go`）：校验 → 加密 → 落库 → 刷新缓存 |
| 内存证书缓存 | ✅ `Cache`（`cache.go`）：hostname → `*Loaded`，`Set/Delete/ReplaceAll` 原子，`RWMutex` 保护 |
| 动态 SNI | ✅ `Getter.GetCertificate`（`sni.go`）：纯内存查找，命中/未中计数，未中回退策略 |
| 校验 | ✅ `validateAndLoad`（`validation.go`）：PEM 解析 / 证书私钥匹配 / SAN 覆盖 / 未过期 |
| 临期扫描 | ✅ `ScanExpiring(warnWindow)`：`ActiveByExpiryBefore` 查询 + 置 `expiring`/`expired`——**只标记告警，不续期**（Phase 6 在此接入续期动作） |
| 仓储 | ✅ `Repository`（`repository.go`）：`All/GetByID/GetByHostname/List/UpdateContent/Delete/ActiveByExpiryBefore/UpdateStatus` |
| 管理面 API | ✅ `GET/POST/PATCH/DELETE /api/admin/certificates`、`/count`、`/:id/status`、`/:id/reload`、`POST /api/admin/domains/:id/certificate`（`api/app.go`，`d.Certificates != nil` 时挂载） |
| 数据面接线 | ✅ `gateway`：`tls.mode=global\|direct`、`TLSConfig{MinVersion,EnforceSNIHostMatch,FallbackCertEnabled,CertEncKey}`、`GetCertificate` 注入（`cmd/server/main.go`） |
| 周期对账 | ✅ `main.go` 5s `ReloadAll` 对账 + 30 天临期扫描循环 |
| 多实例基座 | ✅ Phase 4：`gateway_instances` 表（0011）+ HA coordinator（Redis `SET NX PX` 抢锁 + DB lease 降级）+ Redis 客户端 |
| 审计 / RBAC | ✅ Phase 4：`audit_logs` + `internal/rbac` 三角色；证书写操作已受 RBAC 约束 |
| 前端 | ✅ `CertificatesPage.tsx` 独立证书页 + `DomainsPage.tsx` TLS 列与 `tls_mode` 表单（含 managed 选项占位） |
| 环境变量 | ✅ `MAPLE_TLS_MODE / MAPLE_TLS_MIN_VERSION / MAPLE_TLS_ENFORCE_SNI_HOST_MATCH / MAPLE_TLS_FALLBACK_CERT_ENABLED / MAPLE_TLS_CERT_ENC_KEY` |

> **尚未存在**：`CertificateProvider` 接口（注：`plan-phase5.md` 曾称"Provider 抽象已留位"，经核实**代码中并无该接口**，仅 `source` 枚举与扫描循环预留，本期从零建立）、ACME 客户端与账户、Challenge 处理、自动签发/续期引擎、`POST /api/admin/certificates/:id/renew` 接口、`golang.org/x/crypto/acme` 依赖。这些是本计划全部增量。

> 迁移编号：现有最新为 `0015_domain_tls_fields.sql`，本期从 `0016_*.sql` 起。本计划不预设发布版本号，版本号按实际发版节奏决定。

---

## 四、开发阶段划分

对照需求文档"任务 2.5 / 2.6 / 2.8 Managed 分支"与 Phase 5"十四、Phase 6 衔接"，按依赖编排为六个可验证阶段：

```text
S1  Provider 抽象与数据基座：Provider 接口 + Manual 实现 + certificates 续期字段/审计挂载 + 配置
S2  ACME 客户端与账户：目录 / 账户 / JWS 签名 / staging 打通
S3  Challenge 与域名验证：http-01 代答 + TLS-ALPN-01 备选 + 验证状态机
S4  自动签发：Managed 域名按需签发 → 校验 → 加密落库 → 入缓存
S5  自动续期引擎：临期触发 Renew + 失败重试退避 + 多实例协调 + 生命周期审计
S6  前端 + 端到端验证与发布
```

| 阶段 | 内容 | 涉及模块 | 预期工时 |
|------|------|----------|---------|
| **S1 Provider 抽象与基座** | `CertificateProvider` 接口 + 注册表 + `ManualProvider` 包装 + 续期字段/迁移 + 配置项 | certificate + config + ent | 2-3 天 |
| **S2 ACME 客户端与账户** | ACME directory / account / nonce / JWS 签名 / 错误分类 | certificate/acme | 3-4 天 |
| **S3 Challenge 与验证** | http-01 challenge 代答中间件 + TLS-ALPN-01 备选 + order 状态机 | certificate/acme + gateway + proxy | 3-4 天 |
| **S4 自动签发** | Managed 域名签发编排 + 校验复用 + 加密落库 + 缓存写入 | certificate + domain | 2-3 天 |
| **S5 自动续期引擎** | 续期调度 + 退避重试 + 多实例互斥 + 审计 + 指标 | certificate + ha + audit + metrics | 3-4 天 |
| **S6 前端与收尾验证** | Domains/Certificates Managed 交互 + 端到端/回归 + 文档 | frontend + 全项目 + docs | 3-4 天 |

> **合计约 16-22 天（单人开发）**。S1 是接口基座；S2/S3 是 ACME 协议主体（S3 依赖 S2 的 order/challenge 通道）；S4 依赖 S3；S5 依赖 S4；S6 收口。S2 的账户/目录可用 Let's Encrypt **staging** 目录先行打通，避免生产速率限制。

---

## 五、S1 — CertificateProvider 抽象与数据基座

### 5.1 Provider 接口（对齐需求 2.5）

新增 `server/internal/certificate/provider.go`，定义证书来源统一契约：

```text
CertificateProvider
├── Name() string                                            // "manual" | "acme"
├── Issue(ctx, req IssueRequest) (*Issued, error)            // 申请新证书（Manual 返回 ErrNotSupported）
├── Renew(ctx, req RenewRequest) (*Issued, error)            // 续期（Manual 返回 ErrNotSupported）
├── Revoke(ctx, req RevokeRequest) error                     // 撤销（Manual 无 CA，仅本地）
└── Status(ctx, hostname) (ProviderStatus, error)            // 来源侧状态

IssueRequest  { Hostname, DomainID, ChallengePreference }
RenewRequest  { CertificateID, Hostname }
Issued        { CertificatePEM, PrivateKeyPEM, ChainPEM, NotAfter, Issuer, SerialNumber }
ProviderStatus{ State, Detail, LastError }
```

> 接口**只描述"如何获得证书材料"**，不负责落库/加密/缓存——那些仍由 `Service` 统一编排，保证 provider 可替换而数据副作用集中。Manual 的 `Issue/Renew` 返回 `ErrNotSupported`，其材料由既有 `Upload/Update` 提供。

### 5.2 Provider 注册表

新增 `provider_registry.go`：

```text
Registry
├── Register(p CertificateProvider)
├── Get(source Source) (CertificateProvider, bool)
└── For(hostname) 按 domains.tls_mode / certificates.source 选出 provider
```

- 本期注册 `manual` 与 `acme` 两个 provider；`cloudflare` **不注册**（按 2.2 排除，调用时报 `ErrProviderUnavailable`）。
- `Service` 持有 registry，签发/续期入口按来源分派，不硬编码 if-else。

### 5.3 ManualProvider

`provider_manual.go`：包装 Phase 5 的 `Upload/Update` 语义，`Issue/Renew = ErrNotSupported`，`Status` 直接读 DB 记录。目的只是把"人工来源"纳入统一抽象，为 S4 的编排分支提供对称入口。

### 5.4 数据基座与迁移

`certificates` 已有 `last_renewed_at` / `last_error`，本期补齐续期所需：

```text
0016_certificate_acme.sql
  certificates 追加（可空/带默认，存量行无感）：
    provider_meta   JSON?    # ACME order URL / account ref / challenge type 等来源侧上下文（脱敏）
    renew_attempts  INT default 0   # 连续续期失败计数（退避用，成功后清零）
    next_renew_at   TIMESTAMPTZ?    # 下次续期时间（调度用；空则按 expires_at 推算）
    last_renew_error TEXT?          # 最近一次续期失败原因
  ACME 账户表 acme_accounts（若账户需独立持久化，见 S2）：
    id / directory_url / account_url / email / key_encrypted / status / created_at / updated_at
```

> 结构口径对齐既有：Ent schema 为事实来源 + `go generate` 生成客户端；SQL 迁移作为历史/幂等基线（对齐 0013 以前）。

### 5.5 配置项

```yaml
acme:
  enabled: false                    # 自动签发/续期总开关；关则回退 Phase 5 手动行为
  directory_url: https://acme-v02.api.letsencrypt.org/directory
  # staging 建议：https://acme-staging-v02.api.letsencrypt.org/directory
  email: ""                         # 账户注册邮箱
  challenge: http-01               # http-01（默认）| tls-alpn-01
  key_type: ec256                   # ec256 | rsa2048
  renew_before: 720h                # 提前 30 天续期（对齐 Phase 5 扫描窗口）
  renew_check_interval: 1h          # 续期扫描节奏
  max_renew_attempts: 5             # 连续失败上限，超过转手动告警
  rate_limit_backoff: 6h            # 触发 CA 限流后的最小重试间隔
```

```env
MAPLE_ACME_ENABLED=false
MAPLE_ACME_DIRECTORY_URL=...
MAPLE_ACME_EMAIL=...
MAPLE_ACME_CHALLENGE=http-01
MAPLE_ACME_RENEW_BEFORE=720h
```

### 5.6 验证

- [ ] `Registry` 可注册/取出 manual、acme；`cloudflare` 返回 unavailable
- [ ] `ManualProvider` 行为与 Phase 5 `Upload` 等价（回归：手动作业不破）
- [ ] `0016` 迁移在空库与 Phase 5 存量库上均可执行，存量证书行默认 `renew_attempts=0 / next_renew_at=null`
- [ ] `acme.enabled=false` 时全链路行为与 Phase 5 一致（自动引擎不启动）

---

## 六、S2 — ACME 客户端与账户

### 6.1 模块与文件

```text
server/internal/certificate/acme/
├── client.go        # ACME 目录发现、nonce、JWS/ES256 签名、HTTP 传输
├── directory.go     # newNonce/newAccount/newOrder/revokeCert 端点封装
├── account.go       # 账户注册/查找、账户密钥（JWK）生成与持久化
├── order.go         # newOrder → authorize → finalize → 下载证书 流程
├── errors.go        # 分类错误（限流 / 挑战失败 / 账户无效 / DNS 不可达）
└── client_test.go   # 对 staging 或 httptest 伪 ACME 服务
```

### 6.2 依赖

```text
golang.org/x/crypto/acme        # 优先复用官方 ACME 客户端（directory/account/order/challenge）
（备选）自研 JSON+JWS          # 若需更细粒度控制 order 状态机再评估
```

> 优先采用 `golang.org/x/crypto/acme` 的 `acme.Client`，避免自研 JWS/nonce/防重放等协议细节；仅在需要精细控制 challenge 生命周期时才下沉自研。

### 6.3 账户管理

- 账户密钥（JWK/EC P-256）生成后**加密存储**（复用 `certenc`），落 `acme_accounts.key_encrypted`，绝不落明文、绝不进日志。
- 首次运行自动注册账户（需 `acme.email`）；已存在 `account_url` 则复用。
- 支持 staging 与 production 目录切换（配置驱动）；账户与目录 URL 绑定，切换目录视为新账户。

### 6.4 错误分类

| 场景 | 处理 |
|------|------|
| CA 速率限制（rateLimited/429） | 记录并在 `rate_limit_backoff` 内不再尝试，写 `last_renew_error` |
| 账户无效 / 密钥失配 | 标记账户不可用，停止自动签发并告警（不自动重注册覆盖） |
| 挑战失败 | 计入 `renew_attempts`，按退避重试 |
| 网络 / DNS 不可达 | 归类为瞬时错误，短退避重试 |

### 6.5 验证

- [ ] 对 Let's Encrypt **staging**：能完成目录发现 → 账户注册 → 拿到 `account_url`
- [ ] 账户密钥密文落库，明文/密文均不出现在 API 输出与日志
- [ ] 触发 staging 限流时返回分类错误而非 panic，且不影响其他域名
- [ ] 网络中断归类为瞬时错误并可重试

---

## 七、S3 — Challenge 与域名验证

### 7.1 http-01 优先（本期主路径）

ACME http-01 要求 CA 能访问 `http://{hostname}/.well-known/acme-challenge/{token}`。Maple Gateway 本身就是该 HTTP 入口，故**由数据平面代答挑战**，无需外部组件：

```text
ACME CA ──HTTP GET {hostname}/.well-known/acme-challenge/{token}──▶ Maple Gateway :80
                                                                        │
                                              Challenge Store（内存 token → keyAuthorization）
                                                                        │
                                                       命中 → 200 + keyAuthorization
```

- 新增 `certificate/acme/challenge_http01.go` + 内存 `ChallengeStore`（token → keyAuthorization，带 TTL）。
- 在 **proxy 路由之前**插入对 `/.well-known/acme-challenge/` 前缀的短路处理（先于 Host→Tenant 解析）：命中 store 直接 200，未命中回落常规路由（不误伤真实业务路径）。
- 不查 DB（与热路径原则一致）：Challenge 缓存纯内存 + TTL 自动清理。

### 7.2 TLS-ALPN-01 备选

端口 443 已由数据面持有，TLS-ALPN-01 可作为 http-01 不可用（80 被封）时的备选：在 `tls.Config` 增加 `NextProtos=acme-tls/1` 的临时握手分支，仅对 challenge 期间的 SNI 生效。**列为备选**，S3 先做 http-01，TLS-ALPN-01 视部署条件再落。

### 7.3 验证状态机

```text
Domain（Managed）创建
   ↓
pending（待签发）
   ↓ issue 触发
certificate_pending（order/authorize 进行中）
   ↓ challenge 通过 + finalize
active（证书就绪，enter Route/Cert Cache）
失败：verification_failed / certificate_failed（写 last_error）
```

- **证书未就绪的 Managed 域名不得进入 active 路由**（沿用 Phase 5 语义：无有效缓存证书即握手失败/隔离）。
- `domains.certificate_status` 随签发进度联动（复用 Phase 5 `syncDomainTLS` 扩展出 `managed` 来源）。

### 7.4 验证

- [ ] 本地 httptest 伪 ACME：http-01 代答返回正确 keyAuthorization，非 challenge 路径不受影响
- [ ] 挑战期间临时 token 过期后自动从 store 清除（TTL）
- [ ] `/.well-known/acme-challenge/` 未命中时回落正常业务路由（回归：不误伤真实路径）
- [ ] staging 真实签发：`openssl s_client -servername` 能取到新签发证书

---

## 八、S4 — 自动签发

### 8.1 签发编排

`issue.go`：由 `Service` 编排，Provider 只产出材料：

```text
触发（域名 tls_mode=managed 且无有效证书 / 手动触发 Issue）
   ↓
Registry.For(hostname) → ACMEProvider.Issue()
   ↓ ACME order → challenge → finalize → 下载证书链
复用 Phase 5 validateAndLoad 校验（PEM / 私钥匹配 / SAN 覆盖 / 未过期）
   ↓
certenc 加密私钥 → 落 certificates（source=acme, status=active, last_renewed_at=now）
   ↓
reloadIntoCache（原子写证书缓存）
   ↓
syncDomainTLS（domains.certificate_status=active）+ 审计
```

- **校验复用**：ACME 下发证书同样过 Phase 5 的 `validateAndLoad`，SAN 必须覆盖 hostname，杜绝错发/串域。
- **落库复用**：走既有 `Repository.UpdateContent` / `Create`，不新开写路径。
- 签发失败：状态置 `error` + `last_error`，写负向审计，不影响该域名既有缓存证书（若有旧证则继续服务）。

### 8.2 手动签发入口

`POST /api/admin/certificates/issue`（body：hostname / domain_id / challenge）与 `POST /api/admin/domains/:id/certificate/issue`，受 RBAC 约束（operator / super_admin）。

### 8.3 与 Manual 的共存

- 同一 hostname 既有 manual 证书又要切 managed：以显式动作切换 `source`，切换前校验新证书就绪，避免空窗。
- Managed 签发期间保留旧 manual 证书继续服务，新证就绪后原子替换（无停机）。

### 8.4 验证

- [ ] staging 下 Managed 域名从触发到 `active` 全链路打通，缓存命中新证
- [ ] 签发证书 SAN 不覆盖 hostname 时被拒绝（构造错域证书）
- [ ] 签发失败保留旧证书继续服务，状态置 error 且写审计
- [ ] RBAC：viewer 触发签发被 403

---

## 九、S5 — 自动续期引擎与生命周期自动化

### 9.1 续期调度

`renew.go`：把 Phase 5 的"临期只告警"升级为"临期触发续期"：

```text
周期扫描（renew_check_interval，复用 Phase 5 扫描循环位）
   ↓ 查找 expires_at < now + renew_before 且 provider 支持 Renew 的证书
   ↓ 取多实例互斥锁（Phase 4 Leader / Redis SET NX PX；无 Redis 降级 DB lease）
   ↓ 单实例获得执行权 → ACMEProvider.Renew()
   ↓ 成功：加密落库 + last_renewed_at=now + renew_attempts=0 + next_renew_at 前移
            + 原子刷新缓存 + 审计 + 指标
   ↓ 失败：renew_attempts++ → 指数退避重算 next_renew_at → 写 last_renew_error
            → 达 max_renew_attempts 转手动告警（不无限重试）
```

### 9.2 失败重试与退避

- 退避：`next_renew_at = now + base * 2^attempts`（封顶），受 `rate_limit_backoff` 下限约束。
- 限流感知：ACME 返回 rateLimited 时按 `rate_limit_backoff` 统一推迟，避免雪崩式重试触发更长发禁。
- 达到 `max_renew_attempts`：停止自动重试，状态转 `error`，写高优先级告警日志 + 审计，等待人工介入。

### 9.3 多实例协调

- 续期/签发是**有副作用的状态机流程**，多实例并发会互相踩（重复下单、挑战冲突、CA 限流）。
- 复用 Phase 4 `internal/ha` 的分布式锁（Redis `SET NX PX` 优先，断连降级 DB lease）：同一 hostname 的续期在任一时刻仅一个实例执行。
- 证书变更后跨实例收敛：Phase 5 现为 5s `ReloadAll` 周期对账兜底（无广播）；本期**维持周期对账**为主（避免为证书引入新的广播通道），文档明确该取舍。

### 9.4 生命周期审计与指标

| 动作 | 审计 action | 指标 |
|------|-------------|------|
| 自动/手动签发 | `certificate.issue` | `maple_acme_issue_total{result}` |
| 自动/手动续期 | `certificate.renew` | `maple_acme_renew_total{result}` |
| 撤销 | `certificate.revoke` | `maple_acme_revoke_total{result}` |
| 续期失败 / 账户异常 | `certificate.renew_failed` | `maple_acme_renew_attempts` |
| 证书到期状态 | — | `maple_certificate_expiring`（Phase 5 已有，续期成功后回退） |

> 指标不把 hostname 作为高基数 label（对齐 Phase 5 提示）；hostname 只进日志。

### 9.5 验证

- [ ] 把 staging 证书有效期人为缩短 / 提前 `renew_before`，触发自动续期并原子换证，在途连接不断
- [ ] 续期成功后 `last_renewed_at` 更新、`renew_attempts` 清零、`next_renew_at` 前移、审计落库
- [ ] 连续失败按指数退避，达上限转告警并停止重试
- [ ] 双实例并发扫描同一批临期证书时，仅一个实例执行续期（互斥生效，无重复下单）
- [ ] `acme.enabled=false` 时退化为 Phase 5 行为（仅告警不续期）

---

## 十、S6 — 前端与收尾验证

### 10.1 前端扩展

Domains / Certificates 页在 Phase 5 TLS 区块之上：

- 来源切换：Manual / Managed（Disabled 保留）；选 Managed 展示签发状态与 challenge 进度。
- 动作按钮：签发（Issue）、续期（Renew）、撤销（Revoke）、重载（Reload）——按 RBAC 显示/禁用（viewer 只读）。
- 证书详情：Issuer / Issued At / Expires At / **Last Renewed** / **Next Renew** / Last Error / renew_attempts。
- 状态可视化：`pending / certificate_pending / active / error / expiring / expired` 徽章（复用现有 StatusBadge）。

### 10.2 端到端回归 / 测试计划（对齐需求 2.5/2.6/2.8 与 Phase 5 遗留）

- [ ] Managed 域名自动签发：触发 → challenge 通过 → `active` → 握手取新证
- [ ] 自动续期：临期触发 → 换证 → 旧 keep-alive 不受影响
- [ ] 撤销：CA 侧撤销 + 本地删除 + 缓存摘除
- [ ] 失败路径：挑战失败 / 账户无效 / 限流 → 分类错误 + 退避 + 审计
- [ ] 多实例：签发/续期互斥，跨实例在周期对账窗口内收敛
- [ ] 故障隔离：单域名签发失败不影响其他域名与监听链路
- [ ] 私钥零回显：全量 API 输出 + 日志检索不含 private_key（明文或密文）
- [ ] `acme.enabled=false` 回归：行为与 Phase 5 完全一致
- [ ] `tls.mode=global` 行为不受 Phase 6 影响（回归基线）

### 10.3 质量要求

1. 握手热路径零 DB 查询；续期为离线流程，不进入 `GetCertificate`。
2. 私钥（含 ACME 账户密钥）加密不可降级；明文/密文零 API 输出、零日志。
3. `go vet ./...` 通过；`go test ./...` 全绿；`-race` 建议 Linux/CI 复核（对齐 Phase 4/5 遗留）。
4. 单租户证书签发/续期故障隔离，不拖垮监听与其余租户。
5. 签发/续期/撤销全审计，可追溯。
6. 文档联动更新：本计划勾选；`API接口全面分析.md` / `项目功能全面分析.md` / `项目目录结构.md` 增补 ACME 接口与目录。

### 10.4 遗留复核项（建议 CI / Linux 补）

- `go test -race ./...` 全量（并发续期 / 缓存替换建议 CI 复核）。
- ACME 速率限制压力下的重试行为（用 staging 观察）。
- 大量域名（100/1000）批量续期时的调度吞吐与缓存替换开销。

---

## 十一、关键设计决策

### 11.1 Provider 抽象只描述"如何取得材料"

落库、加密、缓存、审计全部集中在 `Service`，Provider 无副作用（除 ACME 网络交互）。这样 Manual / ACME 可对称替换，且未来新增来源（如远期 Cloudflare）只加 Provider、不动编排层——同时满足本期"不接 Cloudflare"的隔离要求。

### 11.2 http-01 优先，复用网关自身为入口

Maple Gateway 本就是 HTTP 入口，http-01 由数据平面代答挑战最省依赖，不引入 DNS Provider。代价是要求 80 端口对 CA 可达；TLS-ALPN-01 作为 80 被封时的备选，dns-01 明确延后。

### 11.3 续期走"离线装载 + 原子替换"，绝不进握手路径

沿用 Phase 5 核心原则：续期在网络层完成后统一落库 + `Cache.Set` 原子替换；`GetCertificate` 永远只读内存。旧连接按旧证书自然结束，新连接即用新证。

### 11.4 多实例续期必须互斥

续期是有副作用的 CA 交互，多实例并发会触发重复下单与限流。复用 Phase 4 分布式锁保证单执行者；跨实例证书收敛**维持 5s 周期对账**（不为此新增广播通道），是刻意的简化取舍。

### 11.5 自动化默认关闭、可降级

`acme.enabled` 默认 `false`：未显式开启时系统回到 Phase 5（手动上传 + 临期告警），保证存量部署升级无行为突变；开启后自动层叠加在人工基线之上，人工可随时接管。

### 11.6 账户密钥与证书私钥同标准

ACME 账户密钥等同敏感凭据，与租户私钥同走 `certenc` AES-GCM 加密、同受"无 `MAPLE_CERT_ENC_KEY` 拒绝启动"约束、同样零回显。

---

## 十二、技术依赖与配置清单

### 新增依赖

```text
golang.org/x/crypto/acme               # ACME 客户端（directory/account/order/challenge）
golang.org/x/crypto/acme/autocert      # 可选：仅参考其 challenge/cache 实现，不直接用于多租户
（已有）crypto/aes + crypto/cipher + crypto/x509 + crypto/tls   # 加密与证书解析
（已有）github.com/redis/go-redis/v9   # 多实例续期互斥锁（复用 Phase 4）
```

> 不引入 Cloudflare / DNS Provider SDK（本期无 dns-01、无 Cloudflare）。

### 环境变量 / 配置新增

```yaml
acme:
  enabled: false
  directory_url: https://acme-v02.api.letsencrypt.org/directory
  email: ""
  challenge: http-01
  key_type: ec256
  renew_before: 720h
  renew_check_interval: 1h
  max_renew_attempts: 5
  rate_limit_backoff: 6h
```

```env
MAPLE_ACME_ENABLED=false
MAPLE_ACME_DIRECTORY_URL=...
MAPLE_ACME_EMAIL=...
MAPLE_ACME_CHALLENGE=http-01
MAPLE_ACME_RENEW_BEFORE=720h
```

### 迁移新增

```text
0016_certificate_acme.sql          # certificates 续期字段 + acme_accounts 表
```

### Ent 新增/修改

```text
ent/schema/certificate.go          # 追加 provider_meta / renew_attempts / next_renew_at / last_renew_error
ent/schema/acmeaccount.go          # 新增（directory_url / account_url / email / key_encrypted / status）
```

---

## 十三、验收 / 交付标准

### 功能交付

1. [ ] `CertificateProvider` 抽象落地（Manual + ACME 两实现），来源可插拔，cloudflare 明确不可用。——S1
2. [ ] ACME 客户端可用：staging 完成账户注册与目录发现，错误分类清晰。——S2
3. [ ] http-01 挑战由数据平面代答，不影响正常业务路由；Managed 域名可自动签发至 `active`。——S3/S4
4. [ ] 自动续期：临期触发 Renew → 原子换证 → 在途连接不受影响；失败指数退避、达上限转告警。——S5
5. [ ] 多实例续期互斥，跨实例在周期对账窗口内收敛，无重复下单。——S5
6. [ ] 签发/续期/撤销全审计；RBAC 联动作业权限；私钥（含账户密钥）零回显。——S4/S5
7. [ ] 前端支持 Managed 来源选择、签发/续期/撤销动作与续期状态展示。——S6
8. [ ] `acme.enabled=false` 与 `tls.mode=global` 回归基线不破。——S1/S6

### 质量要求

1. TLS 握手热路径零 DB 查询；续期为离线流程，缓存替换并发安全。
2. 私钥与账户密钥加密不可降级；明文/密文零 API 输出、零日志。
3. `go vet ./...` 通过；`go test ./...` 全绿；`-race` 建议 CI/Linux 复核。
4. 单租户签发/续期故障隔离；失败不拖垮监听与其他租户。
5. 文档联动更新：本计划勾选 + 配套分析文档同步。

### 非交付（Phase 7 / 远期）

- dns-01 Challenge 与 DNS Provider 集成（Route53 / DNSPod / 等）。
- **Cloudflare Provider 与 Cloudflare for SaaS 全自动化**（本期明确排除，另立计划）。
- 证书撤销的 CRL/OCSP 在线校验、多区域证书策略、边缘缓存 / CDN 回源。

---

## 十四、Phase 7 衔接（预告）

Phase 6 落地 Managed Certificate + ACME 自动签发续期后，Gateway 具备面向租户的**证书自动化**能力。Phase 7 在此之上叠加：

```text
dns-01 Challenge（泛域名 / 80 端口不可达场景）+ DNS Provider 抽象
   ↓
证书来源统一管理层（Manual / ACME / 远期 Cloudflare 共用同一 Provider 契约）
   ↓
证书生命周期全自动化（失败自愈 / 分级告警 / 与发布流程联动）
   ↓
（远期）Cloudflare for SaaS + Origin TLS + 多区域证书策略
```

Phase 6 预留的 `CertificateProvider` 接口、`certificates.provider_meta` 字段与 `acme_accounts` 表，即为 Phase 7 的 dns-01 与远期 Cloudflare（`source=cloudflare`、`cloudflare_hostname_id`）挂载点。
