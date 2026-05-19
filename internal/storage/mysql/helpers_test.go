package mysql

import (
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// Tests for helpers that don't require a live MySQL connection.

func TestIsEphemeralID(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"bd-123", false},
		{"bd-wisp-abc", true},
		{"agent-wisp-x", true},
		{"foo", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			if got := IsEphemeralID(c.id); got != c.want {
				t.Errorf("IsEphemeralID(%q) = %v, want %v", c.id, got, c.want)
			}
		})
	}
}

func TestAllEphemeral(t *testing.T) {
	cases := []struct {
		name string
		ids  []string
		want bool
	}{
		{"empty", nil, false},
		{"all wisps", []string{"bd-wisp-a", "bd-wisp-b"}, true},
		{"mixed", []string{"bd-wisp-a", "bd-1"}, false},
		{"all permanent", []string{"bd-1", "bd-2"}, false},
		{"single wisp", []string{"bd-wisp-a"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := allEphemeral(c.ids); got != c.want {
				t.Errorf("allEphemeral(%v) = %v, want %v", c.ids, got, c.want)
			}
		})
	}
}

func TestIsCrossPrefixDep(t *testing.T) {
	cases := []struct {
		source, target string
		want           bool
	}{
		{"bd-1", "bd-2", false},
		{"bd-1", "pgr-2", true},
		{"bd-wisp-a", "bd-1", false},
		{"a-1", "b-1", true},
	}
	for _, c := range cases {
		t.Run(c.source+"->"+c.target, func(t *testing.T) {
			if got := isCrossPrefixDep(c.source, c.target); got != c.want {
				t.Errorf("isCrossPrefixDep(%q,%q) = %v, want %v", c.source, c.target, got, c.want)
			}
		})
	}
}

func TestBuildSQLInClause(t *testing.T) {
	placeholders, args := buildSQLInClause([]string{"a", "b", "c"})
	if placeholders != "?,?,?" {
		t.Errorf("placeholders = %q, want ?,?,?", placeholders)
	}
	if len(args) != 3 || args[0] != "a" || args[1] != "b" || args[2] != "c" {
		t.Errorf("args = %v, want [a b c]", args)
	}

	placeholders2, args2 := buildSQLInClause(nil)
	if placeholders2 != "" {
		t.Errorf("empty IDs placeholders = %q, want empty", placeholders2)
	}
	if len(args2) != 0 {
		t.Errorf("empty IDs args = %v, want []", args2)
	}
}

func TestWispPrefix(t *testing.T) {
	cases := []struct {
		name         string
		configPrefix string
		issue        *types.Issue
		want         string
	}{
		{"default", "bd", &types.Issue{}, "bd-wisp"},
		{"explicit prefix override", "bd", &types.Issue{PrefixOverride: "agent"}, "agent"},
		{"id prefix", "bd", &types.Issue{IDPrefix: "ts"}, "bd-ts"},
		{"both override and id prefix → override wins", "bd", &types.Issue{PrefixOverride: "agent", IDPrefix: "ts"}, "agent"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := wispPrefix(c.configPrefix, c.issue); got != c.want {
				t.Errorf("wispPrefix(%q, %+v) = %q, want %q", c.configPrefix, c.issue, got, c.want)
			}
		})
	}
}

func TestApplyUpdatesToIssueStruct(t *testing.T) {
	issue := &types.Issue{
		Title:    "old",
		Status:   types.StatusOpen,
		Priority: 3,
	}
	applyUpdatesToIssueStruct(issue, map[string]interface{}{
		"title":      "new",
		"status":     "closed",
		"priority":   1,
		"wisp":       true,
		"no_history": true,
	})
	if issue.Title != "new" {
		t.Errorf("title not updated: got %q", issue.Title)
	}
	if issue.Status != types.StatusClosed {
		t.Errorf("status not updated: got %s", issue.Status)
	}
	if issue.Priority != 1 {
		t.Errorf("priority not updated: got %d", issue.Priority)
	}
	if !issue.Ephemeral {
		t.Errorf("Ephemeral not set")
	}
	if !issue.NoHistory {
		t.Errorf("NoHistory not set")
	}
}

func TestIssueClosedAt_PrefersClosedAt(t *testing.T) {
	stamp := mustParseRFC3339(t, "2026-05-18T12:00:00Z")
	issue := &types.Issue{ClosedAt: &stamp}
	got := issueClosedAt(issue, mustParseRFC3339(t, "2026-01-01T00:00:00Z"))
	if !got.Equal(stamp) {
		t.Errorf("expected ClosedAt=%v, got %v", stamp, got)
	}
}

func TestIssueClosedAt_FallsBackToUpdatedAt(t *testing.T) {
	stamp := mustParseRFC3339(t, "2026-05-18T12:00:00Z")
	issue := &types.Issue{UpdatedAt: stamp}
	got := issueClosedAt(issue, mustParseRFC3339(t, "2026-01-01T00:00:00Z"))
	if !got.Equal(stamp) {
		t.Errorf("expected UpdatedAt=%v, got %v", stamp, got)
	}
}

func TestIssueClosedAt_FallbackWhenAllZero(t *testing.T) {
	fb := mustParseRFC3339(t, "2026-01-01T00:00:00Z")
	got := issueClosedAt(&types.Issue{}, fb)
	if !got.Equal(fb) {
		t.Errorf("expected fallback=%v, got %v", fb, got)
	}
}

func TestIssueClosedAt_NilIssue(t *testing.T) {
	fb := mustParseRFC3339(t, "2026-01-01T00:00:00Z")
	got := issueClosedAt(nil, fb)
	if !got.Equal(fb) {
		t.Errorf("expected fallback for nil issue=%v, got %v", fb, got)
	}
}
