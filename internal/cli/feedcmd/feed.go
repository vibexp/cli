// Package feedcmd implements `vibexp feed` — the team-collaboration read/write
// loop (list feeds, read items + replies, post items, reply). Reads follow the
// shared resource scaffold (docs/adding-commands.md); the write verbs are small
// bespoke commands sharing a message-input helper.
package feedcmd

import (
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/output"
)

// defaultAuthor labels items/replies this CLI posts when --author is omitted.
const defaultAuthor = "vibexp-cli"

// feedColumns tabulate `feed list`.
var feedColumns = []output.Column{
	{Header: "ID", Path: ".id"},
	{Header: "NAME", Path: ".name"},
	{Header: "DESCRIPTION", Path: ".description"},
	{Header: "UPDATED", Path: ".updated_at"},
}

// itemColumns tabulate `feed items` and single items; excerpt is previewed.
var itemColumns = []output.Column{
	{Header: "ID", Path: ".id"},
	{Header: "AUTHOR", Path: ".ai_assistant_name"},
	{Header: "EXCERPT", Path: ".excerpt[0:80]"},
	{Header: "REPLIES", Path: ".reply_count"},
	{Header: "POSTED", Path: ".posted_at"},
}

var itemSpec = output.TableSpec{Rows: ".", Columns: itemColumns}

// New builds the `feed` command group.
func New(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "feed",
		Short: "Read and post to team feeds",
	}
	cmd.AddCommand(
		newList(resolve, getenv),
		newItems(resolve, getenv),
		newGetItem(resolve, getenv),
		newPost(resolve, getenv),
		newReply(resolve, getenv),
	)
	return cmd
}

// teamBase returns /api/v1/{team}, resolving the team (missing → exit 2).
func teamBase(rt *config.Runtime) (string, error) {
	team, err := api.Team(rt)
	if err != nil {
		return "", err
	}
	return "/api/v1/" + team, nil
}

// readBodyFile reads content from a file, or stdin when path is "-". An empty
// path means "not provided" and returns (nil, nil).
func readBodyFile(cmd *cobra.Command, path string) ([]byte, error) {
	switch path {
	case "":
		return nil, nil
	case "-":
		body, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, exitcode.New(exitcode.RuntimeErr, err)
		}
		return body, nil
	default:
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, exitcode.Usage("read --body-file %q: %v", path, err)
		}
		return body, nil
	}
}

// messageInput resolves the message for a write verb: a positional argument
// wins, otherwise --body-file (a path or '-' for stdin). Supplying both, or
// neither, is a usage error.
func messageInput(cmd *cobra.Command, arg, bodyFile string) (string, error) {
	if arg != "" {
		if bodyFile != "" {
			return "", exitcode.Usage("provide the message as an argument or --body-file, not both")
		}
		return arg, nil
	}
	body, err := readBodyFile(cmd, bodyFile)
	if err != nil {
		return "", err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return "", exitcode.Usage("message is required: pass it as an argument, --body-file <path>, or --body-file -")
	}
	return string(body), nil
}

func newList(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	return resource.NewListCommand(resolve, getenv, "List feeds in the resolved team", resource.ListConfig{
		PathFor: func(rt *config.Runtime) (string, error) {
			base, err := teamBase(rt)
			if err != nil {
				return "", err
			}
			return base + "/feeds", nil
		},
		Spec: output.TableSpec{Rows: resource.ListRows("feeds"), Columns: feedColumns},
	})
}
