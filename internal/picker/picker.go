// Package picker is an inline fuzzy picker: it renders below the shell
// prompt (no alternate screen) like fzf --height --reverse, matches only
// against labels, and shows details dimmed. The terminal cursor is placed
// at the end of the query so IME preedit text composes in place.
package picker

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/mattn/go-runewidth"
	"github.com/sahilm/fuzzy"
)

// ErrAbort is returned when the user cancels the picker.
var ErrAbort = errors.New("abort")

// Item is one selectable row. Label is fuzzy-matched and shown; Detail is
// shown dimmed and excluded from matching.
type Item struct {
	Label  string
	Detail string
}

// Tone colors a key hint's chip by what the action means: the color
// carries information (danger, creation), not decoration.
type Tone int

const (
	ToneNeutral Tone = iota // navigation and the like
	TonePrimary             // the screen's main action
	ToneCreate              // creates something
	ToneDanger              // destructive
)

// KeyHint is one entry of the help line: the key name rendered as a
// chip, toned by the action's meaning, and its dimmed description.
type KeyHint struct {
	Key  string
	Desc string
	Tone Tone
}

// Options configures a picker run.
type Options struct {
	Prompt string
	// Tone colors the prompt to match the screen's meaning (blue for
	// selection, green for creation), in the same system as the chips.
	Tone Tone
	Keys []KeyHint
	// Expect lists key names (e.g. "tab", "ctrl+n") that accept the
	// current state and return, like fzf --expect.
	Expect []string
}

// Result is what a picker run returns: the accepting key ("" for enter),
// the highlighted item (nil when nothing matched), and the query as typed
// — callers implementing an input box use Query when Item is nil.
type Result struct {
	Key   string
	Item  *Item
	Query string
}

// Run shows the picker on the terminal (rendering on /dev/tty, so stdout
// stays free for results) and returns the accepted state. Esc and Ctrl+C
// return ErrAbort.
func Run(items []Item, opts Options) (Result, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return Result{}, fmt.Errorf("cannot open terminal: %w", err)
	}
	defer tty.Close()

	labelW := 0
	for _, it := range items {
		if w := runewidth.StringWidth(it.Label); w > labelW {
			labelW = w
		}
	}
	expect := make(map[string]bool, len(opts.Expect))
	for _, k := range opts.Expect {
		expect[k] = true
	}
	m := model{
		prompt:      opts.Prompt,
		promptStyle: promptStyle(opts.Tone),
		header:      renderKeyHints(opts.Keys),
		items:       items,
		labelW:      labelW,
		expect:      expect,
		choice:      -1,
	}
	m.filter()

	p := tea.NewProgram(m, tea.WithInput(tty), tea.WithOutput(tty))
	res, err := p.Run()
	if err != nil {
		return Result{}, err
	}
	final := res.(model)
	if final.aborted {
		return Result{}, ErrAbort
	}
	r := Result{Key: final.key, Query: final.query}
	if final.choice >= 0 {
		r.Item = &final.items[final.choice]
	}
	return r, nil
}

// row is one filtered entry: the item index and the byte offsets of the
// matched label runes.
type row struct {
	item    int
	matched []int
}

type model struct {
	prompt      string
	promptStyle string
	header      string
	items       []Item
	labelW      int
	expect      map[string]bool

	query  string
	rows   []row
	cursor int
	offset int
	width  int
	height int

	choice  int
	key     string
	aborted bool
	done    bool
}

type source []Item

func (s source) String(i int) string { return s[i].Label }
func (s source) Len() int            { return len(s) }

func (m *model) filter() {
	m.rows = m.rows[:0]
	if m.query == "" {
		for i := range m.items {
			m.rows = append(m.rows, row{item: i})
		}
	} else {
		for _, mt := range fuzzy.FindFrom(m.query, source(m.items)) {
			m.rows = append(m.rows, row{item: mt.Index, matched: mt.MatchedIndexes})
		}
	}
	m.cursor, m.offset = 0, 0
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		k := tea.Key(msg)
		if m.expect[k.String()] {
			return m.accept(k.String())
		}
		switch k.String() {
		case "ctrl+c", "esc":
			m.aborted = true
			m.done = true
			return m, tea.Quit
		case "enter":
			return m.accept("")
		case "up", "ctrl+p", "ctrl+k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "ctrl+n", "ctrl+j":
			if m.cursor < len(m.rows)-1 {
				m.cursor++
			}
		case "backspace":
			if m.query != "" {
				r := []rune(m.query)
				m.query = string(r[:len(r)-1])
				m.filter()
			}
		case "ctrl+u":
			if m.query != "" {
				m.query = ""
				m.filter()
			}
		case "ctrl+w":
			q := strings.TrimRight(m.query, " ")
			if i := strings.LastIndexByte(q, ' '); i >= 0 {
				m.query = q[:i+1]
			} else {
				m.query = ""
			}
			m.filter()
		default:
			if k.Text != "" {
				m.query += k.Text
				m.filter()
			}
		}
	}
	if h := m.listHeight(); h > 0 {
		if m.cursor < m.offset {
			m.offset = m.cursor
		}
		if m.cursor >= m.offset+h {
			m.offset = m.cursor - h + 1
		}
	}
	return m, nil
}

// accept finishes the run, recording the pressed key and the highlighted
// item (when any row matches).
func (m model) accept(key string) (tea.Model, tea.Cmd) {
	if len(m.rows) > 0 {
		m.choice = m.rows[m.cursor].item
	}
	m.key = key
	m.done = true
	return m, tea.Quit
}

// listHeight caps the list at roughly half the terminal, shrinking to the
// match count so the block does not leave blank rows.
func (m model) listHeight() int {
	limit := m.height/2 - 3
	if limit < 5 {
		limit = 5
	}
	if len(m.rows) < limit {
		return len(m.rows)
	}
	return limit
}

const (
	sgrReset   = "\x1b[0m"
	sgrDim     = "\x1b[2m"
	sgrBold    = "\x1b[1m"
	sgrMatch   = "\x1b[1;36m"
	sgrPointer = "\x1b[1;31m"
	sgrChip    = "\x1b[48;5;238m\x1b[38;5;253m"
	sgrRule    = "\x1b[38;5;240m"
	sgrCount   = "\x1b[38;5;178m"
)

func dim(s string) string { return sgrDim + s + sgrReset }

// renderKeyHints styles the help line so the key names stand out while
// the descriptions stay quiet: keys as badge-like chips (background
// color reads as shape, robust across themes), dimmed text, and spacing
// instead of separator characters.
func renderKeyHints(keys []KeyHint) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = chipStyle(k.Tone) + " " + k.Key + " " + sgrReset + " " + dim(k.Desc)
	}
	return strings.Join(parts, "  ")
}

func chipStyle(t Tone) string {
	switch t {
	case TonePrimary:
		return "\x1b[48;5;24m\x1b[38;5;152m" // blue
	case ToneCreate:
		return "\x1b[48;5;22m\x1b[38;5;151m" // green
	case ToneDanger:
		return "\x1b[48;5;52m\x1b[38;5;217m" // red
	default:
		return sgrChip
	}
}

func promptStyle(t Tone) string {
	switch t {
	case ToneCreate:
		return "\x1b[1;38;5;108m" // muted green
	case ToneDanger:
		return "\x1b[1;38;5;174m" // muted red
	default:
		return "\x1b[1;38;5;110m" // muted blue: selection screens
	}
}

func (m model) View() tea.View {
	if m.done {
		return tea.NewView("") // clear the block on exit, like fzf --height
	}
	var b strings.Builder
	b.WriteString(m.promptStyle + m.prompt + sgrReset + m.query + "\n")
	// The counter line doubles as the separator between the input and
	// the results: a rule fills the remaining width, like fzf. The rule
	// gets a medium gray instead of dim, which sinks into dark themes.
	counter := fmt.Sprintf("  %d/%d ", len(m.rows), len(m.items))
	b.WriteString(sgrCount + counter + sgrReset)
	if w := m.width - runewidth.StringWidth(counter) - 1; w > 0 {
		b.WriteString(sgrRule + strings.Repeat("─", w) + sgrReset)
	}
	b.WriteString("\n")
	if m.header != "" {
		b.WriteString(m.header + "\n")
	}
	h := m.listHeight()
	for i := m.offset; i < m.offset+h && i < len(m.rows); i++ {
		r := m.rows[i]
		it := m.items[r.item]
		ptr := "  "
		if i == m.cursor {
			ptr = sgrPointer + "> " + sgrReset
		}
		pad := strings.Repeat(" ", m.labelW-runewidth.StringWidth(it.Label))
		line := ptr + renderLabel(it.Label, r.matched, i == m.cursor) + pad
		if it.Detail != "" {
			line += "  " + dim(truncate(it.Detail, m.width-2-m.labelW-2))
		}
		b.WriteString(line + "\n")
	}
	v := tea.NewView(strings.TrimSuffix(b.String(), "\n"))
	// The real terminal cursor sits at the end of the query, so IME
	// preedit text and candidate windows appear at the input position.
	// A steady (non-blinking) block: the default blinking cursor reads
	// as flicker when combined with repaints.
	c := tea.NewCursor(runewidth.StringWidth(m.prompt)+runewidth.StringWidth(m.query), 0)
	c.Shape = tea.CursorBlock
	c.Blink = false
	v.Cursor = c
	return v
}

// renderLabel bolds the current line's label and colors the matched runes.
// matched holds byte offsets, as produced by the fuzzy matcher.
func renderLabel(label string, matched []int, current bool) string {
	base := ""
	if current {
		base = sgrBold
	}
	set := make(map[int]bool, len(matched))
	for _, i := range matched {
		set[i] = true
	}
	var b strings.Builder
	b.WriteString(base)
	for bi, r := range label {
		if set[bi] {
			b.WriteString(sgrMatch + string(r) + sgrReset + base)
		} else {
			b.WriteRune(r)
		}
	}
	b.WriteString(sgrReset)
	return b.String()
}

func truncate(s string, width int) string {
	if width < 2 {
		return ""
	}
	if runewidth.StringWidth(s) <= width {
		return s
	}
	return runewidth.Truncate(s, width-1, "…")
}
