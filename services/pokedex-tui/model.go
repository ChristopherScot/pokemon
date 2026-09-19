package main

// The Pokedex interface: a filterable list on the left, details for the
// highlighted Pokemon on the right.
//
// Bubble Tea is the Elm architecture: state lives in one struct, every
// input arrives as a message, and Update returns the NEXT state rather
// than mutating the current one. View renders whatever state it is given
// and does nothing else. Work that blocks - the API call below - goes in
// a tea.Cmd, which runs off the event loop and reports back as another
// message, so the interface stays responsive while it is in flight.
//
// Note this is bubbletea v2, whose API differs from most examples
// online: Init returns only a tea.Cmd, View returns a tea.View rather
// than a string, and key presses arrive as tea.KeyPressMsg.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

// listWidth is how much of the window the list takes; the detail pane
// gets the rest.
const listWidth = 34

type item struct{ p api.Pokemon }

func (i item) Title() string { return fmt.Sprintf("#%03d %s", i.p.ID, i.p.Name) }

func (i item) Description() string { return strings.Join(i.p.Types, " / ") }

// FilterValue is what `/` searches. Types are included so "fire" finds
// every fire Pokemon, not just one whose name contains it.
func (i item) FilterValue() string {
	return i.p.Name + " " + strings.Join(i.p.Types, " ")
}

// One message type per outcome, which is the shape the upstream command
// tutorial uses: the type switch in Update then reads as "what
// happened", not "what happened, and did it work".
type loadedMsg []api.Pokemon

type errMsg struct{ err error }

func (e errMsg) Error() string { return e.err.Error() }

// statusFor is what the status line says about an error.
//
// An identity failure gets advice instead of the raw message, because
// "unknown trainer token" names the problem and not the fix. Every
// deploy invalidates every stored token - trainers live in the server's
// memory - so this is the error a returning player is most likely to
// meet, and the least guessable.
//
// It names the CLI because the TUI has no register screen: the two
// share a token file, so registering there fixes it here. The same
// wording as the "no trainer" case a few lines into keys.go, so one
// situation does not get two different instructions.
func statusFor(err error) string {
	if battleclient.IdentityAdvice(err) != "" {
		return "this trainer is no longer registered — run `pokedex-cli register <name>` again"
	}
	return err.Error()
}

type model struct {
	list   list.Model
	client *api.Client

	loading bool
	err     error

	width, height int

	// Which screen is showing. Update and View dispatch on this, as the
	// upstream `views` example does: one model and per-screen handlers,
	// rather than nested Programs.
	screen screen

	// Battle mode. nil until a trainer is registered, because everything
	// here needs a token.
	bc      *battleclient.Client
	trainer string

	lobby    *api.WaitingList
	lobbyIdx int

	battle *battleState

	// team is the three names picked for the next battle, in order.
	team []string

	// joining is the battle id being joined, empty when opening a new one.
	joining string

	// lastBattle is the battle this session was most recently in, kept
	// so leaving the screen is not a one-way trip.
	//
	// esc drops the battle state deliberately - it is a screenful of
	// animation and cursors, not something to keep warm - but the ID is
	// all that is needed to walk back in. The lobby lists only WAITING
	// battles, so once yours goes active it is no longer there, and
	// before this the comment on esc ("can be rejoined from the lobby")
	// was simply untrue for the case that matters.
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
	// Zero size, as the upstream list examples do. Bubble Tea reads the
	// terminal size at startup and delivers a WindowSizeMsg BEFORE the
	// first render, so the real dimensions always arrive before anything
	// is drawn and a placeholder would only ever be wrong.
	// Dark styles to begin with, replaced as soon as the terminal
	// answers RequestBackgroundColor in Init.
	l := list.New(nil, newDelegate(true), 0, 0)
	l.Title = "Pokedex"
	l.SetShowStatusBar(true)
	l.SetStatusBarItemName("pokemon", "pokemon")

	// q is handled in Update, so the list does not know about it; without
	// this the help line never mentions how to leave.
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		}
	}

	m := model{list: l, client: c, loading: true}
	// A stored identity means battle mode is available immediately;
	// without one the b key explains how to register.
	if id, err := battleclient.LoadIdentity(); err == nil {
		id.API = apiBase
		if bc, err := battleclient.New(id); err == nil {
			m.bc = bc
			m.trainer = id.Name
		}
	}
	return m
}

// Init kicks off the fetch. Returning a Cmd rather than calling the API
// here is what keeps the first frame instant: the interface draws
// "loading" immediately and fills in when the response arrives.
func (m model) Init() tea.Cmd {
	// Batch: both run at once and each reports back as its own message,
	// so the background query does not wait on the API call.
	return tea.Batch(tea.RequestBackgroundColor, m.fetch)
}

// fetch is a tea.Cmd: it runs off the event loop and its return value is
// delivered to Update as a message.
func (m model) fetch() tea.Msg {
	// Bounded, because a hung API must not leave the interface stuck on
	// "loading" with no way to find out why.
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
		// Subtract the frame each pane's style adds, so they fill
		// exactly the window between them. Hardcoding these numbers
		// instead is how a layout ends up a row too tall.
		lh, lv := listStyle.GetFrameSize()
		m.list.SetSize(listWidth-lh, msg.Height-lv)
		return m, nil

	case tea.BackgroundColorMsg:
		// The answer to RequestBackgroundColor. Lip Gloss v2 removed
		// AdaptiveColor, so the list keeps its dark defaults until the
		// terminal says otherwise.
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

	// Animation and battle polling only matter on the battle screen, but
	// the messages arrive wherever they were scheduled - so they are
	// handled before the per-screen dispatch rather than inside it.
	case frameMsg:
		if m.battle == nil {
			return m, nil
		}
		if m.battle.advance() {
			return m, tick()
		}
		return m, nil

	case battleMsg:
		if m.battle == nil {
			return m, nil
		}
		if msg.err != nil {
			// Bounded, so a battle that no longer exists stops the
			// loop rather than being asked about once a second
			// forever.
			//
			// Battles live in the server's memory, so a deploy ends
			// every one of them - and a TUI left open on a finished
			// battle kept polling. The web client had the same bug and
			// it is what pushed pokedex-web past its memory limit: 1760
			// requests per 30 minutes, unbroken, for five hours.
			//
			// Several misses rather than one, because gone and
			// unreachable are different: a dropped request or a
			// rollout mid-poll should still retry.
			m.battle.err = msg.err
			m.battle.misses++
			if m.battle.misses >= maxPollMisses {
				m.screen = screenLobby
				m.status = "that battle is over - the server restarted and battles do not survive it"
				m.battle = nil
				return m, fetchLobby(m.bc)
			}
			return m, pollBattle(m.bc, m.battle.id)
		}
		m.battle.err = nil
		m.battle.misses = 0
		start := msg.battle.Version > m.battle.seen
		m.battle.applyBattle(msg.battle)
		cmds := []tea.Cmd{pollBattle(m.bc, m.battle.id)}
		if start {
			cmds = append(cmds, tick())
		}
		return m, tea.Batch(cmds...)

	case startedMsg:
		if msg.err != nil {
			// Back to the lobby with the reason, rather than a battle
			// screen with nothing in it.
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
		// Keep polling only while the lobby is the screen being looked
		// at. Re-arming unconditionally would keep asking during a
		// battle, and stopping on error would make one blip freeze the
		// list for good.
		var again tea.Cmd
		if m.screen == screenLobby {
			again = pollLobby(m.bc)
		}
		if msg.err != nil {
			// A background refresh that fails says nothing: the user
			// did not ask for it, and overwriting the status line would
			// replace something they did ask for.
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
		// While the filter is open every key belongs to it - including
		// "q". Without this check, typing a name containing q quits.
		//
		// Only on a screen that actually shows the list: the filter
		// state belongs to m.list, so on a screen that does not render
		// it a stale Filtering state would swallow every key with
		// nothing on screen to explain why.
		if m.screenUsesList() && m.list.FilterState() == list.Filtering {
			break
		}
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.handleKey(msg)
	}

	// The list is updated on every screen that SHOWS it, which is
	// browse and the team picker - teamView renders the same m.list.
	//
	// It used to be browse only, so on the picker the list was drawn
	// but never updated. Pressing "/" opened its filter, the guard
	// above then routed every following key to the list, and this
	// return threw them away: the filter could not receive text and
	// escape could not close it, so the screen ate input until the
	// program was killed.
	//
	// Which screen is DISPLAYED decides what to draw; it must not
	// decide whether a component that is on screen gets its messages.
	// Those are separate questions, and answering them in two places is
	// what let them disagree.
	if !m.screenUsesList() {
		return m, nil
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// screenUsesList reports whether the current screen renders m.list.
//
// One place both Update and View can agree on, rather than a condition
// restated at each site - restating it is how the picker ended up
// drawing a list it never updated.
func (m model) screenUsesList() bool {
	return m.screen == screenBrowse || m.screen == screenTeam
}

// handleKey dispatches on the active screen, which is the pattern the
// upstream views example uses.
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

var (
	// listStyle frames the left pane. Its frame is subtracted from the
	// pane width before the list is sized, via GetFrameSize.
	listStyle = lipgloss.NewStyle()

	// Fixed-width columns for the moves table. Set on the style rather
	// than with fmt verbs, because these strings carry ANSI escapes and
	// only lipgloss measures their DISPLAY width.
	moveNameStyle  = lipgloss.NewStyle().Width(18)
	movePowerStyle = lipgloss.NewStyle().PaddingLeft(2)

	labelStyle  = lipgloss.NewStyle().Faint(true)
	nameStyle   = lipgloss.NewStyle().Bold(true)
	detailStyle = lipgloss.NewStyle().Padding(1, 2)
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Padding(1, 2)
)

// typeColors are the conventional Pokemon type colours, so a fire type
// reads as fire at a glance rather than as one more line of text.
var typeColors = map[string]string{
	"normal": "249", "fire": "202", "water": "39", "electric": "220",
	"grass": "78", "ice": "87", "fighting": "124", "poison": "127",
	"ground": "179", "flying": "147", "psychic": "205", "bug": "112",
	"rock": "137", "ghost": "97", "dragon": "63", "dark": "240",
	"steel": "145", "fairy": "218",
}

func typeBadge(t string) string {
	c, ok := typeColors[t]
	if !ok {
		c = "245"
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("232")).
		Background(lipgloss.Color(c)).
		Padding(0, 1).
		Render(t)
}

// typeBadgePadded is typeBadge in a fixed-width column, for tables where
// the next column has to line up. The PADDING is added after rendering,
// as plain spaces, so the badge's background stops where the word does.
func typeBadgePadded(t string, width int) string {
	b := typeBadge(t)
	if pad := width - lipgloss.Width(b); pad > 0 {
		return b + strings.Repeat(" ", pad)
	}
	return b
}

// detail renders the right-hand pane for the highlighted Pokemon.
func (m model) detail() string {
	sel, ok := m.list.SelectedItem().(item)
	if !ok {
		return detailStyle.Render(labelStyle.Render("no selection"))
	}
	p := sel.p

	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n\n", nameStyle.Render(p.Name), labelStyle.Render(fmt.Sprintf("#%03d", p.ID)))

	badges := make([]string, len(p.Types))
	for i, t := range p.Types {
		badges[i] = typeBadge(t)
	}
	fmt.Fprintf(&b, "%s\n\n", strings.Join(badges, " "))

	// Decimetres and hectograms are what the upstream Pokedex reports;
	// convert, because nobody thinks in hectograms.
	fmt.Fprintf(&b, "%s %.1f m\n", labelStyle.Render("height"), float64(p.Height)/10)
	fmt.Fprintf(&b, "%s %.1f kg\n\n", labelStyle.Render("weight"), float64(p.Weight)/10)

	if len(p.Moves) > 0 {
		b.WriteString(labelStyle.Render("moves") + "\n")
		for _, mv := range p.Moves {
			power := "—"
			if mv.Power > 0 {
				power = fmt.Sprintf("%d", mv.Power)
			}
			// lipgloss Width, not fmt's %-10s: a styled badge carries ANSI
			// escapes, and fmt pads to BYTE count, so every badge pushed
			// the column a different distance and the rows came out
			// ragged. lipgloss measures what is actually on screen.
			// The badge is padded by ITS OWN style, not wrapped in a
			// second one: a background colour set on an outer Width()
			// paints the padding too, which is what made these columns
			// start at different places rather than just end at them.
			b.WriteString("  " +
				moveNameStyle.Render(mv.Name) +
				typeBadgePadded(mv.Type, 12) +
				movePowerStyle.Render(power) + "\n")
		}
	}

	// Bounded to the pane, so a long move name cannot spill into the
	// list beside it - lipgloss truncates rather than wrapping into the
	// neighbouring column.
	return detailStyle.Render(b.String())
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
	// Draw on the terminal's alternate buffer, so the shell's scrollback
	// is untouched and comes back when the program exits.
	v.AltScreen = true
	return v
}

// browseView is the original two-pane Pokedex.
func (m model) browseView() string {
	var body string
	switch {
	case m.err != nil:
		// The list is still drawn beside it: an API that is down should
		// not make the whole interface disappear.
		body = errStyle.Render(fmt.Sprintf("could not reach the API\n\n%v", m.err))
	case m.loading:
		body = detailStyle.Render(labelStyle.Render("loading…"))
	default:
		body = m.detail()
	}

	// Width, so the two panes sit at fixed columns: the list renders to
	// whatever its longest row needs, which is narrower than listWidth,
	// and without this the detail pane would slide left and right as the
	// selection changed.
	left := listStyle.Width(listWidth).Render(m.list.View())
	joined := lipgloss.JoinHorizontal(lipgloss.Top, left, body)

	// The team, shown as three slots above the help line. A die for a
	// slot nobody picked, because the server fills those at random and
	// that should look like a choice rather than a gap.
	slots := make([]string, 3)
	for i := range slots {
		if i < len(m.team) {
			slots[i] = selStyle.Render(fmt.Sprintf("[%s]", m.team[i]))
		} else {
			slots[i] = labelStyle.Render("[🎲 random]")
		}
	}

	hint := "  enter pick · backspace undo · / filter · q quit"
	if m.bc != nil {
		hint = fmt.Sprintf("  enter pick · ctrl+r battle as %s · b lobby · / filter · q quit", m.trainer)
		// Only offered when there is somewhere to go back TO, so the
		// hint never advertises a key that answers "no battle".
		if m.lastBattle != "" {
			hint = fmt.Sprintf("  enter pick · g back to battle · ctrl+r battle as %s · b lobby · / filter · q quit", m.trainer)
		}
	}
	return joined + "\n  team " + strings.Join(slots, " ") + "\n" + labelStyle.Render(hint)
}
