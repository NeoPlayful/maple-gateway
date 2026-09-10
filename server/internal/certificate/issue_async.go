package certificate

import (
	"context"
	"errors"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/certificate/acme"
	"github.com/NeoPlayful/maple-gateway/server/internal/router"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// IssueAsync 受理一次签发：去重 → 建 queued 记录 → 立即返回 → 后台执行。
//
// 未启用异步运行时（无操作仓储/后台上下文）时退化为同步签发：成功返回 active 视图，
// 失败直接返回错误——保证未配置时管理面行为与旧版一致。
func (s *Service) IssueAsync(ctx context.Context, hostname string, domainID *uuid.UUID, source Source, challenge ChallengeType) (*CertificateOperation, error) {
	h, err := router.NormalizeHost(hostname)
	if err != nil {
		return nil, pkg.ErrValidation("域名格式无效")
	}
	// 来源可用性预检：未启用时提前失败，不建无意义的记录。
	if _, err := s.providers.For(source); err != nil {
		return nil, pkg.ErrValidation("证书来源未启用: " + string(source))
	}

	// 退化路径：无异步基础设施，同步执行。
	if s.opRepo == nil || s.jobCtx == nil {
		if _, ierr := s.issueCore(ctx, h, domainID, source, challenge, nil); ierr != nil {
			return nil, mapProviderErr(ierr)
		}
		return &CertificateOperation{
			Hostname: h, DomainID: domainID, Action: "issue", Status: OpActive, Message: "签发成功",
		}, nil
	}

	// 去重：同域名已有进行中操作则直接复用（避免重复下单触发 CA 限流）。
	if existing, gerr := s.opRepo.ActiveByHostname(ctx, h); gerr == nil {
		return existing, nil
	}

	created, err := s.opRepo.Create(ctx, &CertificateOperation{
		Hostname: h, DomainID: domainID, Action: "issue", Status: OpQueued, Message: "已受理，准备签发",
	})
	if err != nil {
		// 并发下命中"进行中唯一"部分索引 → 回查返回已有记录。
		if pkg.ErrCode(err) == pkg.CodeConflict {
			if existing, gerr := s.opRepo.ActiveByHostname(ctx, h); gerr == nil {
				return existing, nil
			}
		}
		return nil, err
	}

	// 域名证书状态置 pending（展示进行中）。
	s.syncDomainTLS(ctx, domainID, "managed", StatusPending)
	s.runIssueJob(created.ID, h, domainID, source, challenge)
	return created, nil
}

// runIssueJob 在后台执行签发并逐阶段更新操作记录。
// 使用独立的后台上下文（非请求 ctx——请求返回即取消，不能用于后台任务）。
func (s *Service) runIssueJob(opID uuid.UUID, hostname string, domainID *uuid.UUID, source Source, challenge ChallengeType) {
	s.bgWG.Add(1)
	go func() {
		defer s.bgWG.Done()
		ctx := s.jobCtx
		if s.jobTimeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, s.jobTimeout)
			defer cancel()
		}

		// 进度回调：每次阶段转换写库（独立短超时，写失败不阻断签发流程）。
		// lastMsg 记录最后到达阶段的说明，供失败时回显（进度回调与签发同 goroutine，无竞态）。
		var lastMsg string
		progress := func(step, msg string) {
			lastMsg = msg
			wctx, cancel := context.WithTimeout(s.jobCtx, 5*time.Second)
			defer cancel()
			if err := s.opRepo.UpdateStep(wctx, opID, OperationStatus(step), msg); err != nil {
				s.log.Warn("update certificate operation step failed",
					zap.String("op_id", opID.String()), zap.String("step", step), zap.Error(err))
			}
		}

		_, err := s.issueCore(ctx, hostname, domainID, source, challenge, progress)

		fctx, cancel := context.WithTimeout(s.jobCtx, 5*time.Second)
		defer cancel()
		if err != nil {
			msg := progressErrorText(err)
			finishMsg := "签发失败"
			if lastMsg != "" {
				finishMsg = "在「" + lastMsg + "」阶段失败"
			}
			if ferr := s.opRepo.Finish(fctx, opID, OpFailed, finishMsg, msg); ferr != nil {
				s.log.Warn("finish certificate operation (failed) failed",
					zap.String("op_id", opID.String()), zap.Error(ferr))
			}
			s.syncDomainAfterIssueFailure(fctx, domainID, hostname)
			s.incMetric("maple_acme_issue_total", "failed")
			s.log.Warn("certificate issue failed",
				zap.String("hostname", hostname), zap.String("reason", msg), zap.Error(err))
			return
		}
		if ferr := s.opRepo.Finish(fctx, opID, OpActive, "签发成功", ""); ferr != nil {
			s.log.Warn("finish certificate operation (active) failed",
				zap.String("op_id", opID.String()), zap.Error(ferr))
		}
		s.incMetric("maple_acme_issue_total", "success")
		s.log.Info("certificate issued", zap.String("hostname", hostname))
	}()
}

// syncDomainAfterIssueFailure 签发失败后联动域名证书状态：
// 若该域名已有可用证书则不降级（旧证继续服务），仅无证时置 error。
func (s *Service) syncDomainAfterIssueFailure(ctx context.Context, domainID *uuid.UUID, hostname string) {
	if s.domainRepo == nil || domainID == nil {
		return
	}
	if _, err := s.repo.GetByHostname(ctx, hostname); err == nil {
		return
	}
	s.syncDomainTLS(ctx, domainID, "managed", StatusError)
}

// progressErrorText 把签发失败原因转成可回显给管理员的中文文案（按类型分类）。
func progressErrorText(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, acme.ErrRateLimited):
		return "CA 限流，请稍后重试"
	case errors.Is(err, acme.ErrAccountInvalid):
		return "CA 账户无效，请检查 ACME 配置"
	case errors.Is(err, acme.ErrChallengeFailed):
		return "域名验证失败：请确认域名已解析到本网关，且 80 端口可从公网访问"
	case errors.Is(err, acme.ErrTransient):
		return "网络或 CA 暂时不可用，请稍后重试"
	}
	var ae *pkg.AppError
	if errors.As(err, &ae) {
		return ae.Message
	}
	return err.Error()
}
