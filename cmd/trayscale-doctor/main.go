// Command trayscale-doctor is a small console diagnostic for the Windows
// builds. It exercises the same LocalAPI calls the GUI and tray use, and
// prints exactly where they fail, so a "waiting for Tailscale" problem can be
// told apart from a missing service, an ACL/operator problem, or an API
// mismatch.
//
// Build and run it from a normal console window:
//
//	go run ./cmd/trayscale-doctor
//	Trayscale-Doctor.exe -v
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/ipn"
	"tailscale.com/paths"
)

const watchMask = ipn.NotifyInitialState |
	ipn.NotifyInitialPrefs |
	ipn.NotifyWatchEngineUpdates |
	ipn.NotifyInitialStatus |
	ipn.NotifyPeerChanges

func main() {
	verbose := flag.Bool("v", false, "print full JSON status and watch notification")
	socket := flag.String("socket", "", "LocalAPI socket/pipe path (default: TRAYSCALE_LOCALAPI or the platform default)")
	flag.Parse()

	// Keep a terminal window open when started by double-click.
	if len(os.Args) == 1 && runtime.GOOS == "windows" {
		defer func() {
			fmt.Print("\nPress Enter to close…")
			fmt.Scanln()
		}()
	}

	failed := false
	fmt.Println("Trayscale doctor")
	effectiveSocket := *socket
	if effectiveSocket == "" {
		effectiveSocket = os.Getenv("TRAYSCALE_LOCALAPI")
	}
	if effectiveSocket == "" {
		effectiveSocket = paths.DefaultTailscaledSocket()
	}
	fmt.Printf("  build:      Go %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	fmt.Printf("  local API:  %s\n", effectiveSocket)
	if env := os.Getenv("TRAYSCALE_LOCALAPI"); env != "" {
		fmt.Printf("  TRAYSCALE_LOCALAPI: %s\n", env)
	} else {
		fmt.Println("  TRAYSCALE_LOCALAPI: (unset)")
	}
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	lc := &local.Client{Socket: effectiveSocket}

	// 1. Status is the same call the first UI refresh makes.
	fmt.Println("[1] GET /localapi/v0/status")
	st, err := lc.Status(ctx)
	if err != nil {
		failed = true
		fmt.Printf("    FAILED: %v\n", err)
	} else {
		self := "(none)"
		if st.Self != nil {
			self = st.Self.HostName
			if len(st.Self.TailscaleIPs) > 0 {
				self = fmt.Sprintf("%s (%s)", self, st.Self.TailscaleIPs[0])
			}
		}
		fmt.Printf("    OK: daemon=%s state=%s self=%s peers=%d\n",
			st.Version, st.BackendState, self, len(st.Peer))
		if len(st.Health) > 0 {
			fmt.Printf("    health: %s\n", strings.Join(st.Health, "; "))
		}
		if *verbose {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("    ", "  ")
			enc.Encode(st)
		}
	}

	// 2. The watch bus is what keeps the UI updated. This is the exact mask
	// the poller requests, so a 400 here means the daemon and this build
	// disagree about the IPN bus protocol.
	fmt.Println("[2] GET /localapi/v0/watch-ipn-bus")
	watchCtx, watchCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer watchCancel()
	watcher, err := lc.WatchIPNBus(watchCtx, watchMask)
	if err != nil {
		failed = true
		fmt.Printf("    FAILED: %v\n", err)
	} else {
		notify, err := watcher.Next()
		watcher.Close()
		if err != nil {
			failed = true
			fmt.Printf("    FAILED reading first message: %v\n", err)
		} else {
			state := "(unset)"
			if notify.State != nil {
				state = notify.State.String()
			}
			fmt.Printf("    OK: state=%s initialStatus=%t prefs=%t err=%v\n",
				state, notify.InitialStatus != nil, notify.Prefs != nil && notify.Prefs.Valid(), notify.ErrMessage)
			if *verbose {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("    ", "  ")
				enc.Encode(notify)
			}
		}
	}

	// 3. The CLI gives a second opinion, and works around GUI-specific bugs.
	fmt.Println("[3] tailscale CLI")
	cli := findTailscaleCLI()
	if cli == "" {
		fmt.Println("    tailscale.exe not found (checked PATH and Program Files)")
	} else {
		fmt.Printf("    binary: %s\n", cli)
		args := []string{}
		if *socket != "" {
			args = append(args, "--socket="+*socket)
		}
		args = append(args, "status")
		out, err := exec.CommandContext(ctx, cli, args...).CombinedOutput()
		if err != nil {
			fmt.Printf("    FAILED: %v\n", err)
			if len(out) > 0 {
				fmt.Printf("    %s\n", indent(strings.TrimSpace(string(out)), "    "))
			}
		} else {
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				fmt.Printf("    %s\n", line)
			}
		}
	}
	fmt.Println()

	if failed {
		fmt.Println("RESULT: LocalAPI is NOT reachable from this account. The GUI and tray will show this error instead of a status.")
		os.Exit(1)
	}
	fmt.Println("RESULT: LocalAPI works. If the GUI still waits, check that it uses the same socket and account.")
}

func findTailscaleCLI() string {
	if p, err := exec.LookPath("tailscale.exe"); err == nil {
		return p
	}
	if p, err := exec.LookPath("tailscale"); err == nil {
		return p
	}
	if dir := os.Getenv("ProgramFiles"); dir != "" {
		p := filepath.Join(dir, "Tailscale", "tailscale.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
