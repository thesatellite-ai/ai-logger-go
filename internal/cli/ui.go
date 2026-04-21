package cli

import (
	"context"
	"fmt"
	"net"
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

			// Resolve the intended bind address. --addr wins, else
			// 127.0.0.1:<port>.
			bind := addr
			if bind == "" {
				bind = fmt.Sprintf("127.0.0.1:%d", port)
			}
			if !allowNet && !strings.HasPrefix(bind, "127.") && !strings.HasPrefix(bind, "localhost") {
				return fmt.Errorf("refusing to bind to %s without --allow-network (no auth)", bind)
			}

			// Bind the listener here (rather than letting http.Server
			// ListenAndServe do it) so we can fall through to an
			// OS-assigned port on conflict without a TOCTOU race.
			//
			// Explicit --port / --addr is honored strictly — a bind
			// failure surfaces as an error instead of silently using a
			// different port, because "I asked for 9000 and got 9001"
			// is the most confusing UX possible.
			portExplicit := cmd.Flags().Changed("port") || cmd.Flags().Changed("addr")
			l, err := net.Listen("tcp", bind)
			if err != nil {
				if portExplicit {
					return fmt.Errorf("bind %s: %w", bind, err)
				}
				// Defaults in use — ask the kernel for any free port.
				// Same host, just :0 for the port.
				host, _, _ := net.SplitHostPort(bind)
				l, err = net.Listen("tcp", net.JoinHostPort(host, "0"))
				if err != nil {
					return fmt.Errorf("bind %s: %w", host+":0", err)
				}
				actual := l.Addr().(*net.TCPAddr).Port
				fmt.Fprintf(cmd.OutOrStdout(),
					"port %d in use → using %d instead\n", port, actual)
				bind = net.JoinHostPort(host, fmt.Sprintf("%d", actual))
			}

			s, err := openStore(ctx)
			if err != nil {
				_ = l.Close()
				return err
			}
			defer func() { _ = s.Close() }()

			srv := web.New(s, bind)
			fmt.Fprintf(cmd.OutOrStdout(), "ailog ui → http://%s\n", bind)
			fmt.Fprintln(cmd.OutOrStdout(), "Press Ctrl-C to stop.")

			if !noOpen {
				go openBrowserWhenReady("http://" + bind)
			}
			return srv.RunListener(l)
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
