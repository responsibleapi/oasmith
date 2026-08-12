package publicapi

import (
	"encoding/json"
	"fmt"
)

type CompletedResult struct {
	Status string `json:"status"`
	Thing  Thing  `json:"thing"`
}

type CreateThing struct {
	Name string `json:"name"`
}

// Event is generated from an OpenAPI oneOf schema.
// oneOf variant: ProgressEvent
// oneOf variant: TerminalEvent
type Event struct {
	ProgressEvent *ProgressEvent
	TerminalEvent *TerminalEvent
}

func (dst *Event) UnmarshalJSON(data []byte) error {
	var discriminator struct {
		Value string `json:"kind"`
	}
	if err := json.Unmarshal(data, &discriminator); err != nil {
		return fmt.Errorf("decode Event discriminator: %w", err)
	}
	switch discriminator.Value {
	case "progress":
		var decoded ProgressEvent
		if err := json.Unmarshal(data, &decoded); err != nil {
			return fmt.Errorf("decode Event as ProgressEvent: %w", err)
		}
		*dst = Event{ProgressEvent: &decoded}
		return nil
	case "terminal":
		var decoded TerminalEvent
		if err := json.Unmarshal(data, &decoded); err != nil {
			return fmt.Errorf("decode Event as TerminalEvent: %w", err)
		}
		*dst = Event{TerminalEvent: &decoded}
		return nil
	default:
		return fmt.Errorf("unsupported Event discriminator %q", discriminator.Value)
	}
}

func (src Event) MarshalJSON() ([]byte, error) {
	matchCount := 0
	if src.ProgressEvent != nil {
		matchCount++
	}
	if src.TerminalEvent != nil {
		matchCount++
	}
	if matchCount != 1 {
		return nil, fmt.Errorf("Event must contain exactly one variant, got %d", matchCount)
	}
	if src.ProgressEvent != nil {
		return json.Marshal(src.ProgressEvent)
	}
	if src.TerminalEvent != nil {
		return json.Marshal(src.TerminalEvent)
	}
	return nil, fmt.Errorf("Event has no variant")
}

func (src Event) GetActualInstance() any {
	if src.ProgressEvent != nil {
		return src.ProgressEvent
	}
	if src.TerminalEvent != nil {
		return src.TerminalEvent
	}
	return nil
}

func ProgressEventAsEvent(v ProgressEvent) Event {
	return Event{ProgressEvent: &v}
}

func TerminalEventAsEvent(v TerminalEvent) Event {
	return Event{TerminalEvent: &v}
}

type FailedResult struct {
	Message string `json:"message"`
	Status  string `json:"status"`
}

type Problem struct {
	Message string `json:"message"`
}

type ProgressEvent struct {
	Kind    string `json:"kind"`
	Percent int32  `json:"percent"`
}

type TerminalEvent struct {
	Kind   string         `json:"kind"`
	Result TerminalResult `json:"result"`
}

// TerminalResult is generated from an OpenAPI oneOf schema.
// oneOf variant: CompletedResult
// oneOf variant: FailedResult
type TerminalResult struct {
	CompletedResult *CompletedResult
	FailedResult    *FailedResult
}

func (dst *TerminalResult) UnmarshalJSON(data []byte) error {
	var discriminator struct {
		Value string `json:"status"`
	}
	if err := json.Unmarshal(data, &discriminator); err != nil {
		return fmt.Errorf("decode TerminalResult discriminator: %w", err)
	}
	switch discriminator.Value {
	case "completed":
		var decoded CompletedResult
		if err := json.Unmarshal(data, &decoded); err != nil {
			return fmt.Errorf("decode TerminalResult as CompletedResult: %w", err)
		}
		*dst = TerminalResult{CompletedResult: &decoded}
		return nil
	case "failed":
		var decoded FailedResult
		if err := json.Unmarshal(data, &decoded); err != nil {
			return fmt.Errorf("decode TerminalResult as FailedResult: %w", err)
		}
		*dst = TerminalResult{FailedResult: &decoded}
		return nil
	default:
		return fmt.Errorf("unsupported TerminalResult discriminator %q", discriminator.Value)
	}
}

func (src TerminalResult) MarshalJSON() ([]byte, error) {
	matchCount := 0
	if src.CompletedResult != nil {
		matchCount++
	}
	if src.FailedResult != nil {
		matchCount++
	}
	if matchCount != 1 {
		return nil, fmt.Errorf("TerminalResult must contain exactly one variant, got %d", matchCount)
	}
	if src.CompletedResult != nil {
		return json.Marshal(src.CompletedResult)
	}
	if src.FailedResult != nil {
		return json.Marshal(src.FailedResult)
	}
	return nil, fmt.Errorf("TerminalResult has no variant")
}

func (src TerminalResult) GetActualInstance() any {
	if src.CompletedResult != nil {
		return src.CompletedResult
	}
	if src.FailedResult != nil {
		return src.FailedResult
	}
	return nil
}

func CompletedResultAsTerminalResult(v CompletedResult) TerminalResult {
	return TerminalResult{CompletedResult: &v}
}

func FailedResultAsTerminalResult(v FailedResult) TerminalResult {
	return TerminalResult{FailedResult: &v}
}

type Thing struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}
