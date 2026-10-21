package notify

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/events"
)

type inbox map[string]bool

func (i inbox) MarkProcessed(_ context.Context, c, id string) (bool, error) {
	if i[c+id] {
		return false, nil
	}
	i[c+id] = true
	return true, nil
}

func TestNotifiesOnDecisionOnceDespiteRedelivery(t *testing.T) {
	var logs bytes.Buffer
	s := &Service{Log: slog.New(slog.NewJSONHandler(&logs, nil)), Inbox: inbox{}}
	before := &domain.Application{ID: "a", Status: domain.StatusScoring}
	after := &domain.Application{ID: "a", Number: "КР-000001", Status: domain.StatusApproved, StatusChangedAt: time.Now()}
	e := events.FromChange(before, after, "rid")[0]
	for i := 0; i < 2; i++ {
		if err := s.Handle(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(logs.String(), "notification sent"); n != 1 {
		t.Fatalf("уведомление должно уйти один раз, ушло %d", n)
	}
	// промежуточные статусы конвейера не уведомляют
	logs.Reset()
	mid := events.FromChange(&domain.Application{ID: "b", Status: domain.StatusSubmitted}, &domain.Application{ID: "b", Status: domain.StatusDataCheck}, "")[0]
	_ = s.Handle(context.Background(), mid)
	if logs.Len() != 0 {
		t.Fatal("no notification for data_check")
	}
}
