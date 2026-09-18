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

// loadedMsg carries the result of the initial fetch back onto the event
// loop. An error is a value here, not a panic: the interface has to keep
// running and say what went wrong.
type loadedMsg struct {
	pokemon []api.Pokemon
	err     error
}

type model struct {
	list   list.Model
	client *api.Client

	loading bool
	err     error

	width, height int
}

func newModel(c *api.Client) model {
	// Zero size, as the upstream list examples do. Bubble Tea reads the
	// terminal size at startup and delivers a WindowSizeMsg BEFORE the
	// first render, so the real dimensions always arrive before anything
	// is drawn and a placeholder would only ever be wrong.
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
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

	return model{list: l, client: c, loading: true}
}

// Init kicks off the fetch. Returning a Cmd rather than calling the API
// here is what keeps the first frame instant: the interface draws
// "loading" immediately and fills in when the response arrives.
func (m model) Init() tea.Cmd {
	return m.fetch
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
		return loadedMsg{err: err}
	}
	return loadedMsg{pokemon: res.Pokemon}
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

	case loadedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		items := make([]list.Item, len(msg.pokemon))
		for i, p := range msg.pokemon {
			items[i] = item{p: p}
		}
		return m, m.list.SetItems(items)

	case tea.KeyPressMsg:
		// While the filter is open every key belongs to it - including
		// "q". Without this check, typing a name containing q quits.
		if m.list.FilterState() == list.Filtering {
			break
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
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

	v := tea.NewView(lipgloss.JoinHorizontal(lipgloss.Top, left, body))
	// Draw on the terminal's alternate buffer, so the shell's scrollback
	// is untouched and comes back when the program exits.
	v.AltScreen = true
	return v
}
