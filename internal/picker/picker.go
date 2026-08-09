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

// Pick shows the picker on the terminal (rendering on /dev/tty, so stdout
// stays free for the result) and returns the chosen item.
func Pick(prompt, header string, items []Item) (Item, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return Item{}, fmt.Errorf("cannot open terminal: %w", err)
	}
	defer tty.Close()

	labelW := 0
	for _, it := range items {
		if w := runewidth.StringWidth(it.Label); w > labelW {
			labelW = w
		}
	}
	m := model{
		prompt: prompt,
		header: header,
		items:  items,
		labelW: labelW,
		choice: -1,
	}
	m.filter()

	p := tea.NewProgram(m, tea.WithInput(tty), tea.WithOutput(tty))
	res, err := p.Run()
	if err != nil {
		return Item{}, err
	}
	final := res.(model)
	if final.choice < 0 {
		return Item{}, ErrAbort
	}
	return final.items[final.choice], nil
}

// row is one filtered entry: the item index and the byte offsets of the
// matched label runes.
type row struct {
	item    int
	matched []int
}

type model struct {
	prompt string
	header string
	items  []Item
	labelW int

	query  string
	rows   []row
	cursor int
	offset int
	width  int
	height int

	choice int
	done   bool
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
		switch k.String() {
		case "ctrl+c", "esc":
			m.done = true
			return m, tea.Quit
		case "enter":
			if len(m.rows) > 0 {
				m.choice = m.rows[m.cursor].item
			}
			m.done = true
			return m, tea.Quit
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

// listHeight caps the list at roughly half the terminal, shrinking to the
// match count so the block does not leave blank rows.
func (m model) listHeight() int {
	max := m.height/2 - 3
	if max < 5 {
		max = 5
	}
	if len(m.rows) < max {
		return len(m.rows)
	}
	return max
}

const (
	sgrReset   = "\x1b[0m"
	sgrDim     = "\x1b[2m"
	sgrBold    = "\x1b[1m"
	sgrMatch   = "\x1b[1;36m"
	sgrPointer = "\x1b[1;31m"
)

func dim(s string) string { return sgrDim + s + sgrReset }

func (m model) View() tea.View {
	if m.done {
		return tea.NewView("") // clear the block on exit, like fzf --height
	}
	var b strings.Builder
	b.WriteString(sgrBold + m.prompt + sgrReset + m.query + "\n")
	b.WriteString(dim(fmt.Sprintf("  %d/%d", len(m.rows), len(m.items))) + "\n")
	if m.header != "" {
		b.WriteString(dim(m.header) + "\n")
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
	v.Cursor = tea.NewCursor(runewidth.StringWidth(m.prompt)+runewidth.StringWidth(m.query), 0)
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
