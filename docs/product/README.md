# weeb-box product documentation

Product truth for weeb-box. Code and contributor truth live in [`AGENTS.md`](../../AGENTS.md) — this directory defines *what the product is*, not how the code is built.

## Reading order for a new developer

1. [overview.md](overview.md) — what weeb-box is and will never be.
2. [features.md](features.md) — every feature, its platforms, its milestone, its status.
3. [structure.md](structure.md) — modules, information architecture, on-disk contracts.
4. [ui-design.md](ui-design.md) — every screen, every state, every input.
5. [sync.md](sync.md) — the cross-device contract.
6. [roadmap.md](roadmap.md) — sequencing and "done when".
7. [decisions.md](decisions.md) — why anything is the way it is.
8. [operations.md](operations.md) — runbooks for the parts designed to break.

## The doc set

| File | Owns |
|---|---|
| [overview.md](overview.md) | Identity and boundaries; the only doc that may define scope or add a non-goal |
| [features.md](features.md) | Existence: platform availability, milestone, status — the only place a feature's status appears |
| [structure.md](structure.md) | The shape: modules, IA per form factor, all on-disk format contracts |
| [ui-design.md](ui-design.md) | The experience: layout, states, input per form factor; owns the theme token table |
| [sync.md](sync.md) | The cloud contract: Firestore model, field semantics, conflict rules, never-syncs list |
| [decisions.md](decisions.md) | Rationale, once per decision; supersession adds a pointer, never edits history |
| [roadmap.md](roadmap.md) | Milestones, acceptance criteria, the deferred bucket |
| [operations.md](operations.md) | Runbooks: MegaPlay, providers, Firebase, releases |

## The no-gap rule

1. **Every cross-reference resolves.** References name a file + section inside this directory (or AGENTS.md for code reality). "As discussed" or "see the chat plan" is banned.
2. **Every feature has exactly one status**, stated once in [features.md](features.md); every other doc links to its row. Status flips land in features.md's own [change log](features.md#feature-status-change-log), never in a second table.
3. **Every decision has exactly one rationale**, in [decisions.md](decisions.md). Other docs state the *what* and link the *why*. Superseding a decision adds a pointer instead of rewriting history, so there is never both an old reason and a new one in circulation.
4. **Any future change edits the owning doc in the same commit** as the code. A doc that disagrees with code is stale by definition; fixing it is part of the change, not a follow-up.

## Keeping docs honest

- **README.md** — owns the map and the rules; contains zero product facts.
- **overview.md** — owns identity and boundaries; no other doc may redefine scope or add a non-goal.
- **features.md** — owns existence (platform × milestone × status); everything else links to a row here.
- **structure.md** — owns shape and all on-disk contracts; the SQLite schema itself stays in AGENTS.md (code truth).
- **ui-design.md** — owns layout, states, input, and the Carbon token table; never states what is shipped where — links features.md.
- **sync.md** — owns the cloud contract, including the security rules' content.
- **decisions.md** — owns rationale; the latest un-superseded entry is the only current reason.
- **roadmap.md** — owns sequencing and acceptance criteria; milestone contents reference features.md rows.
- **operations.md** — owns runbooks and procedures; never re-specifies product behavior — links the owning doc.

## Relationship to AGENTS.md

AGENTS.md is how the code is built; this directory is what the product is. A change that alters user-visible behavior updates the owning doc here (and AGENTS.md where code structure changed) in the same commit.

## Doc conventions

Status vocabulary is owned by [features.md](features.md#how-to-read-this) and only linked elsewhere: `shipped-CLI`, `planned-M{n}`, `v1.x`, `rejected`. Cross-references name file + section, e.g. "see [ui-design.md § Player](ui-design.md#player)".
