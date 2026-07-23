# NORTHSTAR — Family Relationship Edges in the Entity Graph

**Status:** Draft v0.1 — scoping only, no implementation
**Date:** 2026-07-23
**Founder framing, verbatim:** "families as a network linkage entity graph — public marriage
records enrichment."
**Fits into:** `internal/entitygraph` (PersonNode/Edge graph), `cmd/entity-graph` (signal
correlators), `internal/insider` (Form 4 parsing) — and shares a prerequisite with
`docs/northstar/director-network-ticker-discovery.md` (DEF 14A ingestion).

---

## 1. The idea, plainly

Right now the entity graph can tell you two directors sat on the same board together
(`board_co_member`) and it can flag a director whose *own* surname matches a small hardcoded
list of founder-family names. It cannot tell you that two people in the graph are *related to
each other* — married, parent/child, sibling. A real family-relationship layer would let
correlators reason about actual kinship networks (e.g. "the CEO's son-in-law chairs the comp
committee") instead of guessing from a surname. The founder's instinct — enrich this from public
marriage records — is the right problem; §2 and §3 below are about whether that's the right
*source*.

## 2. What we actually have today (checked, not assumed)

- **`family_control` is a same-person surname keyword match, not a relationship.**
  `internal/entitygraph/signals.go` (~line 311) fires it when a director's own canonicalized name
  contains a substring from `Rules.FamilyNameKeywords`. The default list
  (`internal/entitygraph/rules.go` line 141) is exactly four names: `schwab`, `walton`, `mars`,
  `buffett`. It cannot detect a relationship *between two different people* at all — a married
  couple with different surnames on the same board would produce zero signal, and a coincidental
  surname match (e.g. an unrelated "Walton" director) would produce a false positive. There is no
  code path today that even asks "are person A and person B related."
- **`Graph.EdgeType` has exactly one value.** `internal/entitygraph/graph.go` line 55:
  `EdgeBoardCoMember` — built purely from co-appearance in the same 8-K Item 5.07 filing
  (`BuildEdgesFromFiling`). There is no schema, field, or edge type anywhere for a family/kinship
  relationship. Adding one is a schema change, not a config change.
- **Form 4 already carries a real, unparsed signal for this.** SEC's Form 4 XML schema includes
  `<ownershipNature><directOrIndirectOwnership>` and free-text `<footnotes>`, which routinely
  disclose things like "shares held by spouse" or "held by a trust for the benefit of [insider]'s
  children" whenever a reported holding is indirect. `internal/insider/insider.go` parses
  `nonDerivativeTable` transactions (Code P/S conviction trades) but does not touch
  `directOrIndirectOwnership` or `footnotes` at all — confirmed via grep, zero mentions of either
  in the package. This is real, structured, government-mandated disclosure data already inside a
  filing type (`form4-watcher`) this pipeline already ingests. No new ingestion needed for this
  piece.
- **DEF 14A ingestion does not exist yet** — this was already found and documented in the sibling
  northstar `director-network-ticker-discovery.md` (§2): `secwatch` polls 8-K Item 5.07 (vote
  tallies) but has never ingested DEF 14A proxy statements. This matters here too, because Item
  401(d) of SEC Regulation S-K **legally requires** every DEF 14A (and 10-K Part III, where
  incorporated by reference) to disclose "any family relationships" among directors and executive
  officers — in plain prose, usually one short paragraph, either naming the relationship
  explicitly ("X is the son of Y, our Chief Executive Officer") or stating none exist. This is not
  a scraped rumor or a probabilistic inference — it is a mandatory SEC disclosure, sitting in a
  filing type this pipeline is already planning to ingest for an unrelated reason.

## 3. Why public marriage records are the wrong primary source

Public marriage records exist, but as a *pipeline* input they fail the same test the
replay-fragility and director-network northstars both applied honestly to their own ideas:

- **No federal source, no API.** Marriage records are recorded at the county (sometimes state)
  level in the US — over 3,000 counties, each with its own office, its own digitization state
  (many are paper-only or behind an in-person/mail request), and its own access rules. There is no
  EDGAR-equivalent single structured feed to poll the way `secwatch` polls SEC filings.
- **Access is often legally or procedurally restricted**, not just inconvenient. A number of
  states restrict public access to *recent* marriage records for a period (commonly framed as
  identity-theft protection), require a stated purpose, or charge per-record fees — this is
  fundamentally a per-jurisdiction manual-request model, not something a `*-watcher` poll loop can
  be built against.
- **Identity matching is the real risk, and this system already knows that risk is expensive.**
  A marriage record has a name and a date, not a CIK or a ticker. Reconciling "John A. Smith
  married Jane Doe in Cook County, 2003" against "John Smith, director, appears on 3 of our 50
  tracked boards" with any confidence requires additional corroborating fields (age, address,
  parents' names) that most digitized indexes don't expose. Given the entity graph's own measured
  overall accuracy is already in the moderate range (see `docs/northstar/replay-fragility.md`'s
  discussion of the accuracy layer), adding a high-false-positive-risk signal source would be a
  net negative unless matching confidence can be bounded and disclosed per-record — exactly the
  concern `internal/entitygraph/canon.go`'s `NamesMatch` already exists to manage for the
  same-person case, and this is the harder, different-people case.
- **It doesn't scale as an unattended pipeline the way this codebase's other watchers do.** Every
  existing `*-watcher` (secwatch, prwatch, form4-watcher, ...) is a poll loop against one
  well-defined federal API. A marriage-records feature would be ~3,000 different manual or
  semi-manual lookups, which is a fundamentally different shape of work — closer to the "surface
  a candidate for human review" pattern this codebase already uses for vendor/commerce decisions
  (S135/S163) and for the director-network candidate list (§3c there), not a background ingestion
  process.

**Conclusion: don't build a marriage-records pipeline.** Build the family-relationship *edge*
using data sources that are already structured, already mandatory, and already inside filing
types this pipeline ingests or is already planning to ingest — §4.

## 4. The decision

Add a real `family_relationship` edge type to the graph, sourced from two already-authoritative,
machine-parseable disclosure channels instead of external marriage records:

**4a. Form 4 indirect-ownership footnotes (no new ingestion — fastest win).** Extend
`internal/insider` to parse `directOrIndirectOwnership` and `footnotes` on every transaction
already being ingested by `form4-watcher`. Pattern-match footnote text for spouse/family-trust/
child-beneficiary language (needs grounding against a real sample of fetched Form 4 footnotes
before committing to a specific pattern set — same discipline the director-network northstar
required for its DEF 14A bio parser, not invented from imagined phrasing). Emits a
`(insider, related-person-name, relationship-hint)` tuple per match.

**4b. DEF 14A Item 401(d) family-relationship disclosure (shares the DEF 14A ingestion
prerequisite with `director-network-ticker-discovery.md` §3a).** Once DEF 14A ingestion lands
(for either northstar — whichever is picked up first), add a targeted parser for the Item 401(d)
paragraph specifically: usually short, often boilerplate ("There are no family relationships..."),
and when relationships do exist, phrased with a name and relationship term. Same
grounded-against-real-samples discipline as 4a.

**4c. New graph primitive.** Add `EdgeFamilyRelationship` to `internal/entitygraph/graph.go`'s
`EdgeType` enum, with a `Relationship` field on `Edge.Metadata` (e.g. `"spouse"`,
`"parent-child"`, `"sibling"`, `"unspecified-family"` when the source text doesn't disambiguate)
and a `Source` field (`"form4-footnote"` / `"def14a-401d"`) so provenance is always inspectable —
this is a governance/compliance signal, and "where did this claim come from" must be answerable
for any single edge, not just in aggregate.

**4d. Retire (or demote) the four-keyword `family_control` heuristic** once 4a/4b produce real
edges for a ticker — replace the crude same-surname guess with an actual relationship-backed
signal wherever one exists, falling back to the keyword heuristic only where no disclosed
relationship exists yet (keeps existing behavior for tickers this hasn't reached).

## 5. What this explicitly does not do (yet)

- No marriage-records ingestion, bulk or otherwise — rejected as a pipeline shape per §3, not
  merely deferred.
- No new correlator or edge type built yet — design only, same as the director-network northstar's
  own stated status.
- No auto-anything. Family-relationship edges, once built, feed existing correlators
  (`CorrelateFamilyControlEntrenchment` etc.) the same way `board_co_member` edges do today; they
  don't trigger any new automatic action.
- **One narrow exception worth naming, not building:** if the founder ever wants a *specific named
  individual* checked against public marriage records for a one-off research question (not a
  pipeline), that's a manual lookup task for a human or a single Claude Code session with
  `WebSearch`/`WebFetch` — categorically different from an unattended ingestion process, and not
  in scope here.

## 6. Sequencing, if picked up

1. **4a (Form 4 footnote parsing)** — no ingestion prerequisite, ships independently, real value
   even standalone (indirect-ownership disclosure is useful beyond family-linkage too). Start
   here.
2. **DEF 14A ingestion** — shared prerequisite with `director-network-ticker-discovery.md` §3a;
   whichever northstar gets picked up first should build it once, not twice.
3. **4b (Item 401(d) parser)**, grounded against real fetched DEF 14A text once ingestion exists —
   don't write the pattern set before there's real prose to test it against.
4. **4c (new `EdgeFamilyRelationship` primitive)** — small, mechanical, once 4a or 4b produces the
   first real tuples to store.
5. **4d (retire the keyword heuristic)** — last, and only where real edges now cover a ticker.
