// Package delay は送信ディレイ（遅延送信）のバックグラウンド処理と配送実行を提供する。
// release_at を過ぎた保留メールを自動配送し、Web UI からの即時送信・取消も同じ配送経路を使う。
package delay

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/reinject"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
	"github.com/koizumib/mailshield/services/api-server/internal/storage"
)

// Service は遅延送信の自動配送ワーカーと配送実行を担う。
type Service struct {
	repo       repository.Repository
	emlStorage storage.EMLStorage
	reinjector *reinject.Reinjector
}

// New は Service を生成する。
func New(repo repository.Repository, emlStorage storage.EMLStorage, reinjector *reinject.Reinjector) *Service {
	return &Service{repo: repo, emlStorage: emlStorage, reinjector: reinjector}
}

// RunReleaser は release_at を過ぎた保留メールを定期的に配送するループを起動する。
// ctx がキャンセルされると停止する。
func (s *Service) RunReleaser(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.releaseDue(ctx)
		}
	}
}

// releaseDue は期限到来分を CAS で取り出して配送する。
func (s *Service) releaseDue(ctx context.Context) {
	due, err := s.repo.ListDueDelayedReleases(ctx)
	if err != nil {
		slog.Error("期限到来の遅延送信取得失敗", "error", err)
		return
	}
	for _, rel := range due {
		if err := s.Release(ctx, rel.ID, nil); err != nil {
			slog.Warn("遅延送信の自動配送失敗（次回サイクルで再試行）",
				"delayed_release_id", rel.ID, "message_id", rel.MessageID, "error", err)
		}
	}
}

// Release は遅延送信を配送する。decidedBy が非 nil の場合は即時送信（ユーザー操作）、
// nil の場合は自動配送。CAS でクレームに勝った 1 回だけが実際に配送する（二重配送防止）。
// クレームに負けた場合（他が先に配送・取消済み）は nil を返す（呼び出し元でエラー扱いしない）。
func (s *Service) Release(ctx context.Context, id string, decidedBy *string) error {
	rel, err := s.repo.GetDelayedRelease(ctx, id)
	if err != nil {
		return fmt.Errorf("遅延送信取得失敗 (id=%s): %w", id, err)
	}
	if rel == nil {
		return fmt.Errorf("遅延送信が見つかりません (id=%s)", id)
	}

	// 先に released をクレーム（pending の場合のみ成功）。二重配送を防ぐ。
	claimed, err := s.repo.ClaimDelayedRelease(ctx, id, domain.DelayedReleased, decidedBy)
	if err != nil {
		return err
	}
	if !claimed {
		slog.Info("遅延送信は既に処理済み（配送スキップ）", "delayed_release_id", id)
		return nil
	}

	msg, err := s.repo.GetMessage(ctx, rel.MessageID)
	if err != nil || msg == nil {
		s.rollback(ctx, id)
		return fmt.Errorf("メール取得失敗 (message_id=%s): %w", rel.MessageID, err)
	}

	// 変換後 EML を優先、なければ原本 EML を使用
	emlPath := msg.Message.EMLPath
	if msg.Message.ProcessedEMLPath != nil && *msg.Message.ProcessedEMLPath != "" {
		emlPath = *msg.Message.ProcessedEMLPath
	}
	eml, err := s.emlStorage.GetEML(ctx, emlPath)
	if err != nil {
		s.rollback(ctx, id)
		return fmt.Errorf("EML 取得失敗 (path=%s): %w", emlPath, err)
	}

	if err := s.reinjector.Send(ctx, msg.Message.FromAddress, msg.Message.ToAddresses, eml); err != nil {
		s.rollback(ctx, id)
		return fmt.Errorf("再インジェクト失敗: %w", err)
	}

	if err := s.repo.UpdateMessageStatus(ctx, rel.MessageID, domain.StatusDelivered); err != nil {
		slog.Warn("配送後のメールステータス更新失敗（続行）", "message_id", rel.MessageID, "error", err)
	}
	slog.Info("遅延送信メール配送完了", "delayed_release_id", id, "message_id", rel.MessageID, "auto", decidedBy == nil)
	return nil
}

// rollback は配送失敗時に遅延送信を pending へ戻し、次回サイクルで再試行できるようにする。
func (s *Service) rollback(ctx context.Context, id string) {
	if err := s.repo.UpdateDelayedReleaseStatus(ctx, id, domain.DelayedPending); err != nil {
		slog.Error("遅延送信ステータスのロールバック失敗（手動対応が必要）", "delayed_release_id", id, "error", err)
	}
}

// Cancel は遅延送信を取り消す（配送しない）。CAS でクレームできた場合のみ true を返す。
func (s *Service) Cancel(ctx context.Context, id string, decidedBy string) (bool, error) {
	claimed, err := s.repo.ClaimDelayedRelease(ctx, id, domain.DelayedCancelled, &decidedBy)
	if err != nil {
		return false, err
	}
	if !claimed {
		return false, nil
	}
	rel, err := s.repo.GetDelayedRelease(ctx, id)
	if err == nil && rel != nil {
		if err := s.repo.UpdateMessageStatus(ctx, rel.MessageID, domain.StatusRejected); err != nil {
			slog.Warn("取消後のメールステータス更新失敗（続行）", "message_id", rel.MessageID, "error", err)
		}
	}
	slog.Info("遅延送信を取消", "delayed_release_id", id, "decided_by", decidedBy)
	return true, nil
}
