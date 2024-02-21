// © Copyright Apptio, an IBM Corp. 2024, 2025

package measurement

import (
	"fmt"
)

// Measurement represents a single set of data
type Measurement struct {
	Name      string            `json:"name,omitempty"`
	Metrics   map[string]uint64 `json:"metrics,omitempty"`
	Tags      map[string]string `json:"tags,omitempty"`
	Timestamp int64             `json:"ts,omitempty"`
	Value     float64           `json:"value,omitempty"`
	Values    map[string]string `json:"values,omitempty"`
	Errors    []ErrorDetail     `json:"errors,omitempty"`
}

// ErrorDetail represents a detailed error message
type ErrorDetail struct {
	Name    string `json:"name,omitempty"`
	Message string `json:"message,omitempty"`
	Type    string `json:"type,omitempty"`
}

func (m Measurement) String() string {
	return fmt.Sprintf("%v:%.2f [%v] [%v] [%v] [%+v] @ %v ",
		m.Name, m.Value, m.Tags, m.Metrics, m.Values, m.Errors, m.Timestamp)
}
