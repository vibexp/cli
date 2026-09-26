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
| `resource.AddPaginationFlags(cmd)` | binds `--limit`/`--page`/`--offset`/`--all`, returns a `*Pagination` |
| `resource.RunList(cmd, resolve, getenv, ListConfig, page)` | resolve runtime → build path → apply pagination → fetch (every page with `--all`) → render; warns on stderr when the result is partial |
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

   A noun exposing a **second** paginated collection uses
   `resource.NewNamedListCommand("<verb>", …)` — identical in every respect but
   the verb (`team audit` alongside `team list`). `NewListCommand` is that call
   with `"list"`.

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
Filters: &resource.ListFilters{Stale: true, Labels: true},                              // prompts
Filters: &resource.ListFilters{Metadata: true, Stale: true, Labels: true},              // artifacts, blueprints
Filters: &resource.ListFilters{Metadata: true, Tags: true, Stale: true, Labels: true},  // memories
```

| Field | Flag | Query param | Since |
| --- | --- | --- | --- |
| `Metadata` | `--metadata key=value` (repeatable) | `metadata=<JSON containment>` — keys AND, values within a key OR | platform v0.9.0 |
| `Tags` | `--tags <tag>` (repeatable) | folded into `metadata.tags` — memories only | platform v0.9.0 |
| `Stale` | `--stale` | `freshness=stale` | platform v0.11.0 |
| `Labels` | `--labels <label>` (repeatable) | `labels=a,b` — a resource matches when it carries any of them | platform v0.13.0 |

**Opt in only to what the endpoint takes.** `listPrompts` has no `metadata`
param, so `promptcmd` sets only `Stale` and `Labels` — binding `--metadata` there would let
a user narrow a list and receive the unfiltered one, which reads like a real
answer. The same reasoning is why `freshness` is a strict server-side enum
(anything but `stale` is a 400) and why `--stale` is a bool rather than a
`--freshness=<value>` string the user could get wrong.

Discovery for filter authors lives in `vibexp metadata keys|values --type
<artifacts|blueprints|memories>` (`internal/cli/metadatacmd`).

### Writing metadata

Create/update verbs of a noun whose request schema has `metadata` bind
`resource.MetadataFlags` via `resource.AddMetadataFlags(cmd, &meta, update)`
(`--metadata`, `--metadata-json`; plus `--unset-metadata`/`--replace-metadata`
on update). `create` calls `meta.AddTo(payload)`; `update` calls
`meta.AddMerged(ctx, client, itemPath, payload)`, which (via `ForUpdate`) GETs
the item and merges — the server replaces the whole object whenever `metadata`
is sent, so a bare overlay would wipe every key the user did not repeat. Both
are no-ops when no metadata flag was given. `--body-file -` together with
`--metadata-json -` is rejected by the helper itself (it reads the command's
`body-file` flag).

### Writing labels

Create/update verbs of a noun whose request schema has `labels` bind
`resource.LabelFlags` via `resource.AddLabelFlags(cmd, &labels, update)` and
call `labels.AddTo(payload)` — on update **before** the "nothing to update"
check, so `--label` alone counts. It sends `labels` only when `--label` was
given (an update without it never clears them; `--label ""` sends `[]`). The
flag's own value type rejects more than 10 labels or one over 50 characters
while flags are parsed, so it is a usage error (exit 2) before anything is read
or sent, and `AddTo` has no error to return. `promptcmd` predates the helper and keeps its own `--label`.

## Conventions

- **Gate on `permissions`, never `role`** — team/permission display and any
  access decision use the `permissions` array.
- **`{team}` / scope** resolves via `api.Team(rt)` / `api.Project(rt)`
  (flag > env > context); a missing required scope is a **usage error (exit 2)**.
  A slug is looked up over the command's own client, so call them only **after**
  `resource.RuntimeAndClient` / `resource.Client` (a list's `PathFor` already runs
  after it); called earlier, a slug fails closed with exit 1. Use
  `api.OptionalProject(rt)` for a project that only narrows the results.
- **Column paths are gojq expressions** applied per row — you can transform
  inline (e.g. `.permissions | join(",")`).
- **Fabricated test data only** — never capture a real deployment's response.
