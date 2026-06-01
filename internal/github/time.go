package github

import (
	"strings"
	"time"
)

type githubTime struct {
	time.Time
}

func (t *githubTime) UnmarshalJSON(data []byte) error {
	value := strings.Trim(string(data), `"`)
	if value == "" || value == "null" {
		t.Time = time.Time{}
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return err
	}
	t.Time = parsed
	return nil
}
