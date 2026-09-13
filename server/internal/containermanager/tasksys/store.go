// store.go 为任务系统提供可选的数据库持久化。
//
// 配置了 database_url 时，任务的全生命周期（创建/下发/进度/结果/超时）写穿到
// cm_tasks，进程重启后可查历史；未配置时全部退化为进程内存储（开发用）。
package tasksys

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// Store 把 Task 落库到 cm_tasks。
type Store struct {
	db *sql.DB
}

// NewStore 构造。db 为空时所有方法为无操作。
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Save 以 UPSERT 写入任务当前快照。失败不阻断内存编排。
func (s *Store) Save(t *Task) {
	if s.db == nil {
		return
	}
	snap := t.Snapshot()
	params := rawOrNull(snap.Params)
	result := rawOrNull(snap.Result)
	_, _ = s.db.ExecContext(context.Background(), `
		INSERT INTO cm_tasks (id, node_id, action, params, status, percent, message, error, result,
		                      request_id, parent_task_id, created_by, attempts,
		                      created_at, deadline_at, started_at, finished_at, updated_at)
		VALUES ($1, NULLIF($2,'')::uuid, $3, $4::jsonb, $5, $6, $7, $8, $9::jsonb,
		        $10, $11, $12, $13,
		        $14, $15, $16, $17, now())
		ON CONFLICT (id) DO UPDATE SET
			status=EXCLUDED.status, percent=EXCLUDED.percent, message=EXCLUDED.message,
			error=EXCLUDED.error, result=EXCLUDED.result, attempts=EXCLUDED.attempts,
			started_at=EXCLUDED.started_at, finished_at=EXCLUDED.finished_at,
			deadline_at=EXCLUDED.deadline_at, updated_at=now()`,
		snap.ID, snap.NodeID, snap.Action, params, string(snap.Status), snap.Percent,
		snap.Message, snap.Error, result, snap.RequestID, snap.ParentID, snap.CreatedBy, snap.Attempts,
		msTime(snap.CreatedMs), msTime(snap.DeadlineMs), msTime(snap.StartedMs), msTime(snap.FinishedMs))
}

// load 读取最近 1000 条任务记录，按创建时间倒序。
func (s *Store) load(ctx context.Context) ([]*Task, error) {
	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(node_id::text,''), action, COALESCE(params,'null'::jsonb)::text,
		       status, percent, message, error, COALESCE(result,'null'::jsonb)::text,
		       request_id, COALESCE(parent_task_id,''), COALESCE(created_by,''), attempts,
		       (EXTRACT(EPOCH FROM created_at)*1000)::bigint,
		       COALESCE((EXTRACT(EPOCH FROM deadline_at)*1000)::bigint,0),
		       COALESCE((EXTRACT(EPOCH FROM started_at)*1000)::bigint,0),
		       COALESCE((EXTRACT(EPOCH FROM finished_at)*1000)::bigint,0)
		FROM cm_tasks
		ORDER BY created_at DESC
		LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*Task, 0, 64)
	for rows.Next() {
		var (
			id, nodeID, action, params, status, message, errMsg, result string
			reqID, parent, createdBy                                    string
			percent, attempts                                           int
			createdMs, deadlineMs, startedMs, finishedMs                int64
		)
		if err := rows.Scan(&id, &nodeID, &action, &params, &status, &percent, &message, &errMsg,
			&result, &reqID, &parent, &createdBy, &attempts,
			&createdMs, &deadlineMs, &startedMs, &finishedMs); err != nil {
			return nil, err
		}
		t := newTask()
		t.ID = id
		t.NodeID = nodeID
		t.Action = action
		t.Params = json.RawMessage(params)
		t.Status = Status(status)
		t.Percent = percent
		t.Message = message
		t.Error = errMsg
		t.Result = json.RawMessage(result)
		t.RequestID = reqID
		t.ParentID = parent
		t.CreatedBy = createdBy
		t.Attempts = attempts
		t.CreatedMs = createdMs
		t.DeadlineMs = deadlineMs
		t.StartedMs = startedMs
		t.FinishedMs = finishedMs
		out = append(out, t)
	}
	return out, rows.Err()
}

// rawOrNull 返回可写入 jsonb 列的字节；空值归一为 JSON null。
func rawOrNull(r json.RawMessage) []byte {
	if len(r) == 0 {
		return []byte("null")
	}
	return []byte(r)
}

// msTime 把毫秒时间戳转为 time.Time；0 视为未设置，返回 nil（写 NULL）。
func msTime(ms int64) any {
	if ms == 0 {
		return nil
	}
	return time.UnixMilli(ms)
}
