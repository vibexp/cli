// Package statuscmd implements `vibexp status list|create|rename|delete` — the
// team's status catalog per resource type (platform v0.15.0, epic #1263). It is
// what tells a caller which values the --status flags on the resource
// create/update commands accept: the built-in system statuses plus the team's
// own custom ones.
package statuscmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/output"
)

// resourceTypes is the spec's StatusResourceType enum. It is singular, unlike
// the plural --type of `vibexp metadata`.
var resourceTypes = map[string]bool{"prompt": true, "artifact": true, "blueprint": true, "memory": true}

const typeUsage = "--type must be one of: prompt, artifact, blueprint, memory"

var columns = []output.Column{
	{Header: "SLUG", Path: ".slug"},
	{Header: "NAME", Path: ".name"},
	{Header: "SYSTEM", Path: ".is_system"},
	{Header: "LIVE", Path: ".is_live"},
	{Header: "USAGE", Path: ".usage_count"},
	{Header: "ID", Path: ".id"},
}

var listSpec = output.TableSpec{Rows: ".statuses[]?", Columns: columns}
var itemSpec = output.TableSpec{Columns: columns}

// New builds the `status` command group.
func New(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "List and manage the team's statuses per resource type (platform v0.15.0+)",
		Long: "List the statuses a team accepts for a resource type — the built-in system\n" +
			"statuses plus the team's custom ones — and manage the custom ones. These are\n" +
			"the values the --status flag of the create/update commands takes.\n\n" +
			"--type is singular: prompt, artifact, blueprint or memory. Creating,\n" +
			"renaming and deleting need a team owner or admin. Needs platform v0.15.0+.",
	}
	cmd.AddCommand(
		newList(resolve, getenv),
		newCreate(resolve, getenv),
		newRename(resolve, getenv),
		newDelete(resolve, getenv),
	)
	return cmd
}

func newList(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	var typ string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the statuses of a resource type",
		Long: "List every status the team may give a resource of --type: the system\n" +
			"statuses first, then the team's custom ones, each with how many resources\n" +
			"use it. --format=json returns the raw {\"statuses\":[...]} body.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !resourceTypes[typ] {
				return exitcode.Usage(typeUsage)
			}
			ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
			if err != nil {
				return err
			}
			body, err := fetchList(ctx, rt, client, typ)
			if err != nil {
				return err
			}
			return resource.Render(cmd, rt, getenv, body, &listSpec)
		},
	}
	addTypeFlag(cmd, &typ, "(required)")
	return cmd
}

func newCreate(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	var typ, slug, name string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a custom status",
		Long: "Create a custom status for --type. --slug is the value stored on resources\n" +
			"(lowercase, hyphen-separated, immutable); --name is its display name. A team\n" +
			"may have at most 20 custom statuses per resource type.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !resourceTypes[typ] {
				return exitcode.Usage(typeUsage)
			}
			if slug == "" || name == "" {
				return exitcode.Usage("--slug and --name are required for status create")
			}
			ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
			if err != nil {
				return err
			}
			team, err := api.Team(rt)
			if err != nil {
				return err
			}
			payload := map[string]string{"resource_type": typ, "slug": slug, "name": name}
			return resource.SendItem(ctx, cmd, rt, getenv, client, http.MethodPost, collectionPath(team), payload, &itemSpec)
		},
	}
	addTypeFlag(cmd, &typ, "(required)")
	cmd.Flags().StringVar(&slug, "slug", "", "value stored on resources, e.g. in-review (required)")
	cmd.Flags().StringVar(&name, "name", "", "display name, e.g. \"In review\" (required)")
	return cmd
}

func newRename(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	var typ, name string
	cmd := &cobra.Command{
		Use:   "rename <id|slug>",
		Short: "Rename a custom status",
		Long: "Change the display name of a custom status. Its slug never changes. A\n" +
			"slug argument needs --type (a slug is unique per resource type); an id\n" +
			"does not. System statuses cannot be renamed.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if typ != "" && !resourceTypes[typ] {
				return exitcode.Usage(typeUsage)
			}
			if name == "" {
				return exitcode.Usage("--name is required for status rename")
			}
			ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
			if err != nil {
				return err
			}
			id, _, err := resolveID(ctx, rt, client, typ, args[0])
			if err != nil {
				return err
			}
			team, err := api.Team(rt)
			if err != nil {
				return err
			}
			return resource.SendItem(ctx, cmd, rt, getenv, client, http.MethodPatch, itemPath(team, id), map[string]string{"name": name}, &itemSpec)
		},
	}
	addTypeFlag(cmd, &typ, "(required for a slug)")
	cmd.Flags().StringVar(&name, "name", "", "new display name (required)")
	return cmd
}

func newDelete(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	var typ string
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <id|slug>",
		Short: "Delete a custom status",
		Long: "Delete a custom status. Every resource carrying it moves to the type's\n" +
			"default status: draft for prompts, active for everything else. A locked\n" +
			"resource carrying it blocks the delete. A slug argument needs --type; an\n" +
			"id does not. System statuses cannot be deleted.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if typ != "" && !resourceTypes[typ] {
				return exitcode.Usage(typeUsage)
			}
			proceed, err := resource.ConfirmDeletion(cmd, "status "+args[0]+" (its resources move to "+defaultStatus(typ)+")", yes)
			if err != nil {
				return err
			}
			if !proceed {
				cmd.PrintErrln("Aborted.")
				return nil
			}
			_, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
			if err != nil {
				return err
			}
			return deleteStatus(cmd, rt, client, getenv, typ, args[0])
		},
	}
	addTypeFlag(cmd, &typ, "(required for a slug)")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt (required for non-interactive use)")
	return cmd
}

// deleteStatus sends the delete and reports where the status's resources went:
// a one-line stderr summary, or the raw body when --format/--jq asks for data.
func deleteStatus(cmd *cobra.Command, rt *config.Runtime, client *api.RawClient, getenv config.Getenv, typ, ref string) error {
	ctx := cmd.Context()
	id, slug, err := resolveID(ctx, rt, client, typ, ref)
	if err != nil {
		return err
	}
	team, err := api.Team(rt)
	if err != nil {
		return err
	}
	raw, _, err := resource.Do(ctx, client, http.MethodDelete, itemPath(team, id), nil)
	if err != nil {
		return err
	}
	if cmd.Flags().Changed("format") || cmd.Flags().Changed("jq") {
		return resource.Render(cmd, rt, getenv, raw, nil)
	}
	var out struct {
		ReassignedCount int    `json:"reassigned_count"`
		ReassignedTo    string `json:"reassigned_to"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return exitcode.New(exitcode.RuntimeErr, err)
	}
	cmd.PrintErrf("Deleted status %s; %d resources moved to %s.\n", slug, out.ReassignedCount, out.ReassignedTo)
	return nil
}

func addTypeFlag(cmd *cobra.Command, typ *string, note string) {
	cmd.Flags().StringVar(typ, "type", "", "resource type: prompt, artifact, blueprint, or memory "+note)
}

// defaultStatus names where a deleted status's resources go, as the server
// reports it in reassigned_to.
func defaultStatus(typ string) string {
	switch typ {
	case "prompt":
		return "draft"
	case "":
		return "the default status (draft for prompts, active otherwise)"
	default:
		return "active"
	}
}

func collectionPath(team string) string {
	return "/api/v1/" + team + "/statuses"
}

func itemPath(team, id string) string {
	return collectionPath(team) + "/" + url.PathEscape(id)
}

func fetchList(ctx context.Context, rt *config.Runtime, client *api.RawClient, typ string) ([]byte, error) {
	team, err := api.Team(rt)
	if err != nil {
		return nil, err
	}
	q := url.Values{"resource_type": {typ}}
	return resource.FetchJSON(ctx, client, http.MethodGet, collectionPath(team)+"?"+q.Encode())
}

// resolveID turns a rename/delete argument into a custom status id. A UUID is
// used as-is (its label for messages is the UUID itself). Anything else is a
// slug, looked up in the --type catalog: a system status, or no match, is a
// usage error, since only custom statuses can be renamed or deleted.
func resolveID(ctx context.Context, rt *config.Runtime, client *api.RawClient, typ, ref string) (id, label string, err error) {
	if api.IsUUID(ref) {
		return ref, ref, nil
	}
	if !resourceTypes[typ] {
		return "", "", exitcode.Usage("%q is not a status id, so --type is needed to look the slug up (prompt, artifact, blueprint or memory)", ref)
	}
	body, err := fetchList(ctx, rt, client, typ)
	if err != nil {
		return "", "", err
	}
	var list struct {
		Statuses []struct {
			ID       string `json:"id"`
			Slug     string `json:"slug"`
			IsSystem bool   `json:"is_system"`
		} `json:"statuses"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return "", "", exitcode.New(exitcode.RuntimeErr, err)
	}
	for _, s := range list.Statuses {
		if s.Slug != ref {
			continue
		}
		if s.IsSystem {
			return "", "", exitcode.Usage("%s is a system status; system statuses cannot be renamed or deleted", ref)
		}
		return s.ID, s.Slug, nil
	}
	return "", "", exitcode.Usage("no custom %s status with slug %q (see: vibexp status list --type %s)", typ, ref, typ)
}
