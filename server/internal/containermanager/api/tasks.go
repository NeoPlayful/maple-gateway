package api

import (
	"strconv"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/tasksys"
	"github.com/gofiber/fiber/v3"
)

// 任务列表分页缺省与上限：任务表随观测循环无界增长，必须分页取数。
const (
	defaultTaskLimit = 20
	maxTaskLimit     = 200
)

// TaskSource 提供任务分页列表与按 ID 查询（由 tasksys.Manager 装配）。
type TaskSource struct {
	Page   func(nodeID string, limit, offset int) ([]*tasksys.Task, int)
	Get    func(id string) (*tasksys.Task, bool)
	Retry  func(id string) (*tasksys.Task, error)
	Cancel func(id string, reason string) error
}

// registerTasks 挂载任务管理接口（令牌认证，供 Gateway 聚合代理调用）。
// 任务为后台可查/可重试的一次性下发动作（镜像拉取、人工启停等）。
func registerTasks(app *fiber.App, token string, t TaskSource) {
	g := app.Group("/api/mgmt/tasks", gatewayAuth(token))

	// GET /api/mgmt/tasks?node_id=&limit=&offset= 分页列出任务（最新在前）。
	// 返回 {tasks:[...], total:N}：total 为过滤后的总数，供前端计算页码。
	g.Get("/", func(c fiber.Ctx) error {
		if t.Page == nil {
			return c.JSON(fiber.Map{"tasks": []*tasksys.Task{}, "total": 0})
		}
		nodeID := c.Query("node_id")
		limit, _ := strconv.Atoi(c.Query("limit", strconv.Itoa(defaultTaskLimit)))
		if limit <= 0 {
			limit = defaultTaskLimit
		}
		if limit > maxTaskLimit {
			limit = maxTaskLimit
		}
		offset, _ := strconv.Atoi(c.Query("offset", "0"))
		if offset < 0 {
			offset = 0
		}
		tasks, total := t.Page(nodeID, limit, offset)
		return c.JSON(fiber.Map{"tasks": tasks, "total": total})
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
