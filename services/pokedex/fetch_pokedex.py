#!/usr/bin/env python3
"""Rebuild pokedex.json from pokeapi.co.

The dataset is embedded rather than fetched at runtime, so refreshing it
is a deliberate act: run this, review the diff, commit. See pokedex.go.

    python3 fetch_pokedex.py            # rewrites pokedex.json
    python3 fetch_pokedex.py --limit 5  # a quick subset, for trying it

Kept in Python so it needs nothing installed: PokeAPI is JSON over HTTP
and the standard library covers it. This is a one-off tool, not part of
the service.
"""

import argparse
import json
import re
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

API = "https://pokeapi.co/api/v2"
OUT = Path(__file__).with_name("pokedex.json")

# How many Pokemon the dex holds. The first 100 is Kanto's opening run,
# which is what the service has always shipped.
DEX_SIZE = 100

# How many moves each Pokemon carries. Enough to make a battle a choice
# rather than a formality, few enough to fit a selection UI.
MOVES_PER_POKEMON = 6

# Flavour text is per game version, and the newest reads best: modern
# entries are sentence case, where Gen 1 shouts "this POKéMON". Ordered
# newest first; the first version with an English entry wins.
VERSION_PREFERENCE = [
    "scarlet", "violet", "legends-arceus", "brilliant-diamond",
    "shining-pearl", "sword", "shield", "lets-go-pikachu",
    "lets-go-eevee", "ultra-sun", "ultra-moon", "sun", "moon",
    "omega-ruby", "alpha-sapphire", "x", "y", "black-2", "white-2",
    "black", "white", "heartgold", "soulsilver", "platinum", "diamond",
    "pearl", "firered", "leafgreen", "emerald", "ruby", "sapphire",
    "crystal", "gold", "silver", "yellow", "blue", "red",
]


# PokeAPI rejects urllib's default User-Agent with a 403. Identifying
# the script is both what unblocks it and the polite thing to do against
# a free API.
USER_AGENT = "pokedex-fetch/1.0 (github.com/ChristopherScot/pokemon)"


def get(url: str, attempts: int = 4) -> dict:
    """Fetch one JSON document, retrying on transient failures.

    PokeAPI is free and occasionally rate limits. Backing off and
    retrying costs a few seconds; failing the whole run at Pokemon 87
    costs the run.
    """
    req = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    for attempt in range(attempts):
        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                return json.load(resp)
        except (urllib.error.URLError, TimeoutError, json.JSONDecodeError) as err:
            if attempt == attempts - 1:
                raise SystemExit(f"giving up on {url}: {err}")
            wait = 2 ** attempt
            print(f"  retrying {url} in {wait}s ({err})", file=sys.stderr)
            time.sleep(wait)
    raise AssertionError("unreachable")


def clean(text: str) -> str:
    """Flatten PokeAPI's flavour text into one line.

    The strings carry the games' own line breaks and a form feed where
    the text box paged: "A strange seed was\\nplanted on its\\nback at
    birth.\\x0cThe plant sprouts". Rendering that verbatim in a web page
    or a TUI puts breaks in arbitrary places, so they collapse to
    spaces.
    """
    return re.sub(r"\s+", " ", text.replace("\x0c", " ")).strip()


def english(entries: list) -> list:
    return [e for e in entries if e["language"]["name"] == "en"]


def pick_flavor(entries: list) -> str:
    """The newest English flavour text, by VERSION_PREFERENCE."""
    by_version = {}
    for e in english(entries):
        by_version.setdefault(e["version"]["name"], e["flavor_text"])
    for version in VERSION_PREFERENCE:
        if version in by_version:
            return clean(by_version[version])
    # A version this script has never heard of is better than nothing.
    return clean(next(iter(by_version.values()))) if by_version else ""


def move_flavor(entries: list) -> str:
    """Moves carry version_group rather than version, so preference
    ordering does not apply cleanly. The last entry is the most recent
    the API returns."""
    en = english(entries)
    return clean(en[-1]["flavor_text"]) if en else ""


def fetch_move(name: str) -> dict:
    m = get(f"{API}/move/{name}")
    effects = english(m["effect_entries"])
    return {
        "name": name,
        "type": m["type"]["name"],
        "power": m["power"] or 0,
        "description": move_flavor(m["flavor_text_entries"]),
        # What the move actually does, where the flavour text says how
        # it looks. Templated effects carry "$effect_chance"; the moves
        # here are simple enough that it rarely appears, and leaving the
        # token in is more honest than guessing the number.
        "effect": clean(effects[0]["short_effect"]) if effects else "",
        # A miss is only possible if accuracy is known. null means the
        # move cannot miss (Swift), which is not the same as 100.
        "accuracy": m["accuracy"],
        "pp": m["pp"],
        # physical moves read attack/defense, special ones read the
        # special stats. status moves deal no damage at all.
        "damageClass": m["damage_class"]["name"],
    }


def fetch_pokemon(dex_id: int, move_cache: dict) -> dict:
    p = get(f"{API}/pokemon/{dex_id}")
    s = get(f"{API}/pokemon-species/{dex_id}")

    stats = {st["stat"]["name"]: st["base_stat"] for st in p["stats"]}
    genera = [g["genus"] for g in s["genera"] if g["language"]["name"] == "en"]

    # The first MOVES_PER_POKEMON in the order PokeAPI returns, which is
    # what the dataset has always held - not sorted, not filtered by how
    # the move is learned. Reordering here would silently rewrite every
    # Pokemon's moveset, and the battle picks from this list.
    #
    # A handful of Pokemon have fewer: Weedle knows four moves in total.
    #
    # Names, not objects. Every move's full record lives once in the
    # top-level catalogue - tackle is known by dozens of Pokemon, and
    # inlining its description in each would be dozens of copies to
    # keep in step.
    moves = []
    for entry in p["moves"][:MOVES_PER_POKEMON]:
        name = entry["move"]["name"]
        if name not in move_cache:
            move_cache[name] = fetch_move(name)
        moves.append(name)

    # Everything this Pokemon can learn by any method, in PokeAPI's
    # order. Bulbasaur has 86 and Mewtwo 167, which is why they are
    # names here and served by their own endpoint rather than inlined
    # on every Pokemon in a list response.
    learnable = []
    for entry in p["moves"]:
        name = entry["move"]["name"]
        if name not in move_cache:
            move_cache[name] = fetch_move(name)
        learnable.append(name)

    return {
        "id": p["id"],
        "name": p["name"],
        "types": [t["type"]["name"] for t in p["types"]],
        "height": p["height"],
        "weight": p["weight"],
        "description": pick_flavor(s["flavor_text_entries"]),
        # "Seed Pokémon" - the games' one-line label, which reads well
        # under the name.
        "genus": genera[0] if genera else "",
        # Where it lives: grassland, cave, sea. Null upstream for
        # species the games never placed, so it can be empty.
        "habitat": (s.get("habitat") or {}).get("name", ""),
        "baseHp": stats["hp"],
        "baseAttack": stats["attack"],
        "baseDefense": stats["defense"],
        "baseSpecialAttack": stats["special-attack"],
        "baseSpecialDefense": stats["special-defense"],
        "baseSpeed": stats["speed"],
        "sprite": p["sprites"]["front_default"],
        # Several hundred pixels rather than 96, for anything that wants
        # to show a Pokemon larger than a list row.
        "artwork": (p["sprites"].get("other", {})
                    .get("official-artwork", {})
                    .get("front_default") or ""),
        "evolvesFrom": (s.get("evolves_from_species") or {}).get("name", ""),
        "legendary": s["is_legendary"] or s["is_mythical"],
        "moves": moves,
        "learnableMoves": learnable,
    }


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--limit", type=int, default=DEX_SIZE,
                    help=f"how many Pokemon to fetch (default {DEX_SIZE})")
    ap.add_argument("--out", type=Path, default=OUT)
    args = ap.parse_args()

    move_cache: dict = {}
    dex = []
    for dex_id in range(1, args.limit + 1):
        entry = fetch_pokemon(dex_id, move_cache)
        dex.append(entry)
        print(f"{dex_id:3d}/{args.limit} {entry['name']}", file=sys.stderr)

    # Sorted so a re-fetch produces the same bytes when nothing upstream
    # changed, which is what makes the diff worth reviewing.
    doc = {
        "pokemon": dex,
        "moves": [move_cache[k] for k in sorted(move_cache)],
    }
    args.out.write_text(json.dumps(doc, indent=2, ensure_ascii=False) + "\n")
    print(f"\nwrote {args.out} - {len(dex)} Pokemon, "
          f"{len(move_cache)} moves in the catalogue", file=sys.stderr)


if __name__ == "__main__":
    main()
