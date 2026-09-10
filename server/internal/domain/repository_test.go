package domain

import (
	"testing"

	"github.com/google/uuid"
)

// serviceIDMutation 的 service_id 三态覆盖，含"仅更新其它字段时不得清空绑定"的回归点。
func TestServiceIDMutation(t *testing.T) {
	svc := uuid.New()
	other := uuid.New()

	t.Run("设置绑定", func(t *testing.T) {
		set, clear := serviceIDMutation(Update{ServiceID: &svc})
		if set == nil || *set != svc || clear {
			t.Fatalf("expected set=%v clear=false, got set=%v clear=%v", svc, set, clear)
		}
	})

	t.Run("显式清空", func(t *testing.T) {
		set, clear := serviceIDMutation(Update{ClearServiceID: true})
		if set != nil || !clear {
			t.Fatalf("expected set=nil clear=true, got set=%v clear=%v", set, clear)
		}
	})

	t.Run("两者皆否=保持不变(回归:证书联动/enable-disable 不得清空)", func(t *testing.T) {
		// 仅带 TLS 联动字段的部分更新，历史 bug 会误清 service_id。
		set, clear := serviceIDMutation(Update{TLSMode: strptr("manual")})
		if set != nil || clear {
			t.Fatalf("partial update must not touch service_id, got set=%v clear=%v", set, clear)
		}
	})

	t.Run("设置优先于清空标记", func(t *testing.T) {
		set, clear := serviceIDMutation(Update{ServiceID: &other, ClearServiceID: true})
		if set == nil || *set != other || clear {
			t.Fatalf("set should win, got set=%v clear=%v", set, clear)
		}
	})
}

func strptr(s string) *string { return &s }
