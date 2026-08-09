package picker

import (
	"regexp"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func testModel(items []Item, query string) model {
	labelW := 0
	for _, it := range items {
		if w := runewidth.StringWidth(it.Label); w > labelW {
			labelW = w
		}
	}
	m := model{
		items:  items,
		labelW: labelW,
		prompt: "repo> ",
		choice: -1,
		width:  200,
		height: 40,
	}
	m.query = query
	m.filter()
	return m
}

// plainLines renders the view and returns its lines with ANSI stripped.
func plainLines(m model) []string {
	return strings.Split(ansiRE.ReplaceAllString(m.View().Content, ""), "\n")
}

// detailColumns returns the display column at which each list line's
// detail starts — display width, not byte offset, so CJK labels measure
// the way the terminal renders them.
func detailColumns(t *testing.T, lines []string) []int {
	t.Helper()
	var cols []int
	for _, l := range lines {
		i := strings.Index(l, "/detail/")
		if i < 0 {
			continue
		}
		cols = append(cols, runewidth.StringWidth(l[:i]))
	}
	if len(cols) == 0 {
		t.Fatalf("no list lines found:\n%s", strings.Join(lines, "\n"))
	}
	return cols
}

func TestViewAlignsDetails(t *testing.T) {
	items := []Item{
		{Label: "qa-mercoin-update-vulnerable-dependencies (main)", Detail: "/detail/main"},
		{Label: "install-tobari", Detail: "/detail/wt/new-feature"},
		{Label: "skip-malformed-scanning-alert-sync", Detail: "/detail/claude-worktrees"},
		{Label: "qa-mercoin-fix-http-auth", Detail: "/detail/fix"},
	}
	m := testModel(items, "")

	cols := detailColumns(t, plainLines(m))
	if len(cols) != len(items) {
		t.Fatalf("rendered %d list lines, want %d", len(cols), len(items))
	}
	want := 2 + m.labelW + 2 // pointer + padded label + gap
	for i, c := range cols {
		if c != want {
			t.Errorf("line %d: detail starts at column %d, want %d", i, c, want)
		}
	}
}

func TestViewAlignsDetailsWithQuery(t *testing.T) {
	items := []Item{
		{Label: "qa-mercoin-update-vulnerable-dependencies (main)", Detail: "/detail/main"},
		{Label: "qa-mercoin-fix-http-auth", Detail: "/detail/fix"},
	}
	m := testModel(items, "qamercoin")

	cols := detailColumns(t, plainLines(m))
	if len(cols) != 2 {
		t.Fatalf("rendered %d list lines, want 2", len(cols))
	}
	// Match highlighting inserts SGR sequences only; once stripped, the
	// columns must line up regardless of which runes matched.
	if cols[0] != cols[1] {
		t.Errorf("detail columns differ with a query: %v", cols)
	}
}

func TestViewCJKLabelAlignment(t *testing.T) {
	items := []Item{
		{Label: "feature/日本語ブランチ", Detail: "/detail/a"},
		{Label: "main", Detail: "/detail/b"},
	}
	m := testModel(items, "")

	cols := detailColumns(t, plainLines(m))
	if cols[0] != cols[1] {
		t.Errorf("detail columns differ with CJK labels: %v", cols)
	}
}
