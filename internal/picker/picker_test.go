package picker

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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

func TestPasteAppendsToQuery(t *testing.T) {
	items := []Item{
		{Label: "feature-x", Detail: "/detail/a"},
		{Label: "main", Detail: "/detail/b"},
	}
	m := testModel(items, "")

	// C0, DEL, and C1 (e.g. CSI, which terminals may interpret as an
	// escape sequence) must all be stripped.
	next, _ := m.Update(tea.PasteMsg{Content: "feat\nure-x\r"})
	got := next.(model)
	if got.query != "feature-x" {
		t.Errorf("query after paste = %q, want %q (control characters stripped)", got.query, "feature-x")
	}
	if len(got.rows) != 1 || got.rows[0].item != 0 {
		t.Errorf("paste did not refilter: rows = %+v", got.rows)
	}

	next, _ = got.Update(tea.PasteMsg{Content: "[2J"})
	got = next.(model)
	if got.query != "feature-x[2J" {
		t.Errorf("C1 control not stripped: query = %q", got.query)
	}
}

func TestCtrlHDeletesLikeBackspace(t *testing.T) {
	// Terminals may send the same byte (0x08) for ctrl+h and backspace,
	// and fzf binds ctrl+h to deletion too: it must edit the query, not
	// mean anything else.
	items := []Item{
		{Label: "feature-x", Detail: "/detail/a"},
		{Label: "main", Detail: "/detail/b"},
	}
	m := testModel(items, "fe")

	next, _ := m.Update(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	got := next.(model)
	if got.query != "f" {
		t.Errorf("query after ctrl+h = %q, want %q", got.query, "f")
	}
	next, _ = got.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	got = next.(model)
	if got.query != "" {
		t.Errorf("query after backspace = %q, want empty", got.query)
	}
	// On an empty query both keys are inert — neither aborts nor picks.
	for _, key := range []tea.KeyPressMsg{
		{Code: 'h', Mod: tea.ModCtrl},
		{Code: tea.KeyBackspace},
	} {
		next, _ = got.Update(key)
		got = next.(model)
		if got.done || got.aborted {
			t.Errorf("%s on an empty query ended the picker", tea.Key(key))
		}
	}
}

func TestInitialQueryFilters(t *testing.T) {
	// A run offered again after reloading its items resumes with the
	// query already typed, so the reload does not cost the filter.
	items := []Item{{Label: "feature-x"}, {Label: "main"}}
	m := testModel(items, "")
	m.query = Sanitize("feat")
	m.filter()
	if len(m.rows) != 1 || m.rows[0].item != 0 {
		t.Errorf("initial query did not filter: rows = %+v", m.rows)
	}
}

func TestSanitizeNeutralizesUntrustedStrings(t *testing.T) {
	for _, tt := range []struct {
		name, in, want string
	}{
		{"csi", "wt\x1b[2Jgone", "wt�[2Jgone"},
		{"newline", "main\nrogue", "main�rogue"},
		{"c1 introducer", "wt\u009b2Jgone", "wt�2Jgone"},
		{"del", "wt\x7f", "wt�"},
		{"invalid utf-8", "wt\xff", "wt�"},
		{"kept as is", "feature/日本語 (main)", "feature/日本語 (main)"},
	} {
		if got := Sanitize(tt.in); got != tt.want {
			t.Errorf("%s: Sanitize(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

func TestViewDrawsNoInjectedSequences(t *testing.T) {
	// A directory can be named with an ESC in it, and a config value can
	// carry one too. Drawn as they arrive, the terminal would obey them —
	// OSC 52 reaches the clipboard — and a newline would split one row
	// into two, moving every row below it out of place.
	items := sanitizeItems([]Item{
		{Label: "wt\x1b[2J", Detail: "/detail/a\x1b]52;c;ZXZpbA==\x07"},
		{Label: "main\nrogue", Detail: "/detail/b"},
	})
	m := testModel(items, "")
	view := m.View().Content

	for _, seq := range []string{"\x1b[2J", "\x1b]52", "\x1b]"} {
		if strings.Contains(view, seq) {
			t.Errorf("view carries the injected sequence %q", seq)
		}
	}
	if !strings.Contains(view, "�") {
		t.Error("view drops the neutralized characters instead of marking them")
	}
	// Prompt, counter rule, and one line per item: the newline in a label
	// must not have added a line.
	if lines := plainLines(m); len(lines) != 4 {
		t.Errorf("view has %d lines, want 4:\n%q", len(lines), lines)
	}
}

func TestResultDistinguishesNoAnswerFromAnAnswer(t *testing.T) {
	items := []Item{{Label: "no"}, {Label: "yes"}}

	// Accepted with nothing matching the query: an answer all the same,
	// which an input box reads as its query.
	m := testModel(items, "maybe")
	m.done = true
	res, err := result(m, false)
	if err != nil || res.Index != -1 || res.Query != "maybe" {
		t.Errorf("accepted with no match = (%+v, %v), want index -1, query \"maybe\", no error", res, err)
	}

	// Esc or Ctrl+C: this step is off, callers may offer it again.
	m = testModel(items, "")
	m.aborted, m.done = true, true
	if _, err := result(m, false); !errors.Is(err, ErrAbort) {
		t.Errorf("aborted run = %v, want ErrAbort", err)
	}

	// Neither accepted nor aborted: the program ended by itself, as it
	// does on SIGTERM, which bubbletea quits on without an error. The
	// model's state is not an answer and must not read as one.
	m = testModel(items, "y")
	if _, err := result(m, false); !errors.Is(err, ErrTerminated) {
		t.Errorf("run that ended without an answer = %v, want ErrTerminated", err)
	}
}

func TestYesNoConfirmationRows(t *testing.T) {
	// The confirmation prompt is a two-item picker: "no" first so Enter
	// keeps the safe default, and either answer reachable by typing its
	// first letter in any case.
	items := []Item{{Label: "no"}, {Label: "yes"}}
	for _, tt := range []struct {
		query string
		want  int // item index Enter would accept, -1 for none
	}{
		{"", 0},
		{"y", 1},
		{"Y", 1},
		{"yes", 1},
		{"n", 0},
		{"N", 0},
		{"no", 0},
		{"maybe", -1},
	} {
		m := testModel(items, tt.query)
		got := -1
		if len(m.rows) > 0 {
			got = m.rows[m.cursor].item
		}
		if got != tt.want {
			t.Errorf("query %q selects item %d, want %d", tt.query, got, tt.want)
		}
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
