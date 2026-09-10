package certificate

import (
	"strconv"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// Handler 暴露 Certificate 的 Management API。
// 注意：所有输出模型均不含 private_key / private_key_encrypted（见 model.go JSON tag）。
type Handler struct {
	svc    *Service
	getter *Getter // 可空：数据面 SNI getter 引用，用于统计展示
}

// NewHandler 构造。
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// SetGetter 注入数据面 SNI getter（main 层在构造 Getter 后调用，用于命中/未中统计）。
func (h *Handler) SetGetter(g *Getter) { h.getter = g }

// Count GET /api/admin/certificates/count（缓存条目数，运维查看）
func (h *Handler) Count(c fiber.Ctx) error {
	out := fiber.Map{"cached": h.svc.Cache().Len()}
	if h.getter != nil {
		hits, misses := h.getter.Stats()
		out["cache_hits"] = hits
		out["cache_misses"] = misses
	}
	return pkg.OK(c, out)
}

// List GET /api/admin/certificates?limit=&offset=
func (h *Handler) List(c fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	items, total, err := h.svc.List(c.Context(), limit, offset)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OKMeta(c, items, fiber.Map{"total": total, "limit": limit, "offset": offset})
}

// Get GET /api/admin/certificates/:id
func (h *Handler) Get(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的证书 ID"))
	}
	rec, err := h.svc.Get(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, rec)
}

// Upload POST /api/admin/domains/:id/certificate  或  POST /api/admin/certificates
// 前者绑定具体域名；后者 body 自含 hostname（默认手动域名）。
// 需求路径为 POST /api/admin/domains/:id/certificate，同时提供泛化入口便于独立管理。
func (h *Handler) Upload(c fiber.Ctx) error {
	var in New
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if did := c.Params("id"); did != "" {
		id, err := uuid.Parse(did)
		if err != nil {
			return pkg.Err(c, pkg.ErrValidation("无效的域名 ID"))
		}
		in.DomainID = &id
	}
	if err := pkg.ValidateStruct(in); err != nil {
		return pkg.Err(c, err)
	}
	rec, err := h.svc.Upload(c.Context(), in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, rec)
}

// Update PATCH /api/admin/certificates/:id
// 更换指定证书的材料（续期/替换）：hostname 不变，证书与私钥必填。
func (h *Handler) Update(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的证书 ID"))
	}
	var in UpdateRequest
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if err := pkg.ValidateStruct(in); err != nil {
		return pkg.Err(c, err)
	}
	rec, err := h.svc.Update(c.Context(), id, in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, rec)
}

// Delete DELETE /api/admin/certificates/:id
// 语义等同于"撤销并移除"：来源支持时先向 CA 作废，再删本地记录。
// CA 撤销失败不阻断删除，在响应 revoke_error 中回报。
func (h *Handler) Delete(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的证书 ID"))
	}
	out, err := h.svc.Delete(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	res := fiber.Map{"deleted": true}
	if out != nil && out.RevokeError != "" {
		res["revoke_error"] = out.RevokeError
	}
	return pkg.OK(c, res)
}

// Reload POST /api/admin/certificates/:id/reload
func (h *Handler) Reload(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的证书 ID"))
	}
	rec, err := h.svc.Reload(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, rec)
}

// IssueReq 是签发请求体。domain_id 必填：证书必须绑定到具体域名。
type IssueReq struct {
	Hostname  string     `json:"hostname" validate:"required"`
	DomainID  *uuid.UUID `json:"domain_id" validate:"required"`
	Source    string     `json:"source"`    // acme（默认）/ manual
	Challenge string     `json:"challenge"` // http-01（默认）
}

// Issue POST /api/admin/certificates/issue  或  POST /api/admin/domains/:id/certificate/issue
// 触发 Managed/ACME 自动签发；来源不支持主动签发时返回校验错误。
func (h *Handler) Issue(c fiber.Ctx) error {
	var in IssueReq
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if did := c.Params("id"); did != "" {
		id, err := uuid.Parse(did)
		if err != nil {
			return pkg.Err(c, pkg.ErrValidation("无效的域名 ID"))
		}
		in.DomainID = &id
	}
	if err := pkg.ValidateStruct(in); err != nil {
		return pkg.Err(c, err)
	}
	src := Source(in.Source)
	if src == "" {
		src = SourceACME
	}
	chal := ChallengeType(in.Challenge)
	if chal == "" {
		chal = ChallengeHTTP01
	}
	rec, err := h.svc.Issue(c.Context(), in.Hostname, in.DomainID, src, chal)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, rec)
}

// Renew POST /api/admin/certificates/:id/renew
// 立即续期（跳过临期窗口）；来源不支持续期时返回校验错误。
func (h *Handler) Renew(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的证书 ID"))
	}
	rec, err := h.svc.RenewNow(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, rec)
}

// Status GET /api/admin/certificates/:id/status（直接复用详情；返回证书状态视图）
func (h *Handler) Status(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的证书 ID"))
	}
	rec, err := h.svc.Get(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, fiber.Map{
		"id":            rec.ID,
		"hostname":      rec.Hostname,
		"domain_id":     rec.DomainID,
		"source":        rec.Source,
		"status":        rec.Status,
		"issuer":        rec.Issuer,
		"serial_number": rec.SerialNumber,
		"issued_at":     rec.IssuedAt,
		"expires_at":    rec.ExpiresAt,
		"last_error":    rec.LastError,
	})
}
