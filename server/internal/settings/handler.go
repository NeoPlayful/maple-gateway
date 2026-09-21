package settings

import (
	"encoding/json"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/ipam"
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
		m[e.Key] = fiber.Map{"value": val, "version": e.Version, "created_at": e.CreatedAt, "updated_at": e.UpdatedAt}
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
	case SectionGateway, SectionProxy, SectionHealth, SectionACME, SectionSecurity, SectionLogging, SectionMetrics, SectionAppearance, SectionContainer:
	default:
		return pkg.Err(c, pkg.ErrValidation("无效的 section（gateway/proxy/health/acme/security/logging/metrics/appearance/container）"))
	}
	var body map[string]json.RawMessage
	if err := c.Bind().Body(&body); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if len(body) == 0 {
		return pkg.Err(c, pkg.ErrValidation("请求体不能为空"))
	}
	// acme 分区：启用开关有跨字段约束（需 directory_url + email 齐备），
	// 校验值来自本次提交（优先）或当前已存配置。
	if section == SectionACME {
		if err := h.validateACMEEnabled(body); err != nil {
			return pkg.Err(c, err)
		}
	}
	for key, val := range body {
		// health/proxy 等运行时键做范围校验，非法值直接拒绝，避免把网关配成不可用。
		if err := ValidateRuntimeKey(section, key, val); err != nil {
			return pkg.Err(c, err)
		}
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
	case SectionGateway, SectionProxy, SectionHealth, SectionACME, SectionSecurity, SectionLogging, SectionMetrics, SectionAppearance, SectionContainer:
		return true
	}
	return false
}

// validateACMEEnabled 校验 acme 分区提交的跨字段约束：enabled=true 时
// directory_url 与 email 必须齐备。值优先取本次提交，缺省回退当前已存配置。
func (h *Handler) validateACMEEnabled(body map[string]json.RawMessage) error {
	enabled := h.repo.GetBool(SectionACME, KeyACMEEnabled, false)
	if raw, ok := body[KeyACMEEnabled]; ok {
		var b bool
		if json.Unmarshal(raw, &b) == nil {
			enabled = b
		}
	}
	if !enabled {
		return nil
	}
	dirURL := h.repo.GetString(SectionACME, KeyACMEDirectoryURL, "")
	if raw, ok := body[KeyACMEDirectoryURL]; ok {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			dirURL = s
		}
	}
	email := h.repo.GetString(SectionACME, KeyACMEEmail, "")
	if raw, ok := body[KeyACMEEmail]; ok {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			email = s
		}
	}
	return ValidateACMEEnabled(enabled, dirURL, email)
}

// GetContainerNetwork GET /api/admin/system/container-network
// 返回系统默认容器网络配置（缺失键回退内建默认）。
func (h *Handler) GetContainerNetwork(c fiber.Ctx) error {
	return pkg.OK(c, h.repo.GetContainerNetwork())
}

// UpdateContainerNetwork PUT /api/admin/system/container-network
// 保存系统默认容器网络配置。仅影响未来新节点的默认池，不回溯修改已有池。
func (h *Handler) UpdateContainerNetwork(c fiber.Ctx) error {
	var in ContainerNetworkConfig
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	// 校验默认池：CIDR 合法、项目前缀合法、池不与保留段重叠。
	pool, err := ipam.Parse(in.DefaultNetworkPool)
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("默认地址池非法: "+err.Error()))
	}
	if _, err := pool.Capacity(in.DefaultProjectPrefix); err != nil {
		return pkg.Err(c, pkg.ErrValidation("默认项目前缀非法: "+err.Error()))
	}
	if _, err := ipam.ValidatePool(pool); err != nil {
		return pkg.Err(c, pkg.ErrValidation("默认地址池不可用: "+err.Error()))
	}
	if in.AllocationMode == "" {
		in.AllocationMode = DefaultAllocationMode
	}
	if in.AllocationMode != DefaultAllocationMode {
		return pkg.Err(c, pkg.ErrValidation("暂仅支持 sequential 分配模式"))
	}
	if in.DefaultReuseDelaySeconds < 0 {
		return pkg.Err(c, pkg.ErrValidation("复用冷却时间不可为负"))
	}
	if err := h.repo.SaveContainerNetwork(c.Context(), in); err != nil {
		return pkg.Err(c, err)
	}
	if err := h.repo.Reload(c.Context()); err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, h.repo.GetContainerNetwork())
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
