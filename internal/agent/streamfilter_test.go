package agent

import (
	"bytes"
	"testing"
)

func TestStreamFilterWriter_ExtractsAssistantText(t *testing.T) {
	var buf bytes.Buffer
	w := newStreamFilterWriter(&buf)

	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello world"}]}}` + "\n"
	w.Write([]byte(line))

	if got := buf.String(); got != "Hello world" {
		t.Errorf("expected %q, got %q", "Hello world", got)
	}
}

func TestStreamFilterWriter_PassesThroughPlainText(t *testing.T) {
	var buf bytes.Buffer
	w := newStreamFilterWriter(&buf)

	w.Write([]byte("plain text output\n"))

	if got := buf.String(); got != "plain text output\n" {
		t.Errorf("expected %q, got %q", "plain text output\n", got)
	}
}

func TestStreamFilterWriter_IgnoresSystemEvents(t *testing.T) {
	var buf bytes.Buffer
	w := newStreamFilterWriter(&buf)

	w.Write([]byte(`{"type":"system","subtype":"init","cwd":"/tmp"}` + "\n"))

	if got := buf.String(); got != "" {
		t.Errorf("expected empty output for system event, got %q", got)
	}
}

func TestStreamFilterWriter_HandlesPartialLines(t *testing.T) {
	var buf bytes.Buffer
	w := newStreamFilterWriter(&buf)

	w.Write([]byte("first part "))
	w.Write([]byte("second part\n"))

	if got := buf.String(); got != "first part second part\n" {
		t.Errorf("expected %q, got %q", "first part second part\n", got)
	}
}

func TestStreamFilterWriter_ExtractsResultText(t *testing.T) {
	var buf bytes.Buffer
	w := newStreamFilterWriter(&buf)

	line := `{"type":"result","subtype":"success","result":"Final answer text"}` + "\n"
	w.Write([]byte(line))

	if got := buf.String(); got != "Final answer text" {
		t.Errorf("expected %q, got %q", "Final answer text", got)
	}
}

func TestStreamFilterWriter_MultipleAssistantMessages(t *testing.T) {
	var buf bytes.Buffer
	w := newStreamFilterWriter(&buf)

	w.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"Part 1"}]}}` + "\n"))
	w.Write([]byte(`{"type":"system","subtype":"tool_use"}` + "\n"))
	w.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"Part 2"}]}}` + "\n"))

	if got := buf.String(); got != "Part 1Part 2" {
		t.Errorf("expected %q, got %q", "Part 1Part 2", got)
	}
}
