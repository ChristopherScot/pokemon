// Package battleclient is the part of playing a battle that every Go
// client needs: where the trainer identity is stored, the typed calls
// against the generated API, and the turn-legality check that keeps a
// client from offering a move the server will reject.
//
// The CLI, the TUI and the Gio app all import it, so anything here is
// shared by three front ends - which is the point, and the reason a
// fourth copy of the rules is not needed.
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

const PollInterval = time.Second

const LobbyPollInterval = 3 * time.Second

var ErrNoIdentity = errors.New("no trainer registered; run register first")

var ErrStaleIdentity = errors.New("this trainer is no longer registered; run register again")

type Identity struct {
	Name  string `json:"name"`
	Token string `json:"token"`
	API   string `json:"api"`
}

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
		return Identity{}, ErrNoIdentity
	}
	if id.Token == "" {
		return Identity{}, ErrNoIdentity
	}
	return id, nil
}

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

func (c *Client) MyTurn(b *api.Battle) bool {
	return b.Status == api.BattleStatusActive && b.Turn.Value == c.Name
}

func (c *Client) SideFor(b *api.Battle) (mine, theirs api.Side, ok bool) {
	mi, ti, ok := c.SideIndex(b)
	if !ok {
		return mine, theirs, false
	}
	return b.Sides[mi], b.Sides[ti], true
}

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

// MoveUsable reports whether a move can be selected.
//
// Reads usableMoves, which the server computes. The DisabledMove
// fallback is for a battle fetched from a server older than that
// field - it is the previous rule, kept only so a mixed deployment
// degrades rather than lighting up every move.
func MoveUsable(p api.BattlePokemon, moveIdx int) bool {
	if u := p.UsableMoves; len(u) > 0 {
		return moveIdx >= 0 && moveIdx < len(u) && u[moveIdx]
	}
	i, ok := p.DisabledMove.Get()
	return !ok || i != moveIdx
}

// CanAct reports whether a Pokemon may be chosen as the attacker.
func CanAct(p api.BattlePokemon) bool {
	if v, ok := p.CanAct.Get(); ok {
		return v
	}
	return !p.Fainted
}

// CanBeTargeted reports whether a Pokemon is a legal target.
func CanBeTargeted(p api.BattlePokemon) bool {
	if v, ok := p.CanBeTargeted.Get(); ok {
		return v
	}
	return !p.Fainted
}

var (
	ErrBattleOver  = errors.New("this battle is over")
	ErrNotActive   = errors.New("this battle has not started")
	ErrNotYourTurn = errors.New("not your turn")
	ErrNoSuchMon   = errors.New("no such pokemon")
	ErrNoSuchMove  = errors.New("no such move")
	ErrFainted     = errors.New("that pokemon has fainted")
	ErrTargetDown  = errors.New("that target has already fainted")
	ErrDisabled    = errors.New("that move is disabled this turn")
)

const TeamSize = 3

type Turn struct {
	Attacker int
	Move     int
	Target   int
}

// CheckTurn reports whether a turn is legal, so a client can refuse a
// tap instead of sending a request it knows will 409.
//
// It READS the server's answer rather than recomputing it. The
// previous version re-implemented all nine of takeTurn's checks, in
// the same order, with its own error values - two copies of the rules
// that a test existed solely to hold together, and which the
// TypeScript client could not share at all because it cannot import
// Go. The server now publishes canAct, canBeTargeted and usableMoves
// in the battle itself.
func (c *Client) CheckTurn(b *api.Battle, t Turn) error {
	switch b.Status {
	case api.BattleStatusFinished:
		return ErrBattleOver
	case api.BattleStatusActive:
	default:
		return ErrNotActive
	}
	mine, theirs, ok := c.SideFor(b)
	if !ok || !c.MyTurn(b) {
		return ErrNotYourTurn
	}

	// Index checks stay: they guard the slice access below, and an
	// out-of-range index is a client bug rather than a game rule.
	if t.Attacker < 0 || t.Attacker >= len(mine.Team) {
		return fmt.Errorf("%w: you have no pokemon %d", ErrNoSuchMon, t.Attacker+1)
	}
	if t.Target < 0 || t.Target >= len(theirs.Team) {
		return fmt.Errorf("%w: they have no pokemon %d", ErrNoSuchMon, t.Target+1)
	}

	// Order matches takeTurn's: a fainted attacker is reported before
	// a bad move index, so the two agree on the FIRST reason a turn
	// is illegal and not merely on whether it is.
	attacker := mine.Team[t.Attacker]
	if !CanAct(attacker) {
		return fmt.Errorf("%w: %s", ErrFainted, attacker.Name)
	}
	if t.Move < 0 || t.Move >= len(attacker.Moves) {
		return fmt.Errorf("%w: %s has no move %d", ErrNoSuchMove, attacker.Name, t.Move+1)
	}

	if target := theirs.Team[t.Target]; !CanBeTargeted(target) {
		return fmt.Errorf("%w: %s", ErrTargetDown, target.Name)
	}
	if !MoveUsable(attacker, t.Move) {
		return fmt.Errorf("%w: %s", ErrDisabled, attacker.Moves[t.Move].Name)
	}
	return nil
}

func IdentityAdvice(err error) string {
	switch {
	case errors.Is(err, ErrNoIdentity):
		return "no trainer registered yet — register to start battling"
	case errors.Is(err, ErrStaleIdentity):
		return "the server no longer knows this trainer (it restarted) — register again"
	}
	return ""
}
