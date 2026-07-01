package orchestrator

import "testing"

func TestParseAuditVerdict(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		complete bool
		found    bool
	}{
		{"complete", "All checks pass.\nVERDICT: COMPLETE", true, true},
		{"incomplete", "Missing tests.\nVERDICT: INCOMPLETE — no tests for auth", false, true},
		{"no verdict", "Looks good overall, nothing else to add.", false, false},
		{"markdown wrapped", "Summary here.\n\n**VERDICT: COMPLETE**", true, true},
		{"lowercase", "verdict: incomplete — docs missing", false, true},
		{"last verdict wins", "VERDICT: INCOMPLETE\nAfter re-checking:\nVERDICT: COMPLETE", true, true},
		{"trailing punctuation", "VERDICT: COMPLETE.", true, true},
		{"incomplete not misread as complete", "VERDICT: INCOMPLETE", false, true},
		{"empty", "", false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			complete, found := parseAuditVerdict(tc.text)
			if complete != tc.complete || found != tc.found {
				t.Fatalf("parseAuditVerdict(%q) = (%v, %v), want (%v, %v)",
					tc.text, complete, found, tc.complete, tc.found)
			}
		})
	}
}
