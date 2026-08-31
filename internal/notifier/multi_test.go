package notifier

import (
	"context"
	"strings"
	"testing"

	"github.com/user/daily-info-agent/pkg/models"
)

// stubSender is a scripted Sender for Multi tests.
type stubSender struct {
	name      string
	digestErr error
	alertErr  error

	digestCalls int
	alertCalls  int
}

func (s *stubSender) Name() string { return s.name }

func (s *stubSender) SendDailySummary(ctx context.Context, articles []models.ProcessedArticle, result models.RunResult) error {
	s.digestCalls++
	return s.digestErr
}

func (s *stubSender) SendAlert(ctx context.Context, message string) error {
	s.alertCalls++
	return s.alertErr
}

func TestMulti_FansOutToAllChannels(t *testing.T) {
	a := &stubSender{name: "a"}
	b := &stubSender{name: "b"}
	m := NewMulti(nil, a, b)

	if err := m.SendDailySummary(context.Background(), nil, models.RunResult{}); err != nil {
		t.Fatalf("SendDailySummary: %v", err)
	}
	if a.digestCalls != 1 || b.digestCalls != 1 {
		t.Errorf("digest calls: a=%d b=%d, want 1/1", a.digestCalls, b.digestCalls)
	}

	if err := m.SendAlert(context.Background(), "x"); err != nil {
		t.Fatalf("SendAlert: %v", err)
	}
	if a.alertCalls != 1 || b.alertCalls != 1 {
		t.Errorf("alert calls: a=%d b=%d, want 1/1", a.alertCalls, b.alertCalls)
	}
}

func TestMulti_PartialFailure_StillSucceeds(t *testing.T) {
	a := &stubSender{name: "a", digestErr: context.Canceled, alertErr: context.Canceled}
	b := &stubSender{name: "b"}
	m := NewMulti(nil, a, b)

	if err := m.SendDailySummary(context.Background(), nil, models.RunResult{}); err != nil {
		t.Errorf("partial digest failure should not error, got %v", err)
	}
	if err := m.SendAlert(context.Background(), "x"); err != nil {
		t.Errorf("partial alert failure should not error, got %v", err)
	}
	if b.digestCalls != 1 || b.alertCalls != 1 {
		t.Errorf("healthy channel skipped: digest=%d alert=%d", b.digestCalls, b.alertCalls)
	}
}

func TestMulti_AllFail_ReturnsJoinedError(t *testing.T) {
	a := &stubSender{name: "a", digestErr: context.DeadlineExceeded, alertErr: context.DeadlineExceeded}
	b := &stubSender{name: "b", digestErr: context.Canceled, alertErr: context.Canceled}
	m := NewMulti(nil, a, b)

	err := m.SendDailySummary(context.Background(), nil, models.RunResult{})
	if err == nil {
		t.Fatal("expected error when all channels fail")
	}
	if !strings.Contains(err.Error(), "a") || !strings.Contains(err.Error(), "b") {
		t.Errorf("joined error should name both channels: %v", err)
	}

	err = m.SendAlert(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error when all channels fail")
	}
}

func TestMulti_Empty_IsNoopSuccess(t *testing.T) {
	m := NewMulti(nil)
	if m.Len() != 0 {
		t.Errorf("Len = %d, want 0", m.Len())
	}
	if err := m.SendDailySummary(context.Background(), nil, models.RunResult{}); err != nil {
		t.Errorf("empty multi digest: %v", err)
	}
	if err := m.SendAlert(context.Background(), "x"); err != nil {
		t.Errorf("empty multi alert: %v", err)
	}
}

func TestMulti_NilSenderSkipped(t *testing.T) {
	a := &stubSender{name: "a"}
	m := NewMulti(nil, nil, a, nil)
	if m.Len() != 3 {
		t.Errorf("Len = %d, want 3", m.Len())
	}
	if err := m.SendAlert(context.Background(), "x"); err != nil {
		t.Fatalf("SendAlert: %v", err)
	}
	if a.alertCalls != 1 {
		t.Errorf("alert calls = %d, want 1", a.alertCalls)
	}
}

func TestMulti_ImplementsDigestSender(t *testing.T) {
	// The scheduler depends on this interface; compile-time check.
	var _ interface {
		SendDailySummary(ctx context.Context, articles []models.ProcessedArticle, result models.RunResult) error
	} = NewMulti(nil)
}
