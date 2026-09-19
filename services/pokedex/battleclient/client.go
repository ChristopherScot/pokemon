// Package battleclient is the part of playing a battle that every Go
// client needs and none of them should write twice.
//
// It sits beside the generated API client rather than inside a CLI or a
// TUI, because both import services/pokedex already: the CLI and the TUI
// are separate modules that each `replace` to it, so a package here is
// importable from both with no new wiring.
//
// What it does NOT do is render. A CLI prints lines and a TUI owns a
// frame; one abstraction over both suits neither. This package deals in
// state and transitions, and leaves the drawing to the caller.
//
// That was not true for a while: StageLabel, Conditions, EventIcon and
// IdentityAdvice lived here and returned display strings, emoji and an
// English sentence. They are in battletext now, and this comment is a
// promise again rather than an aspiration.
//
// MoveUsable stays, despite reading as presentation at both call sites.
// It answers "would this be a 409?" - a protocol precondition, and the
// reason a client must not offer a disabled move.
package battleclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// DefaultAPI is the deployed Pokedex.
const DefaultAPI = "https://pokemon.home.chrisscotmartin.com/api"

// PollInterval is how often Watch asks for new state.
//
// Battles are turn-based and a human takes seconds to decide, so a
// tighter loop would spend requests to shave latency nobody notices.
const PollInterval = time.Second

// LobbyPollInterval is how often the lobby re-reads the waiting list.
//
// Slower than PollInterval because the lobby is a waiting room rather
// than a game in progress: the thing it is watching for is another
// person deciding to open a battle, which happens on human timescales.
const LobbyPollInterval = 3 * time.Second

var ErrNoIdentity = errors.New("no trainer registered; run register first")

// ErrStaleIdentity is a token the server does not recognise: it was
// issued by a process that has since restarted.
//
// Distinct from ErrNoIdentity because the fix is the same command for a
// different reason, and because the stored token is now junk - a client
// that keeps it will fail identically on every command until someone
// works out to re-register. Battles and trainers live in the server's
// memory, so this happens on every deploy, not only on a crash.
var ErrStaleIdentity = errors.New("this trainer is no longer registered; run register again")

// Identity is a trainer name and the token that authorises its moves.
//
// Persisted so a CLI - which exits between commands - can take a second
// turn without registering again. The TUI keeps one process alive and
// does not strictly need this, but sharing it means both clients answer
// "who am I" the same way, and a player can start in one and continue in
// the other.
type Identity struct {
	Name  string `json:"name"`
	Token string `json:"token"`
	API   string `json:"api"`
}

// identityPath keeps the token out of the working directory, where it
// would eventually be committed by someone.
func identityPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pokedex", "trainer.json"), nil
}

// LoadIdentity returns the stored trainer, or ErrNoIdentity.
func LoadIdentity() (Identity, error) {
	path, err := identityPath()
	if err != nil {
		return Identity{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Identity{}, ErrNoIdentity
		}
		return Identity{}, err
	}
	var id Identity
	if err := json.Unmarshal(b, &id); err != nil {
		// A corrupt file is the same as no identity from the caller's
		// side: re-register rather than making them find and delete it.
		return Identity{}, ErrNoIdentity
	}
	if id.Token == "" {
		return Identity{}, ErrNoIdentity
	}
	return id, nil
}

// SaveIdentity writes the trainer, 0600 because the token authorises
// moves on that trainer's behalf.
func SaveIdentity(id Identity) error {
	path, err := identityPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// ClearIdentity removes the stored trainer.
//
// Called when the server rejects the token, because the file is now
// junk: every later command fails the same way, and the CLI cannot tell
// "I have never registered" from "the token I have is dead" without it.
// Removing it makes the next run take the ordinary unregistered path.
//
// A missing file is success - the caller wants it gone, and it is.
func ClearIdentity() error {
	path, err := identityPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Client wraps the generated client with the trainer's token, so callers
// never thread it through by hand and cannot forget it on one call.
type Client struct {
	API   *api.Client
	Token string
	Name  string
}

// New builds a client from a stored or supplied identity.
func New(id Identity) (*Client, error) {
	base := id.API
	if base == "" {
		base = DefaultAPI
	}
	c, err := api.NewClient(base)
	if err != nil {
		return nil, fmt.Errorf("pokedex api at %s: %w", base, err)
	}
	return &Client{API: c, Token: id.Token, Name: id.Name}, nil
}

// Register claims a name and stores the identity that comes back.
func Register(ctx context.Context, apiURL, name string) (Identity, error) {
	if apiURL == "" {
		apiURL = DefaultAPI
	}
	c, err := api.NewClient(apiURL)
	if err != nil {
		return Identity{}, err
	}
	res, err := c.RegisterTrainer(ctx, &api.RegisterTrainer{Name: name})
	if err != nil {
		return Identity{}, err
	}
	switch v := res.(type) {
	case *api.Trainer:
		id := Identity{Name: v.Name, Token: v.Token, API: apiURL}
		return id, SaveIdentity(id)
	case *api.Error:
		return Identity{}, errors.New(v.Message)
	default:
		return Identity{}, fmt.Errorf("unexpected response %T", res)
	}
}

// Create opens a battle with three Pokemon, or a random team when team
// is empty.
//
// An empty team is sent as an ABSENT field, not an empty array: the spec
// says minItems 3, so `"team": []` is a validation error rather than
// "pick for me". A nil slice is what makes ogen omit the field.
func (c *Client) Create(ctx context.Context, team []string) (*api.Battle, error) {
	if len(team) == 0 {
		team = nil
	}
	res, err := c.API.CreateBattle(ctx, &api.CreateBattle{Team: team},
		api.CreateBattleParams{XTrainerToken: c.Token})
	if err != nil {
		return nil, err
	}
	switch v := res.(type) {
	case *api.Battle:
		return v, nil
	case *api.CreateBattleBadRequest:
		return nil, errors.New(v.Message)
	case *api.CreateBattleUnauthorized:
		return nil, fmt.Errorf("%w (%s)", ErrStaleIdentity, v.Message)
	default:
		return nil, fmt.Errorf("unexpected response %T", res)
	}
}

// Join enters a waiting battle, with a random team when team is empty.
func (c *Client) Join(ctx context.Context, id string, team []string) (*api.Battle, error) {
	if len(team) == 0 {
		team = nil
	}
	res, err := c.API.JoinBattle(ctx, &api.JoinBattle{Team: team},
		api.JoinBattleParams{ID: id, XTrainerToken: c.Token})
	if err != nil {
		return nil, err
	}
	switch v := res.(type) {
	case *api.Battle:
		return v, nil
	case *api.JoinBattleBadRequest:
		return nil, errors.New(v.Message)
	case *api.JoinBattleConflict:
		return nil, errors.New(v.Message)
	case *api.JoinBattleNotFound:
		return nil, errors.New(v.Message)
	case *api.JoinBattleUnauthorized:
		return nil, fmt.Errorf("%w (%s)", ErrStaleIdentity, v.Message)
	default:
		return nil, fmt.Errorf("unexpected response %T", res)
	}
}

// Attack takes one turn.
func (c *Client) Attack(ctx context.Context, id string, attacker, move, target int) (*api.Battle, error) {
	res, err := c.API.TakeTurn(ctx, &api.TakeTurn{Attacker: attacker, Move: move, Target: target},
		api.TakeTurnParams{ID: id, XTrainerToken: c.Token})
	if err != nil {
		return nil, err
	}
	switch v := res.(type) {
	case *api.Battle:
		return v, nil
	case *api.TakeTurnConflict:
		return nil, errors.New(v.Message)
	case *api.TakeTurnNotFound:
		return nil, errors.New(v.Message)
	case *api.TakeTurnUnauthorized:
		return nil, fmt.Errorf("%w (%s)", ErrStaleIdentity, v.Message)
	default:
		return nil, fmt.Errorf("unexpected response %T", res)
	}
}

// Get fetches current state.
func (c *Client) Get(ctx context.Context, id string) (*api.Battle, error) {
	res, err := c.API.GetBattle(ctx, api.GetBattleParams{ID: id})
	if err != nil {
		return nil, err
	}
	switch v := res.(type) {
	case *api.Battle:
		return v, nil
	case *api.Error:
		return nil, errors.New(v.Message)
	default:
		return nil, fmt.Errorf("unexpected response %T", res)
	}
}

// Lobby lists open invitations.
func (c *Client) Lobby(ctx context.Context) (*api.WaitingList, error) {
	return c.API.ListWaitingTrainers(ctx)
}

// Watch polls until the battle changes past the version the caller has
// already seen, then returns the new state.
//
// Comparing version rather than diffing state is the whole reason the
// API exposes it: a caller can sit in this loop cheaply and only
// re-render when something actually happened.
func (c *Client) Watch(ctx context.Context, id string, seen int) (*api.Battle, error) {
	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()
	for {
		b, err := c.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if b.Version > seen {
			return b, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// MyTurn reports whether it is this client's move. Every caller needs
// this and it is easy to get subtly wrong by comparing the wrong field.
func (c *Client) MyTurn(b *api.Battle) bool {
	return b.Status == api.BattleStatusActive && b.Turn.Value == c.Name
}

// SideFor splits the battle into this client's side and the opponent's,
// so a caller never indexes Sides by a number it guessed.
//
// Returns false while a battle is still waiting for its second trainer.
func (c *Client) SideFor(b *api.Battle) (mine, theirs api.Side, ok bool) {
	mi, ti, ok := c.SideIndex(b)
	if !ok {
		return mine, theirs, false
	}
	return b.Sides[mi], b.Sides[ti], true
}

// SideIndex is SideFor when the caller needs the positions rather than
// the sides - to index a parallel array of cursors, say.
//
// It exists because callers were recomputing it: the TUI called SideFor
// and then wrote `mineIdx, theirsIdx := 0, 1; if Sides[1].Trainer ==
// name {...}` three lines later, which is the same decision made twice
// and the kind that drifts.
//
// ok is false for a SPECTATOR as well as for a battle still waiting.
// It used to be true: a name matching neither side fell through to the
// "must be side 1" branch and got the two sides back swapped, so
// watching someone else's battle labelled a stranger's team "you".
func (c *Client) SideIndex(b *api.Battle) (mine, theirs int, ok bool) {
	if len(b.Sides) < 2 {
		return 0, 0, false
	}
	switch c.Name {
	case b.Sides[0].Trainer:
		return 0, 1, true
	case b.Sides[1].Trainer:
		return 1, 0, true
	}
	return 0, 0, false
}

// MoveUsable reports whether a move can be selected right now.
//
// A disabled move is a 409 rather than a wasted turn, so a client that
// shows it as available is setting the player up to fail.
func MoveUsable(p api.BattlePokemon, moveIdx int) bool {
	i, ok := p.DisabledMove.Get()
	return !ok || i != moveIdx
}
