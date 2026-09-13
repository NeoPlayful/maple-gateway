package api

import (
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/enrollment"
	"github.com/gofiber/fiber/v3"
)

// registerEnrollment 挂载 Enrollment Token 管理接口（令牌认证，供 Gateway 聚合代理调用）。
// 节点注册本身走 WebSocket 握手（agent.hello 携带 token），不在此处。
func registerEnrollment(app *fiber.App, token string, tokens *enrollment.TokenStore) {
	g := app.Group("/api/mgmt/enrollment-tokens", gatewayAuth(token))

	// GET /api/mgmt/enrollment-tokens 列出全部 Token（含 used/revoked，供管理端展示）。
	g.Get("/", func(c fiber.Ctx) error {
		list := tokens.List()
		now := time.Now()
		out := make([]tokenView, 0, len(list))
		for _, t := range list {
			out = append(out, toView(t, now))
		}
		return c.JSON(out)
	})

	// POST /api/mgmt/enrollment-tokens 签发一个一次性注册令牌。
	// 请求体：{ note, ttl_seconds, created_by }。
	g.Post("/", func(c fiber.Ctx) error {
		var in struct {
			Note       string `json:"note"`
			TTLSeconds int    `json:"ttl_seconds"`
			CreatedBy  string `json:"created_by"`
		}
		if err := c.Bind().Body(&in); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "请求体格式错误")
		}
		ttl := time.Duration(in.TTLSeconds) * time.Second
		t, err := tokens.IssueBy(in.Note, ttl, in.CreatedBy)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return c.JSON(toView(t, time.Now()))
	})

	// DELETE /api/mgmt/enrollment-tokens/:id 撤销未使用的签发。
	g.Delete("/:id", func(c fiber.Ctx) error {
		if !tokens.Revoke(c.Params("id")) {
			return fiber.NewError(fiber.StatusNotFound, "令牌不存在或已不可撤销")
		}
		return c.JSON(fiber.Map{"revoked": true})
	})
}

// tokenView 是对外暴露的 Token 视图。已消费的 Token 不返回明文（改为空）。
type tokenView struct {
	ID        string `json:"id"`
	Value     string `json:"value,omitempty"`
	Note      string `json:"note,omitempty"`
	Status    string `json:"status"`
	CreatedBy string `json:"created_by,omitempty"`
	CreatedMs int64  `json:"created_at_ms"`
	ExpiresMs int64  `json:"expires_at_ms"`
	UsedMs    int64  `json:"used_at_ms,omitempty"`
	UsedBy    string `json:"used_by_node_id,omitempty"`
}

// toView 把内部 Token 映射为对外视图：仅 active Token 回显明文 value。
func toView(t *enrollment.Token, now time.Time) tokenView {
	status := t.DisplayStatus(now)
	v := tokenView{
		ID:        t.ID,
		Note:      t.Note,
		Status:    string(status),
		CreatedBy: t.CreatedBy,
		CreatedMs: t.CreatedMs,
		ExpiresMs: t.ExpiresMs,
		UsedMs:    t.UsedMs,
		UsedBy:    t.UsedBy,
	}
	if status == enrollment.TokenActive {
		v.Value = t.Value
	}
	return v
}
