package attachmentcmd

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/output"
)

func newUpload(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	var ownerID, ownerType, contentType string
	cmd := &cobra.Command{
		Use:   "upload <file> --owner-id <id>",
		Short: "Upload a file as an attachment",
		Long: "Upload a file, streamed as multipart/form-data so memory stays bounded\n" +
			"regardless of size. The content type is detected from the file (override\n" +
			"with --content-type). --owner-id is required; --owner-type defaults to\n" +
			"\"artifact\".",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if ownerID == "" {
				return exitcode.Usage("--owner-id <id> is required")
			}
			ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
			if err != nil {
				return err
			}
			base, err := basePath(rt)
			if err != nil {
				return err
			}

			f, err := os.Open(args[0])
			if err != nil {
				return exitcode.Usage("open %q: %v", args[0], err)
			}
			defer func() { _ = f.Close() }()
			info, err := f.Stat()
			if err != nil {
				return exitcode.New(exitcode.RuntimeErr, err)
			}
			if info.IsDir() {
				return exitcode.Usage("%q is a directory, not a file", args[0])
			}

			filename := filepath.Base(args[0])
			ct := contentType
			if ct == "" {
				ct, err = detectContentType(f, args[0])
				if err != nil {
					return exitcode.New(exitcode.RuntimeErr, err)
				}
			}

			// Progress is a stderr-only affordance, shown only on a terminal so it
			// never corrupts piped output.
			var body io.Reader = f
			if output.IsTerminal(os.Stderr) {
				body = &progressReader{r: f, total: info.Size(), w: cmd.ErrOrStderr(), name: filename}
			}

			reader, reqContentType := api.Stream("file", filename, ct, body, map[string]string{
				"owner_id":   ownerID,
				"owner_type": ownerType,
			})
			resp, err := client.DoStream(ctx, http.MethodPost, base, reqContentType, reader)
			if err != nil {
				return exitcode.New(exitcode.RuntimeErr, err)
			}
			raw, err := api.ReadBody(resp)
			if err != nil {
				return exitcode.New(exitcode.RuntimeErr, err)
			}
			if cerr := api.Check(resp.StatusCode, raw); cerr != nil {
				return cerr
			}
			return resource.Render(cmd, rt, getenv, raw, &itemSpec)
		},
	}
	cmd.Flags().StringVar(&ownerID, "owner-id", "", "UUID of the owning resource (required)")
	cmd.Flags().StringVar(&ownerType, "owner-type", defaultOwnerType, "type of the owning resource")
	cmd.Flags().StringVar(&contentType, "content-type", "", "override the detected content type")
	return cmd
}

// detectContentType resolves the file's content type: extension first, then a
// 512-byte sniff (rewinding afterwards so the upload still streams the whole
// file).
func detectContentType(f *os.File, path string) (string, error) {
	if ct := mime.TypeByExtension(filepath.Ext(path)); ct != "" {
		return ct, nil
	}
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return "", err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return http.DetectContentType(buf[:n]), nil
}

// progressReader reports upload progress to a terminal as bytes flow through it.
type progressReader struct {
	r       io.Reader
	total   int64
	read    int64
	w       io.Writer
	name    string
	lastPct int
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.read += int64(n)
	if p.total > 0 {
		if pct := int(p.read * 100 / p.total); pct != p.lastPct {
			p.lastPct = pct
			fmt.Fprintf(p.w, "\rUploading %s... %d%%", p.name, pct)
		}
	}
	if err == io.EOF {
		fmt.Fprintln(p.w)
	}
	return n, err
}
