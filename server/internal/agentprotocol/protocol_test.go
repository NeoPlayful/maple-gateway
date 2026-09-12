package agentprotocol

import "testing"

func TestEnvelopeRoundTrip(t *testing.T) {
	env, err := New(TypeTaskExecute, "req-1", TaskExecutePayload{
		TaskID: "task-1", Action: ActionContainerStart,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if env.Type != TypeTaskExecute || env.RequestID != "req-1" || env.Timestamp == 0 {
		t.Fatalf("envelope header wrong: %+v", env)
	}
	var p TaskExecutePayload
	if err := env.DecodePayload(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.TaskID != "task-1" || p.Action != ActionContainerStart {
		t.Fatalf("payload mismatch: %+v", p)
	}
}

func TestNilPayloadOmitted(t *testing.T) {
	env, err := New(TypeHeartbeat, "", nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if env.Payload != nil {
		t.Fatalf("nil payload should stay nil, got %s", env.Payload)
	}
}

func TestDecodeEmptyPayloadIsNoop(t *testing.T) {
	env := Envelope{Type: TypeHeartbeat}
	var p HeartbeatPayload
	if err := env.DecodePayload(&p); err != nil {
		t.Fatalf("decode empty: %v", err)
	}
}

func TestActionWhitelist(t *testing.T) {
	if !IsAllowedAction(ActionContainerStart) {
		t.Fatal("container.start should be allowed")
	}
	if !IsAllowedAction(ActionApplicationDeploy) {
		t.Fatal("application.deploy should be allowed")
	}
	if IsAllowedAction("shell.exec") {
		t.Fatal("shell.exec must not be allowed")
	}
	for _, f := range []string{"shell.exec", "host.exec", "command.exec"} {
		if !IsForbiddenAction(f) {
			t.Fatalf("%s should be forbidden", f)
		}
	}
}
