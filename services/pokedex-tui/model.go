package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

const listWidth = 34

type item struct{ p api.Pokemon }

func (i item) Title() string { return fmt.Sprintf("#%03d %s", i.p.ID, i.p.Name) }

func (i item) Description() string { return strings.Join(i.p.Types, " / ") }

func (i item) FilterValue() string {
	return i.p.Name + " " + strings.Join(i.p.Types, " ")
}

type loadedMsg []api.Pokemon

type errMsg struct{ err error }

func (e errMsg) Error() string { return e.err.Error() }

func statusFor(err error) string {
	if advice := battleclient.IdentityAdvice(err); advice != "" {
		return advice + " — run `pokedex-cli register <name>`"
	}
	return err.Error()
}

type model struct {
	list   list.Model
	client *api.Client

	loading bool
	err     error

	width, height int

	screen screen

	// animating is true while a frame chain is running, so a version
	// bump during an animation does not start a second one.
	animating bool

	bc      *battleclient.Client
	trainer string

	lobby    *api.WaitingList
	lobbyIdx int

	battle *battleState

	// team is the three names picked for the next battle, in order.
	team []string

	// joining is the battle id being joined, empty when opening a new one.
	joining string

	lastBattle string

	// status is a transient line: an error from an action, or a hint.
	status string
}

// newDelegate builds the row renderer for a light or dark terminal.
func newDelegate(isDark bool) list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.Styles = list.NewDefaultItemStyles(isDark)
	return d
}

func newModel(c *api.Client, apiBase string) model {
	l := list.New(nil, newDelegate(true), 0, 0)
	l.Title = "Pokedex"
	l.SetShowStatusBar(true)
	l.SetStatusBarItemName("pokemon", "pokemon")

	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		}
	}

	m := model{list: l, client: c, loading: true}
	if id, err := battleclient.LoadIdentity(); err == nil {
		id.API = apiBase
		if bc, err := battleclient.New(id); err == nil {
			m.bc = bc
			m.trainer = id.Name
		}
	}
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, m.fetch)
}

func (m model) fetch() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := m.client.ListPokemon(ctx, api.ListPokemonParams{})
	if err != nil {
		return errMsg{err}
	}
	return loadedMsg(res.Pokemon)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		lh, lv := listStyle.GetFrameSize()
		m.list.SetSize(listWidth-lh, msg.Height-lv)
		return m, nil

	case tea.BackgroundColorMsg:
		isDark := msg.IsDark()
		m.list.Styles = list.DefaultStyles(isDark)
		m.list.SetDelegate(newDelegate(isDark))
		return m, nil

	case loadedMsg:
		m.loading = false
		items := make([]list.Item, len(msg))
		for i, p := range msg {
			items[i] = item{p: p}
		}
		return m, m.list.SetItems(items)

	case errMsg:
		m.loading = false
		m.err = msg.err
		return m, nil

	case frameMsg:
		if m.battle == nil {
			m.animating = false
			return m, nil
		}
		if m.battle.advance() {
			return m, tick()
		}
		m.animating = false
		return m, nil

	case battleMsg:
		// Belongs to the battle we are actually in, or it is the reply
		// to a poll for one we have left. A tea.Cmd cannot be
		// cancelled, so that reply still arrives - and joining a new
		// battle inside the poll interval used to apply the OLD
		// battle's state to the new one. Dropping it here also ends
		// the orphaned chain, because nothing re-arms it.
		if m.battle == nil || msg.id != m.battle.id {
			return m, nil
		}
		if msg.err != nil {
			m.battle.misses++
			// Only fatal once the retry budget is spent. Setting err on
			// the FIRST miss replaced the whole battle - HP, teams, log -
			// with "battle error, press esc", so misses 2 through 5 were
			// invisible to a player who had already been told it was
			// broken. The counter exists precisely because one miss is
			// expected to be survivable.
			if m.battle.misses >= maxPollMisses {
				m.battle.err = msg.err
				m.screen = screenLobby
				m.status = "that battle is over - the server restarted and battles do not survive it"
				m.battle = nil
				return m, fetchLobby(m.bc)
			}
			// Keep the last good battle on screen and keep trying, but
			// only the poll chain re-arms itself.
			if !msg.polled {
				return m, nil
			}
			return m, pollBattle(m.bc, m.battle.id)
		}
		m.battle.err = nil
		m.battle.misses = 0
		start := msg.battle.Version > m.battle.seen
		m.battle.applyBattle(msg.battle)

		// Re-arm only for a poll. An attack's reply is a battleMsg too,
		// and re-arming on it started a second chain that never stopped:
		// one more GET per second for every turn taken.
		var cmds []tea.Cmd
		if msg.polled {
			cmds = append(cmds, pollBattle(m.bc, m.battle.id))
		}
		// One frame chain at a time. frameMsg re-arms itself while
		// advance() reports movement, so a version bump landing during
		// an existing drain used to start a SECOND chain - both calling
		// advance(), so HP drained and floats expired at double speed,
		// getting faster the more turns were played.
		if start && !m.animating {
			m.animating = true
			cmds = append(cmds, tick())
		}
		return m, tea.Batch(cmds...)

	case startedMsg:
		if msg.err != nil {
			m.screen = screenLobby
			m.status = statusFor(msg.err)
			return m, fetchLobby(m.bc)
		}
		bs := &battleState{client: m.bc, id: msg.battle.ID, shown: map[slot]int{}}
		m.lastBattle = msg.battle.ID
		bs.applyBattle(msg.battle)
		m.battle = bs
		m.screen = screenBattle
		m.status = ""
		return m, tea.Batch(pollBattle(m.bc, bs.id), tick())

	case lobbyMsg:
		var again tea.Cmd
		if m.screen == screenLobby {
			again = pollLobby(m.bc)
		}
		if msg.err != nil {
			if !msg.polled {
				m.status = statusFor(msg.err)
			}
			return m, again
		}
		m.lobby = msg.list
		if m.lobbyIdx >= len(msg.list.Waiting) {
			m.lobbyIdx = 0
		}
		return m, again

	case tea.KeyPressMsg:
		if m.screenUsesList() && m.list.FilterState() == list.Filtering {
			break
		}
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.handleKey(msg)
	}

	if !m.screenUsesList() {
		return m, nil
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) screenUsesList() bool {
	return m.screen == screenBrowse || m.screen == screenTeam
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenLobby:
		return m.lobbyKey(msg)
	case screenTeam:
		return m.teamKey(msg)
	case screenBattle:
		return m.battleKey(msg)
	default:
		return m.browseKey(msg)
	}
}

func (m model) View() tea.View {
	var content string
	switch m.screen {
	case screenLobby:
		content = m.lobbyView()
	case screenTeam:
		content = m.teamView()
	case screenBattle:
		content = m.battle.view(m.width, m.height)
	default:
		content = m.browseView()
	}

	if m.status != "" {
		content += "\n" + statusStyle.Render("  "+m.status)
	}

	v := tea.NewView(content)
	v.AltScreen = true
	return v
}
