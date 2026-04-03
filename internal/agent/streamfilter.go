package agent

import (
	"bytes"
	"encoding/json"
	"io"
)

// streamFilterWriter is a best-effort filter that extracts text from
// Claude CLI stream-json events. Non-JSON lines pass through unchanged.
// This keeps the runner format-agnostic: if the agent command doesn't
// output stream-json, everything passes through as-is.
type streamFilterWriter struct {
	dst     io.Writer
	lineBuf bytes.Buffer
}

func newStreamFilterWriter(dst io.Writer) *streamFilterWriter {
	return &streamFilterWriter{dst: dst}
}

func (w *streamFilterWriter) Write(p []byte) (int, error) {
	n := len(p)
	w.lineBuf.Write(p)

	for {
		line, err := w.lineBuf.ReadBytes('\n')
		if err != nil {
			// No complete line yet — put the partial back
			w.lineBuf.Write(line)
			break
		}
		w.processLine(line[:len(line)-1]) // strip trailing \n
	}
	return n, nil
}

// Flush processes any remaining partial line in the buffer.
// Call after the upstream writer is done (e.g. after cmd.Wait()).
func (w *streamFilterWriter) Flush() {
	if w.lineBuf.Len() > 0 {
		w.processLine(w.lineBuf.Bytes())
		w.lineBuf.Reset()
	}
}

func (w *streamFilterWriter) processLine(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}

	// Try JSON parse
	var event struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		Message *struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
		Result string `json:"result"`
	}

	if json.Unmarshal(line, &event) != nil {
		// Not JSON — pass through as plain text
		w.dst.Write(line)
		w.dst.Write([]byte("\n"))
		return
	}

	switch event.Type {
	case "assistant":
		if event.Message != nil {
			for _, block := range event.Message.Content {
				if block.Type == "text" && block.Text != "" {
					w.dst.Write([]byte(block.Text))
				}
			}
		}
	case "result":
		if event.Result != "" {
			w.dst.Write([]byte(event.Result))
		}
	// system, tool_use, etc. — silently skip
	}
}
