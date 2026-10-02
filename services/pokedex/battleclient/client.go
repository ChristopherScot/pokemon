// Package battleclient holds trainer identity, typed API calls, and the
// turn-legality check shared by every Go client (CLI, TUI, Gio app).
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

// Join enters a waiting battle; an empty team means "server picks".
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

// MoveUsable reads the server's usableMoves; DisabledMove is the fallback
// for a server older than that field, kept so a mixed deploy degrades.
func MoveUsable(p api.BattlePokemon, moveIdx int) bool {
	if u := p.UsableMoves; len(u) > 0 {
		return moveIdx >= 0 && moveIdx < len(u) && u[moveIdx]
	}
	i, ok := p.DisabledMove.Get()
	return !ok || i != moveIdx
}

// CanAct reads the server's verdict; Fainted is the pre-verdict fallback.
func CanAct(p api.BattlePokemon) bool {
	if v, ok := p.CanAct.Get(); ok {
		return v
	}
	return !p.Fainted
}

// CanBeTargeted reads the server's verdict; Fainted is the pre-verdict fallback.
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

// CheckTurn refuses a turn the server would 409, reading the server's own
// canAct/canBeTargeted/usableMoves verdict rather than recomputing rules.
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

	// Index checks guard the slice access below.
	if t.Attacker < 0 || t.Attacker >= len(mine.Team) {
		return fmt.Errorf("%w: you have no pokemon %d", ErrNoSuchMon, t.Attacker+1)
	}
	if t.Target < 0 || t.Target >= len(theirs.Team) {
		return fmt.Errorf("%w: they have no pokemon %d", ErrNoSuchMon, t.Target+1)
	}

	// Order matches takeTurn's so the two agree on the first reason a turn is illegal.
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
