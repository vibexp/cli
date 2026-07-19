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

3. **Register it** in `internal/cli/root.go`:
   `root.AddCommand(thingcmd.New(resource.CredResolver(credResolver), getenv))`.
4. **Test it** — an `httptest` server returning a **fabricated** response shape,
   asserting: JSON is byte-identical, table/TSV columns, pagination flags reach
   the query, and (for scoped commands) missing scope → exit 2. See
   `internal/cli/identity_test.go`.
5. **Verify against staging** per the epic policy — every `--format`, a `--jq`
   expression, piped TSV, and pagination flags on real data. Never print or
   commit staging URLs/keys.

## Conventions

- **Gate on `permissions`, never `role`** — team/permission display and any
  access decision use the `permissions` array.
- **`{team}` / scope** resolves via `api.Team(rt)` / `api.Project(rt)`
  (flag > env > context); a missing required scope is a **usage error (exit 2)**.
- **Column paths are gojq expressions** applied per row — you can transform
  inline (e.g. `.permissions | join(",")`).
- **Fabricated test data only** — never capture a real deployment's response.
