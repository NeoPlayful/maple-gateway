package certificate

import (
	"time"

	"github.com/google/uuid"
)

// OperationStatus 是签发/续期过程的阶段进度。
type OperationStatus string

const (
	OpQueued             OperationStatus = "queued"              // 已受理
	OpAccountReady       OperationStatus = "account_ready"       // CA 账户就绪
	OpOrderCreated       OperationStatus = "order_created"       // 订单已创建
	OpChallengePresented OperationStatus = "challenge_presented" // 挑战已登记，等 CA 验证
	OpChallengeValidated OperationStatus = "challenge_validated" // 域名验证通过
	OpFinalizing         OperationStatus = "finalizing"          // 正在签发/下载
	OpActive             OperationStatus = "active"              // 完成（终态）
	OpFailed             OperationStatus = "failed"              // 失败（终态）
)

// Terminal 报告该阶段是否为终态（不再更新）。
func (s OperationStatus) Terminal() bool {
	return s == OpActive || s == OpFailed
}

// OperationSteps 是前端 stepper 的阶段顺序，供按序渲染进度条。
var OperationSteps = []OperationStatus{
	OpQueued,
	OpAccountReady,
	OpOrderCreated,
	OpChallengePresented,
	OpChallengeValidated,
	OpFinalizing,
	OpActive,
}

// CertificateOperation 是一次签发/续期过程的进度记录。
// 与 Certificate 分离：进行中的操作无可用证书材料，绝不进证书表。
type CertificateOperation struct {
	ID         uuid.UUID       `json:"id"`
	Hostname   string          `json:"hostname"`
	DomainID   *uuid.UUID      `json:"domain_id,omitempty"`
	Action     string          `json:"action"` // issue / renew
	Status     OperationStatus `json:"status"`
	Message    string          `json:"message,omitempty"`
	Error      string          `json:"error,omitempty"`
	StartedAt  time.Time       `json:"started_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
}
