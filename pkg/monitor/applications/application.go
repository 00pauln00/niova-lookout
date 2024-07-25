package applications

import (
	"encoding/json"
	"time"
)

type Application interface {
}

type Histogram struct {
	Num1       int `json:"1,omitempty"`
	Num2       int `json:"2,omitempty"`
	Num4       int `json:"4,omitempty"`
	Num8       int `json:"8,omitempty"`
	Num16      int `json:"16,omitempty"`
	Num32      int `json:"32,omitempty"`
	Num64      int `json:"64,omitempty"`
	Num128     int `json:"128,omitempty"`
	Num256     int `json:"256,omitempty"`
	Num512     int `json:"512,omitempty"`
	Num1024    int `json:"1024,omitempty"`
	Num2048    int `json:"2048,omitempty"`
	Num4096    int `json:"4096,omitempty"`
	Num8192    int `json:"8192,omitempty"`
	Num16384   int `json:"16384,omitempty"`
	Num32768   int `json:"32768,omitempty"`
	Num65536   int `json:"65536,omitempty"`
	Num131072  int `json:"131072,omitempty"`
	Num262144  int `json:"262144,omitempty"`
	Num524288  int `json:"524288,omitempty"`
	Num1048576 int `json:"1048576,omitempty"`
}

type Time struct {
	WrappedTime time.Time `json:"time"`
}

// custom UnmarshalJSON method used for handling various timestamp formats.
func (t *Time) UnmarshalJSON(data []byte) error {
	var err error

	data = chompQuotes(data)

	if err = json.Unmarshal(data, t.WrappedTime); err == nil {
		return nil
	}
	const layout = "Mon Jan 02 15:04:05 MST 2006"

	t.WrappedTime, err = time.Parse(layout, string(data))

	return err
}

func chompQuotes(data []byte) []byte {
	s := string(data)

	// Check for quotes
	if len(s) > 0 {
		if s[0] == '"' {
			s = s[1:]
		}
		if s[len(s)-1] == '"' {
			s = s[:len(s)-1]
		}
	}

	return []byte(s)
}
