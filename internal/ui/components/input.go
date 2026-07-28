package components

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"claude-code-go/internal/vim"
)

// =============================================================================
// Input Component
// =============================================================================

// Styles
var (
	inputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1)

	promptStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86")).
			Bold(true)

	placeholderStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("243"))

	cursorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86")).
			Bold(true)

	multilineHintStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241"))
)

// InputModel represents a text input field.
type InputModel struct {
	Value       string
	Placeholder string
	Prompt      string
	CursorPos   int
	Width       int
	Multiline   bool
	Focused     bool
	History     []string
	HistoryPos  int
	VimEnabled  bool
	VimState    *vim.VimState

	vimPersistent *vim.PersistentState
	vimTransition *vim.Transition
	undo          []inputSnapshot
	redo          []inputSnapshot
	lastEdit      *vim.Action
	visualAnchor  int
}

type inputSnapshot struct {
	value  string
	cursor int
}

// NewInput creates a new input field.
func NewInput(prompt, placeholder string, width int) *InputModel {
	return &InputModel{
		Value:       "",
		Placeholder: placeholder,
		Prompt:      prompt,
		CursorPos:   0,
		Width:       width,
		Multiline:   false,
		Focused:     true,
		History:     []string{},
		HistoryPos:  -1,
		VimState:    vim.NewVimState(),
	}
}

// Update handles input updates.
func (m *InputModel) Update(msg tea.Msg) tea.Cmd {
	if !m.Focused {
		return nil
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	if m.VimEnabled {
		m.updateVim(key)
		return nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyBackspace:
			if m.CursorPos > 0 {
				runes := []rune(m.Value)
				m.Value = string(append(runes[:m.CursorPos-1], runes[m.CursorPos:]...))
				m.CursorPos--
			}
		case tea.KeyDelete:
			runes := []rune(m.Value)
			if m.CursorPos < len(runes) {
				m.Value = string(append(runes[:m.CursorPos], runes[m.CursorPos+1:]...))
			}
		case tea.KeyLeft:
			if m.CursorPos > 0 {
				m.CursorPos--
			}
		case tea.KeyRight:
			if m.CursorPos < len([]rune(m.Value)) {
				m.CursorPos++
			}
		case tea.KeyHome:
			m.CursorPos = 0
		case tea.KeyEnd:
			m.CursorPos = len([]rune(m.Value))
		case tea.KeyUp:
			// Navigate history
			if len(m.History) > 0 && m.HistoryPos < len(m.History)-1 {
				m.HistoryPos++
				m.Value = m.History[len(m.History)-1-m.HistoryPos]
				m.CursorPos = len([]rune(m.Value))
			}
		case tea.KeyDown:
			// Navigate history
			if m.HistoryPos > 0 {
				m.HistoryPos--
				m.Value = m.History[len(m.History)-1-m.HistoryPos]
				m.CursorPos = len([]rune(m.Value))
			} else if m.HistoryPos == 0 {
				m.HistoryPos = -1
				m.Value = ""
				m.CursorPos = 0
			}
		case tea.KeyRunes:
			// Insert character at cursor position
			m.insertRunes(msg.Runes)
		}
	}

	return nil
}

// View renders the input field.
func (m *InputModel) View() string {
	var b strings.Builder

	// Render prompt
	b.WriteString(promptStyle.Render(m.Prompt) + " ")

	// Render value with cursor
	if m.Value == "" {
		// Show placeholder
		b.WriteString(placeholderStyle.Render(m.Placeholder))
	} else {
		// Show value with cursor
		runes := []rune(m.Value)
		cursor := min(max(m.CursorPos, 0), len(runes))
		before := string(runes[:cursor])
		atCursor := " "
		after := ""
		if cursor < len(runes) {
			atCursor = string(runes[cursor])
			after = string(runes[cursor+1:])
		}

		b.WriteString(before + cursorStyle.Render(atCursor) + after)
	}

	// Multiline hint
	if m.Multiline {
		b.WriteString("\n" + multilineHintStyle.Render("Shift+Enter for new line"))
	}

	return inputBoxStyle.Render(b.String())
}

// SetValue sets the input value.
func (m *InputModel) SetValue(value string) {
	m.Value = value
	m.CursorPos = len([]rune(value))
}

// Clear clears the input.
func (m *InputModel) Clear() {
	// Save to history if not empty
	if m.Value != "" {
		m.History = append(m.History, m.Value)
	}
	m.Value = ""
	m.CursorPos = 0
	m.HistoryPos = -1
	m.undo, m.redo, m.lastEdit = nil, nil, nil
	if m.VimEnabled {
		m.VimState = vim.NewVimState()
		m.vimPersistent = vim.NewPersistentState()
		m.vimTransition = vim.NewTransition(m.VimState, m.vimPersistent)
	}
}

// Focus focuses the input.
func (m *InputModel) Focus() {
	m.Focused = true
}

// Blur unfocuses the input.
func (m *InputModel) Blur() {
	m.Focused = false
}

// SetVimEnabled switches editing behavior without changing the current text.
func (m *InputModel) SetVimEnabled(enabled bool) {
	if m.VimEnabled == enabled {
		return
	}
	m.VimEnabled = enabled
	if enabled {
		m.VimState = vim.NewVimState()
		m.vimPersistent = vim.NewPersistentState()
		m.vimTransition = vim.NewTransition(m.VimState, m.vimPersistent)
		return
	}
	m.VimState, m.vimPersistent, m.vimTransition = vim.NewVimState(), nil, nil
}

func (m *InputModel) updateVim(key tea.KeyMsg) {
	if m.vimTransition == nil {
		m.SetVimEnabled(true)
	}
	if m.VimState.Mode == vim.ModeInsert {
		switch key.Type {
		case tea.KeyBackspace:
			if m.CursorPos > 0 {
				m.recordMutation()
				m.deleteRange(m.CursorPos-1, m.CursorPos)
			}
			return
		case tea.KeyDelete:
			if m.CursorPos < len([]rune(m.Value)) {
				m.recordMutation()
				m.deleteRange(m.CursorPos, m.CursorPos+1)
			}
			return
		case tea.KeyLeft:
			m.CursorPos = max(0, m.CursorPos-1)
			return
		case tea.KeyRight:
			m.CursorPos = min(len([]rune(m.Value)), m.CursorPos+1)
			return
		case tea.KeyHome:
			m.CursorPos = 0
			return
		case tea.KeyEnd:
			m.CursorPos = len([]rune(m.Value))
			return
		}
	}

	keys := keyStrings(key)
	for _, value := range keys {
		action, err := m.vimTransition.ActionForKey(value)
		if err == nil {
			m.applyVimAction(action)
		}
	}
}

func keyStrings(key tea.KeyMsg) []string {
	switch key.Type {
	case tea.KeyEsc:
		return []string{"<Esc>"}
	case tea.KeyCtrlR:
		return []string{"\x12"}
	case tea.KeyLeft:
		return []string{"h"}
	case tea.KeyRight:
		return []string{"l"}
	case tea.KeyUp:
		return []string{"k"}
	case tea.KeyDown:
		return []string{"j"}
	case tea.KeyRunes:
		values := make([]string, len(key.Runes))
		for index, value := range key.Runes {
			values[index] = string(value)
		}
		return values
	default:
		return nil
	}
}

func (m *InputModel) applyVimAction(action vim.Action) {
	switch action.Kind {
	case vim.ActionInsert:
		m.recordMutation()
		m.insertRunes([]rune(action.Text))
		copy := action
		m.lastEdit = &copy
	case vim.ActionSetMode:
		if action.Mode == vim.ModeVisual {
			m.visualAnchor = m.CursorPos
		}
		if action.Mode == vim.ModeInsert {
			switch action.Motion {
			case "start":
				m.CursorPos = 0
			case "end":
				m.CursorPos = len([]rune(m.Value))
			}
		}
		if action.Mode == vim.ModeNormal && action.FromMode == vim.ModeInsert {
			if action.Text != "" {
				m.lastEdit = &vim.Action{Kind: vim.ActionInsert, Text: action.Text, Count: 1}
			}
			m.CursorPos = max(0, m.CursorPos-1)
		}
	case vim.ActionMove:
		m.move(action.Motion, action.Count)
	case vim.ActionDelete:
		m.applyOperator(vim.Action{Kind: vim.ActionOperator, Operator: vim.OperatorDelete, Motion: action.Motion, Count: action.Count})
	case vim.ActionOperator:
		m.applyOperator(action)
	case vim.ActionFind:
		m.find(action)
	case vim.ActionUndo:
		m.restore(&m.undo, &m.redo)
	case vim.ActionRedo:
		m.restore(&m.redo, &m.undo)
	case vim.ActionRepeat:
		if m.lastEdit != nil {
			m.applyRepeated(*m.lastEdit)
		}
	case vim.ActionPaste:
		if m.vimPersistent != nil && m.vimPersistent.Register != "" {
			m.recordMutation()
			if action.Motion == "after" {
				m.CursorPos = min(len([]rune(m.Value)), m.CursorPos+1)
			}
			m.insertRunes([]rune(m.vimPersistent.Register))
		}
	case vim.ActionReplace:
		m.recordMutation()
		m.deleteRange(m.CursorPos, min(len([]rune(m.Value)), m.CursorPos+max(1, action.Count)))
		m.insertRunes([]rune(strings.Repeat(action.Text, max(1, action.Count))))
		copy := action
		m.lastEdit = &copy
	}
}

func (m *InputModel) applyRepeated(action vim.Action) {
	switch action.Kind {
	case vim.ActionInsert:
		m.recordMutation()
		m.insertRunes([]rune(action.Text))
	case vim.ActionOperator, vim.ActionDelete:
		m.applyOperator(action)
	case vim.ActionReplace:
		m.applyVimAction(action)
	}
}

func (m *InputModel) applyOperator(action vim.Action) {
	start, end := m.motionRange(action)
	if start == end {
		return
	}
	runes := []rune(m.Value)
	text := string(runes[start:end])
	if m.vimPersistent != nil {
		m.vimPersistent.Register = text
	}
	if action.Operator == vim.OperatorYank {
		return
	}
	m.recordMutation()
	m.deleteRange(start, end)
	copy := action
	m.lastEdit = &copy
}

func (m *InputModel) motionRange(action vim.Action) (int, int) {
	runes := []rune(m.Value)
	count := max(1, action.Count)
	switch action.Motion {
	case "char":
		return m.CursorPos, min(len(runes), m.CursorPos+count)
	case "line":
		return 0, len(runes)
	case "selection":
		return min(m.visualAnchor, m.CursorPos), min(len(runes), max(m.visualAnchor, m.CursorPos)+1)
	case "w", "W":
		end := m.CursorPos
		for range count {
			end = nextWordStart(runes, end)
		}
		return m.CursorPos, end
	case "$":
		return m.CursorPos, len(runes)
	case "0":
		return 0, min(len(runes), m.CursorPos+1)
	case "h":
		return max(0, m.CursorPos-count), min(len(runes), m.CursorPos+1)
	case "l":
		return m.CursorPos, min(len(runes), m.CursorPos+count+1)
	case "textobj":
		return wordBounds(runes, m.CursorPos)
	case "find":
		original := m.CursorPos
		m.find(action)
		return min(original, m.CursorPos), min(len(runes), max(original, m.CursorPos)+1)
	default:
		return m.CursorPos, m.CursorPos
	}
}

func (m *InputModel) move(motion string, count int) {
	runes := []rune(m.Value)
	count = max(1, count)
	switch motion {
	case "h":
		m.CursorPos = max(0, m.CursorPos-count)
	case "l":
		m.CursorPos = min(max(0, len(runes)-1), m.CursorPos+count)
	case "0", "^":
		m.CursorPos = 0
	case "$":
		m.CursorPos = max(0, len(runes)-1)
	case "w", "W":
		for range count {
			m.CursorPos = nextWordStart(runes, m.CursorPos)
		}
	case "b", "B":
		for range count {
			m.CursorPos = previousWordStart(runes, m.CursorPos)
		}
	case "e", "E":
		for range count {
			m.CursorPos = wordEnd(runes, m.CursorPos)
		}
	}
}

func (m *InputModel) find(action vim.Action) {
	runes := []rune(m.Value)
	target := []rune(action.Text)
	if len(target) == 0 {
		return
	}
	count := max(1, action.Count)
	direction := 1
	if action.Find == vim.FindTypeFUpper || action.Find == vim.FindTypeTUpper {
		direction = -1
	}
	position := m.CursorPos
	for range count {
		found := -1
		for index := position + direction; index >= 0 && index < len(runes); index += direction {
			if runes[index] == target[0] {
				found = index
				break
			}
		}
		if found < 0 {
			return
		}
		position = found
	}
	if action.Find == vim.FindTypeT {
		position--
	} else if action.Find == vim.FindTypeTUpper {
		position++
	}
	m.CursorPos = min(max(position, 0), max(0, len(runes)-1))
}

func (m *InputModel) recordMutation() {
	m.undo = append(m.undo, inputSnapshot{value: m.Value, cursor: m.CursorPos})
	m.redo = nil
}

func (m *InputModel) restore(from, to *[]inputSnapshot) {
	if len(*from) == 0 {
		return
	}
	*to = append(*to, inputSnapshot{value: m.Value, cursor: m.CursorPos})
	last := len(*from) - 1
	snapshot := (*from)[last]
	*from = (*from)[:last]
	m.Value, m.CursorPos = snapshot.value, snapshot.cursor
}

func (m *InputModel) insertRunes(inserted []rune) {
	runes := []rune(m.Value)
	m.CursorPos = min(max(m.CursorPos, 0), len(runes))
	updated := make([]rune, 0, len(runes)+len(inserted))
	updated = append(updated, runes[:m.CursorPos]...)
	updated = append(updated, inserted...)
	updated = append(updated, runes[m.CursorPos:]...)
	m.Value = string(updated)
	m.CursorPos += len(inserted)
}

func (m *InputModel) deleteRange(start, end int) {
	runes := []rune(m.Value)
	start, end = min(max(start, 0), len(runes)), min(max(end, 0), len(runes))
	if start > end {
		start, end = end, start
	}
	m.Value = string(append(append([]rune{}, runes[:start]...), runes[end:]...))
	m.CursorPos = min(start, max(0, len([]rune(m.Value))-1))
}

func nextWordStart(runes []rune, position int) int {
	position = min(max(position, 0), len(runes))
	for position < len(runes) && !unicode.IsSpace(runes[position]) {
		position++
	}
	for position < len(runes) && unicode.IsSpace(runes[position]) {
		position++
	}
	return position
}

func previousWordStart(runes []rune, position int) int {
	position = min(max(position-1, 0), len(runes))
	for position > 0 && unicode.IsSpace(runes[position]) {
		position--
	}
	for position > 0 && !unicode.IsSpace(runes[position-1]) {
		position--
	}
	return position
}

func wordEnd(runes []rune, position int) int {
	position = min(max(position, 0), len(runes))
	for position < len(runes) && unicode.IsSpace(runes[position]) {
		position++
	}
	for position+1 < len(runes) && !unicode.IsSpace(runes[position+1]) {
		position++
	}
	return min(position, max(0, len(runes)-1))
}

func wordBounds(runes []rune, position int) (int, int) {
	if len(runes) == 0 {
		return 0, 0
	}
	position = min(max(position, 0), len(runes)-1)
	start := previousWordStart(runes, position+1)
	end := position
	for end < len(runes) && !unicode.IsSpace(runes[end]) {
		end++
	}
	return start, end
}

// =============================================================================
// Multiline Input Component
// =============================================================================

// MultilineInputModel represents a multiline text input.
type MultilineInputModel struct {
	*InputModel
	Lines []string
}

// NewMultilineInput creates a new multiline input.
func NewMultilineInput(prompt, placeholder string, width int) *MultilineInputModel {
	return &MultilineInputModel{
		InputModel: NewInput(prompt, placeholder, width),
		Lines:      []string{""},
	}
}

// View renders the multiline input.
func (m *MultilineInputModel) View() string {
	var b strings.Builder

	// Render prompt
	b.WriteString(promptStyle.Render(m.Prompt) + "\n")

	// Render each line
	for i, line := range m.Lines {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("  " + line)
	}

	// Show cursor on last line if empty
	if len(m.Lines) == 1 && m.Lines[0] == "" {
		b.WriteString(placeholderStyle.Render(m.Placeholder))
	}

	return inputBoxStyle.Render(b.String())
}
