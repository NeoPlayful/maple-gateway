package api

import (
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/tasksys"
	"github.com/gofiber/fiber/v3"
)

// TaskSource 提供任务列表与按 ID 查询（由 tasksys.Manager 装配）。
type TaskSource struct {
	List   func() []*tasksys.Task
	Get    func(id string) (*tasksys.Task, bool)
	Retry  func(id string) (*tasksys.Task, error)
	Cancel func(id string, reason string) error
}

// registerTasks 挂载任务管理接口（令牌认证，供 Gateway 聚合代理调用）。
// 任务为后台可查/可重试的一次性下发动作（镜像拉取、人工启停等）。
func registerTasks(app *fiber.App, token string, t TaskSource) {
	g := app.Group("/api/mgmt/tasks", gatewayAuth(token))

	// GET /api/mgmt/tasks 列出全部任务（最多 1000 条，最新在前）。
	g.Get("/", func(c fiber.Ctx) error {
		if t.List == nil {
			return c.JSON([]*tasksys.Task{})
		}
		return c.JSON(t.List())
	})

	// GET /api/mgmt/tasks/:id 查看任务详情（含 payload/result）。
	g.Get("/:id", func(c fiber.Ctx) error {
		if t.Get == nil {
			return fiber.NewError(fiber.StatusNotFound, "task not found")
		}
		task, ok := t.Get(c.Params("id"))
		if !ok {
			return fiber.NewError(fiber.StatusNotFound, "task not found")
		}
		return c.JSON(task)
	})

	// POST /api/mgmt/tasks/:id/retry 基于终态任务重新下发。
	g.Post("/:id/retry", func(c fiber.Ctx) error {
		if t.Retry == nil {
			return fiber.NewError(fiber.StatusNotImplemented, "未启用任务重试")
		}
		task, err := t.Retry(c.Params("id"))
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		return c.JSON(task)
	})

	// POST /api/mgmt/tasks/:id/cancel 取消未结束的任务。
	g.Post("/:id/cancel", func(c fiber.Ctx) error {
		if t.Cancel == nil {
			return fiber.NewError(fiber.StatusNotImplemented, "未启用任务取消")
		}
		var in struct {
			Reason string `json:"reason"`
		}
		_ = c.Bind().Body(&in)
		if err := t.Cancel(c.Params("id"), in.Reason); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		return c.JSON(fiber.Map{"cancelled": true})
	})
}
