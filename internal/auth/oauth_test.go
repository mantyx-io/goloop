package auth

import (
	"encoding/json"
	"testing"
)

func TestFlexIntUnmarshal(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{`5`, 5},
		{`"5"`, 5},
		{`5.0`, 5},
	}
	for _, tc := range tests {
		var got flexInt
		if err := json.Unmarshal([]byte(tc.input), &got); err != nil {
			t.Fatalf("input %s: %v", tc.input, err)
		}
		if int(got) != tc.want {
			t.Fatalf("input %s: got %d want %d", tc.input, got, tc.want)
		}
	}
}
