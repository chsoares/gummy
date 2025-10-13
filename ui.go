// ui.go
package main

import (
	"fmt"
	"log"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

//
// ──────────────────────────────────────────────
//   Shared UI state
// ──────────────────────────────────────────────
//

// UIState tells which screen is currently active.
type UIState int

const (
	ListView UIState = iota
	SessionView
)

//
// ──────────────────────────────────────────────
//   List view model
// ──────────────────────────────────────────────
//

type SessionItem struct {
	title, desc string
}

func (i SessionItem) Title() string       { return i.title }
func (i SessionItem) Description() string { return i.desc }
func (i SessionItem) FilterValue() string { return i.title }

type Model struct {
	state       UIState
	list        list.Model
	manager     *SessionManager
	sessionView *SessionViewModel // pointer to session view
	selected    *Session          // currently selected session
	quitting    bool
}

type newSessionMsg *Session
type closedSessionMsg *Session
type openSessionMsg *Session // triggered when pressing Enter

func NewModel(manager *SessionManager) Model {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = true

	l := list.New([]list.Item{}, delegate, 40, 12)
	l.Title = "Active Sessions"
	l.SetShowStatusBar(false)

	return Model{
		state:   ListView,
		list:    l,
		manager: manager,
	}
}

func (m Model) Init() tea.Cmd {
	return waitForSessionEvent(m.manager.Events)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.state {
	//
	// LIST VIEW STATE
	//
	case ListView:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "ctrl+c", "q":
				m.quitting = true
				return m, tea.Quit
			case "enter":
				i, ok := m.list.SelectedItem().(SessionItem)
				if ok {
					// open selected session
					for _, s := range m.manager.Sessions {
						if fmt.Sprintf("Session #%d", s.ID) == i.title {
							m.selected = s
							m.sessionView = NewSessionViewModel(s)
							m.state = SessionView
							return m, nil
						}
					}
				}
			}
		case newSessionMsg:
			item := SessionItem{
				title: fmt.Sprintf("Session #%d", msg.ID),
				desc:  fmt.Sprintf("%s — started at %s", msg.Address, msg.StartTime.Format("15:04:05")),
			}
			m.list.InsertItem(0, item)
		case closedSessionMsg:
			for i, listItem := range m.list.Items() {
				si := listItem.(SessionItem)
				if si.title == fmt.Sprintf("Session #%d", msg.ID) {
					m.list.RemoveItem(i)
					break
				}
			}
		}
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, tea.Batch(cmd, waitForSessionEvent(m.manager.Events))

	//
	// SESSION VIEW STATE
	//
	case SessionView:
		var cmd tea.Cmd
		m.sessionView, cmd = m.sessionView.Update(msg)
		if m.sessionView.exit {
			m.state = ListView
			m.sessionView = nil
		}
		return m, cmd
	}
	return m, nil
}

func (m Model) View() string {
	if m.quitting {
		return "\n  Goodbye!\n"
	}
	switch m.state {
	case ListView:
		style := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(1, 2)
		return style.Render(m.list.View())
	case SessionView:
		return m.sessionView.View()
	default:
		return ""
	}
}

func waitForSessionEvent(events chan SessionEvent) tea.Cmd {
	return func() tea.Msg {
		event := <-events
		switch event.Type {
		case EventNewSession:
			return newSessionMsg(event.Session)
		case EventClosedSession:
			return closedSessionMsg(event.Session)
		default:
			return nil
		}
	}
}

func startUI(manager *SessionManager) {
	p := tea.NewProgram(NewModel(manager))
	if err := p.Start(); err != nil {
		log.Fatal(err)
	}
}
