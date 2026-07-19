package authcmd

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/logging"
)

func newLogin(resolve StoreResolver) *cobra.Command {
	var withAPIKey bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate a context (API key)",
		Long: "Authenticate the active context with an API key.\n\n" +
			"The key is read from an interactive hidden prompt (TTY) or from stdin\n" +
			"when piped — never as a command-line argument, which would leak into\n" +
			"shell history. The key is validated against the server before it is\n" +
			"stored in ~/.vibexp/credentials.json (0600).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !withAPIKey {
				return exitcode.Usage("specify an auth method: --with-api-key (interactive browser login arrives in a later release)")
			}
			ctx := cmd.Context()
			rt, err := requireRuntime(ctx)
			if err != nil {
				return err
			}
			contextName, err := requireContextName(rt)
			if err != nil {
				return err
			}
			baseURL, err := requireBaseURL(rt)
			if err != nil {
				return err
			}

			key, err := readAPIKey(cmd)
			if err != nil {
				return exitcode.New(exitcode.RuntimeErr, err)
			}
			if key == "" {
				return exitcode.Usage("no API key provided")
			}
			logging.RegisterSecret(key)

			user, err := fetchIdentity(ctx, baseURL, key, rt.Timeout)
			if err != nil {
				return err
			}

			store, err := resolve()
			if err != nil {
				return exitcode.New(exitcode.RuntimeErr, err)
			}
			if err := store.Save(contextName, cred.Entry{Type: cred.TypeAPIKey, APIKey: key}); err != nil {
				return exitcode.New(exitcode.RuntimeErr, err)
			}

			cmd.PrintErrf("Logged in to context %q as %s <%s>.\n", contextName, user.Name, string(user.Email))
			return nil
		},
	}
	cmd.Flags().BoolVar(&withAPIKey, "with-api-key", false, "authenticate with an API key (read from prompt or stdin)")
	return cmd
}

// readAPIKey reads the key from a hidden TTY prompt or, when stdin is piped,
// from a single line of stdin.
func readAPIKey(cmd *cobra.Command) (string, error) {
	in := cmd.InOrStdin()
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		cmd.PrintErr("API key: ")
		b, err := term.ReadPassword(int(f.Fd()))
		cmd.PrintErrln()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
