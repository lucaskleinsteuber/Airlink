package engine

import (
	"context"
	"errors"
	"testing"
)

func TestLateBackgroundCallbacksCannotUndoExplicitPause(t *testing.T) {
	e, _, _ := testEngine(t)
	tr := incoming(t, e, "payload", []byte("x"))
	if err := e.Pause(tr.ID); err != nil {
		t.Fatal(err)
	}
	for _, callback := range []func() error{func() error { return e.MarkReconnecting(tr.ID, "late network failure") }, func() error { return e.Fail(tr.ID, "late source error") }} {
		if err := callback(); !errors.Is(err, ErrConflict) {
			t.Fatal("background callback overrode pause", err)
		}
		got, _ := e.Get(tr.ID)
		if got.State != "paused" {
			t.Fatal("pause was lost", got.State)
		}
	}
	if err := e.Resume(tr.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.MarkReconnecting(tr.ID, "actual disconnection"); err != nil {
		t.Fatal("legitimate active reconnect rejected", err)
	}
}

func TestFailedStateSaveDoesNotCommitMemoryOrCancelIO(t *testing.T) {
	for _, action := range []string{"pause", "cancel", "reconnecting", "failed"} {
		t.Run(action, func(t *testing.T) {
			e, data, receive := testEngine(t)
			tr := incoming(t, e, "payload", []byte("x"))
			opCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			op := &verificationOperation{ctx: opCtx, cancel: cancel}
			sourceCtx, sourceCancel := context.WithCancel(context.Background())
			defer sourceCancel()
			e.mu.Lock()
			e.verifications[tr.ID] = op
			e.sourceOperations[tr.ID] = map[string]context.CancelFunc{"fixture": sourceCancel}
			e.mu.Unlock()
			if _, err := e.db.Exec("CREATE TRIGGER fail_state_write BEFORE UPDATE ON transfers BEGIN SELECT RAISE(ABORT,'simulated persistence failure'); END"); err != nil {
				t.Fatal(err)
			}
			var err error
			switch action {
			case "pause":
				err = e.Pause(tr.ID)
			case "cancel":
				err = e.Cancel(tr.ID)
			case "reconnecting":
				err = e.MarkReconnecting(tr.ID, "network")
			case "failed":
				err = e.Fail(tr.ID, "failure")
			}
			if err == nil {
				t.Fatal("state operation unexpectedly survived SQLite fault")
			}
			got, _ := e.Get(tr.ID)
			if got.State != "running" || got.Error != "" {
				t.Fatal("failed durable state leaked into memory", got.State, got.Error)
			}
			if opCtx.Err() != nil || sourceCtx.Err() != nil {
				t.Fatal("failed state operation cancelled live I/O")
			}
			if _, err = e.db.Exec("DROP TRIGGER fail_state_write"); err != nil {
				t.Fatal(err)
			}
			if err = e.Close(); err != nil {
				t.Fatal(err)
			}
			e, err = New(data, receive)
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			got, _ = e.Get(tr.ID)
			if got.State != "reconnecting" {
				t.Fatal("restart resurrected an acknowledged state change", got.State)
			}
		})
	}
}

func TestSuccessfulPauseAndCancelRemainDurableAcrossRestart(t *testing.T) {
	for _, action := range []string{"paused", "cancelled"} {
		t.Run(action, func(t *testing.T) {
			e, data, receive := testEngine(t)
			tr := incoming(t, e, "payload", []byte("x"))
			var err error
			if action == "paused" {
				err = e.Pause(tr.ID)
			} else {
				err = e.Cancel(tr.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			e.Close()
			e, err = New(data, receive)
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			got, _ := e.Get(tr.ID)
			if got.State != action {
				t.Fatal("successful state was not durable", got.State)
			}
		})
	}
}
