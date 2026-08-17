# Adding a resource command

Curated resource commands (`whoami`, `team list`, `project list`, `memory …`, …)
are built from the shared scaffold in [`internal/cli/resource`](../internal/cli/resource).
A new **read/list** command needs only its **endpoint path** and a **column
spec** — everything else (auth, retries, RFC 7807 errors, `--format`/`--jq`,
pagination) comes from the scaffold.

## The scaffold

| Helper | Purpose |
| --- | --- |
| `resource.CredResolver` | how a command gets the credential store (passed from root) |
| `resource.AddPaginationFlags(cmd)` | binds `--limit`/`--page`/`--offset`, returns a `*Pagination` |
| `resource.RunList(cmd, resolve, getenv, ListConfig, page)` | resolve runtime → build path → apply pagination → fetch → render |
| `resource.GetItem(cmd, resolve, getenv, path, spec)` | single-object fetch + render (e.g. `whoami`) |
| `resource.FetchJSON` / `resource.Render` | lower-level fetch (with `api.Check`) and render, for non-list shapes |

The JSON contract is the **raw response body**: list responses render byte-for-byte
under `--format=json`; the `TableSpec` only drives table/TSV.

## Recipe for a list command

1. **Create the package** `internal/cli/<noun>cmd/<noun>.go` with a `New(resolve
   resource.CredResolver, getenv config.Getenv) *cobra.Command` that adds a
   `list` subcommand.
2. **Write the `list` subcommand.** Bind pagination, then call `RunList` with a
   `ListConfig`:

   ```go
   page := resource.AddPaginationFlags(cmd)
   cmd.RunE = func(cmd *cobra.Command, _ []string) error {
       return resource.RunList(cmd, resolve, getenv, resource.ListConfig{
           PathFor: func(rt *config.Runtime) (string, error) {
               team, err := api.Team(rt) // for team-scoped endpoints; omit otherwise
               if err != nil {
                   return "", err        // missing team → exit 2 with guidance
               }
               return "/api/v1/" + team + "/things", nil
           },
           Spec: output.TableSpec{
               Rows: ".things[]? // .items[]? // .data[]?", // tolerate the list field name
               Columns: []output.Column{
                   {Header: "SLUG", Path: ".slug"},
                   {Header: "NAME", Path: ".name"},
               },
           },
       }, page)
   }
   ```

   A column that every curated resource carries is declared **once** in
   `internal/cli/resource`, not copied per noun — `resource.FreshnessColumn()`
   for the compact list flag and `resource.WithFreshnessDetail(head, tail)` to
   splice the full v0.11.0 freshness block into a detail spec. Add the next
   such field the same way; a literal copied into four noun packages is four
   places to fix when its gojq expression turns out to be subtly wrong.

3. **Register it** in `internal/cli/root.go`:
   `root.AddCommand(thingcmd.New(resource.CredResolver(credResolver), getenv))`.
4. **Test it** — an `httptest` server returning a **fabricated** response shape,
   asserting: JSON is byte-identical, table/TSV columns, pagination flags reach
   the query, and (for scoped commands) missing scope → exit 2. See
   `internal/cli/identity_test.go`.
5. **Verify against staging** per the epic policy — every `--format`, a `--jq`
   expression, piped TSV, and pagination flags on real data. Never print or
   commit staging URLs/keys.

## Server-side filtering on a list command

Set `Filters` on the `ListConfig` and opt into exactly the filters the endpoint
accepts. The shared builder binds those flags and merges them into the query;
filters compose with each other and with pagination.

```go
Filters: &resource.ListFilters{Stale: true},                              // prompts
Filters: &resource.ListFilters{Metadata: true, Stale: true},              // artifacts, blueprints
Filters: &resource.ListFilters{Metadata: true, Tags: true, Stale: true},  // memories
```

| Field | Flag | Query param | Since |
| --- | --- | --- | --- |
| `Metadata` | `--metadata key=value` (repeatable) | `metadata=<JSON containment>` — keys AND, values within a key OR | platform v0.9.0 |
| `Tags` | `--tags <tag>` (repeatable) | folded into `metadata.tags` — memories only | platform v0.9.0 |
| `Stale` | `--stale` | `freshness=stale` | platform v0.11.0 |

**Opt in only to what the endpoint takes.** `listPrompts` has no `metadata`
param, so `promptcmd` sets `Stale` alone — binding `--metadata` there would let
a user narrow a list and receive the unfiltered one, which reads like a real
answer. The same reasoning is why `freshness` is a strict server-side enum
(anything but `stale` is a 400) and why `--stale` is a bool rather than a
`--freshness=<value>` string the user could get wrong.

Discovery for filter authors lives in `vibexp metadata keys|values --type
<artifacts|blueprints|memories>` (`internal/cli/metadatacmd`).

## Conventions

- **Gate on `permissions`, never `role`** — team/permission display and any
  access decision use the `permissions` array.
- **`{team}` / scope** resolves via `api.Team(rt)` / `api.Project(rt)`
  (flag > env > context); a missing required scope is a **usage error (exit 2)**.
- **Column paths are gojq expressions** applied per row — you can transform
  inline (e.g. `.permissions | join(",")`).
- **Fabricated test data only** — never capture a real deployment's response.
