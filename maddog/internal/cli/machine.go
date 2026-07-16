package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"maddog/internal/event"
	"maddog/internal/eventwire"
)

const machineSchemaVersion = "1"

type outputFormat string

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			*s = append(*s, p)
		}
	}
	return nil
}

const (
	outputText       outputFormat = "text"
	outputJSON       outputFormat = "json"
	outputStreamJSON outputFormat = "stream-json"
)

func parseOutputFormat(s string) (outputFormat, error) {
	switch outputFormat(strings.TrimSpace(s)) {
	case "", outputText:
		return outputText, nil
	case outputJSON:
		return outputJSON, nil
	case outputStreamJSON:
		return outputStreamJSON, nil
	default:
		return "", fmt.Errorf("invalid --output-format %q (want text, json, or stream-json)", s)
	}
}

type machineResult struct {
	SchemaVersion string            `json:"schema_version"`
	OK            bool              `json:"ok"`
	Events        []eventwire.Event `json:"events,omitempty"`
	Text          string            `json:"text,omitempty"`
	Error         string            `json:"error,omitempty"`
}

type machineSink struct {
	mu     sync.Mutex
	out    io.Writer
	format outputFormat
	events []eventwire.Event
	text   strings.Builder
}

func newMachineSink(out io.Writer, format outputFormat) *machineSink {
	return &machineSink{out: out, format: format}
}

func (s *machineSink) Emit(e event.Event) {
	w := eventwire.ToWire(e)
	s.mu.Lock()
	defer s.mu.Unlock()
	if w.Text != "" {
		s.text.WriteString(w.Text)
	}
	if s.format == outputStreamJSON {
		_ = json.NewEncoder(s.out).Encode(w)
		return
	}
	s.events = append(s.events, w)
}

func (s *machineSink) Result(runErr error) machineResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := machineResult{SchemaVersion: machineSchemaVersion, OK: runErr == nil, Text: s.text.String()}
	if s.format == outputJSON {
		r.Events = append([]eventwire.Event(nil), s.events...)
	}
	if runErr != nil {
		r.Error = runErr.Error()
	}
	return r
}

func (s *machineSink) Finish(runErr error) error {
	if s.format == outputText {
		return nil
	}
	return json.NewEncoder(s.out).Encode(s.Result(runErr))
}
