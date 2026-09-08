package settings

import (
	"encoding/json"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
	"go.uber.org/zap"
)

// Handler 暴露 Settings Management API。
type Handler struct {
	repo *Repository
}

// NewHandler 构造。
func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

// sectionValue 管理端视图：section → {key: {value, version}}。
func groupBySection(items []Entry) fiber.Map {
	out := fiber.Map{}
	for _, e := range items {
		sec := string(e.Section)
		m, _ := out[sec].(fiber.Map)
		if m == nil {
			m = fiber.Map{}
			out[sec] = m
		}
		var val any
		if err := json.Unmarshal(e.Value, &val); err != nil {
			val = nil
		}
		m[e.Key] = fiber.Map{"value": val, "version": e.Version, "updated_at": e.UpdatedAt}
	}
	return out
}

// Get GET /api/admin/settings?section=
func (h *Handler) Get(c fiber.Ctx) error {
	all, err := h.repo.All(c.Context())
	if err != nil {
		return pkg.Err(c, err)
	}
	if sec := c.Query("section"); sec != "" {
		filtered := []Entry{}
		for _, e := range all {
			if string(e.Section) == sec {
				filtered = append(filtered, e)
			}
		}
		return pkg.OK(c, fiber.Map{sec: groupBySection(filtered)[sec]})
	}
	return pkg.OK(c, groupBySection(all))
}

// Update PATCH /api/admin/settings/:section  body {key: value, ...}
// 保存即生效（写入 DB + 重载 Store 缓存）；audit 中间件自动记录 action=settings。
func (h *Handler) Update(c fiber.Ctx) error {
	section := Section(c.Params("section"))
	switch section {
	case SectionGateway, SectionProxy, SectionHealth, SectionSecurity, SectionLogging, SectionMetrics, SectionAppearance:
	default:
		return pkg.Err(c, pkg.ErrValidation("无效的 section（gateway/proxy/health/security/logging/metrics/appearance）"))
	}
	var body map[string]json.RawMessage
	if err := c.Bind().Body(&body); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if len(body) == 0 {
		return pkg.Err(c, pkg.ErrValidation("请求体不能为空"))
	}
	for key, val := range body {
		if _, err := h.repo.Upsert(c.Context(), section, key, val); err != nil {
			// debug 级：默认 info 不输出，排查时把 log.level 调成 debug 即可看到保存失败原因。
			pkg.Log().Debug("settings update: upsert failed",
				zap.String("section", string(section)), zap.String("key", key), zap.Error(err))
			return pkg.Err(c, err)
		}
	}
	if err := h.repo.Reload(c.Context()); err != nil {
		pkg.Log().Debug("settings update: reload failed", zap.Error(err))
		return pkg.Err(c, err)
	}
	// 日志分区保存后按 logging.debug 同步全局日志级别（设置页 debug 开关即时生效）。
	SyncLogLevel(h.repo)
	return h.Get(c)
}

// validSection 校验 section 是否受支持。
func validSection(s string) bool {
	switch Section(s) {
	case SectionGateway, SectionProxy, SectionHealth, SectionSecurity, SectionLogging, SectionMetrics, SectionAppearance:
		return true
	}
	return false
}

// History GET /api/admin/settings/:section/history?key=
func (h *Handler) History(c fiber.Ctx) error {
	section := Section(c.Params("section"))
	if !validSection(string(section)) {
		return pkg.Err(c, pkg.ErrValidation("无效的 section"))
	}
	key := c.Query("key")
	if key == "" {
		return pkg.Err(c, pkg.ErrValidation("缺少 key 参数"))
	}
	list, err := h.repo.History(c.Context(), section, key)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, list)
}

// Rollback PATCH /api/admin/settings/:section/rollback  body {key, version}
func (h *Handler) Rollback(c fiber.Ctx) error {
	section := Section(c.Params("section"))
	if !validSection(string(section)) {
		return pkg.Err(c, pkg.ErrValidation("无效的 section"))
	}
	var in struct {
		Key     string `json:"key" validate:"required"`
		Version int    `json:"version" validate:"required,min=1"`
	}
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if err := pkg.ValidateStruct(in); err != nil {
		return pkg.Err(c, err)
	}
	ent, err := h.repo.Rollback(c.Context(), section, in.Key, in.Version)
	if err != nil {
		return pkg.Err(c, err)
	}
	if err := h.repo.Reload(c.Context()); err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, ent)
}
