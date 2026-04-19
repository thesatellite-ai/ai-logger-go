package cli

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/khanakia/ai-logger/internal/web"
	"github.com/spf13/cobra"
)

// newUICmd registers `ailog ui` — launches the embedded web server,
// optionally opening a browser. Default bind is 127.0.0.1 so no
// network exposure unless the user explicitly opts in.
func newUICmd() *cobra.Command {
	var (
		addr     string
		port     int
		noOpen   bool
		allowNet bool
	)
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Launch the local web UI for browsing and searching entries",
		Long: `Starts a small embedded HTTP server (default: http://127.0.0.1:8090)
and opens it in your default browser. Stop with Ctrl-C.

By default the server only binds to 127.0.0.1 — pass --allow-network to
expose it on the LAN (no auth, do this only on trusted networks).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Resolve bind address: --addr wins, else 127.0.0.1:<port>.
			bind := addr
			if bind == "" {
				bind = fmt.Sprintf("127.0.0.1:%d", port)
			}
			if !allowNet && !strings.HasPrefix(bind, "127.") && !strings.HasPrefix(bind, "localhost") {
				return fmt.Errorf("refusing to bind to %s without --allow-network (no auth)", bind)
			}

			s, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()

			srv := web.New(s, bind)
			fmt.Fprintf(cmd.OutOrStdout(), "ailog ui → http://%s\n", bind)
			fmt.Fprintln(cmd.OutOrStdout(), "Press Ctrl-C to stop.")

			if !noOpen {
				go openBrowserWhenReady("http://" + bind)
			}
			return srv.Run()
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "", "explicit host:port bind (overrides --port)")
	cmd.Flags().IntVar(&port, "port", 8090, "port to listen on (binds to 127.0.0.1)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "don't auto-open the browser")
	cmd.Flags().BoolVar(&allowNet, "allow-network", false, "allow non-localhost binds (no auth — use carefully)")
	return cmd
}

// openBrowserWhenReady polls /healthz, then opens the URL in the
// system default browser. Best-effort; ignores any errors so the
// server keeps running even if browser launch fails.
func openBrowserWhenReady(url string) {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		req, _ := http.NewRequestWithContext(ctx, "GET", url+"/healthz", nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	openInBrowser(url)
}

// openInBrowser shells out to the OS's "open the default browser" verb.
// macOS: open. Linux: xdg-open. Windows: rundll32 url.dll.
func openInBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	_ = cmd.Start()
}
