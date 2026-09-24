package sipprobe

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
	"github.com/your-org/gb28181-simulator/internal/platform/servicectx"
)

// Version carries the ldflags-injected build metadata printed by --version.
type Version struct {
	Version string
	Commit  string
	BuiltAt string
}

// RunCLI is the shared command surface for the sipprobe diagnostic. It backs
// both cmd/sipprobe and the `gb28181-simulator sipprobe` subcommand, so the
// two stay behaviourally identical while each binary remains a thin assembly
// entry point (Change 3 §9).
//
// Wiring goes through servicectx — the logger is provided by the container —
// and everything else is plain flag parsing. Returns the process exit code.
func RunCLI(args []string, ver Version) int {
	fs := flag.NewFlagSet("sipprobe", flag.ContinueOnError)
	bind := fs.String("bind", "", "local bind address, e.g. udp://0.0.0.0:0 (required)")
	sendTo := fs.String("send-to", "", "remote address to send one INVITE to; empty = receive-only mode")
	expectStatus := fs.String("expect-status", "", "expected response status code (100..699); empty = accept any")
	timeout := fs.Duration("timeout", 5*time.Second, "max time to wait for a single inbound message")
	from := fs.String("from", "", "From header URI for the INVITE (default: sip:probe@127.0.0.1)")
	to := fs.String("to", "", "To header URI for the INVITE (default: derived from --send-to)")
	showVersion := fs.Bool("version", false, "print version and exit")

	if err := fs.Parse(args); err != nil {
		// flag already printed the error to stderr
		return ExitUsage
	}

	if *showVersion {
		fmt.Printf("sipprobe %s (commit %s, built %s)\n", ver.Version, ver.Commit, ver.BuiltAt)
		return ExitOK
	}

	// ServiceContext: sipprobe only needs a logger.
	loggerKey := servicectx.NewKey[*logging.Hub]("logger")
	c := servicectx.NewContainer().
		Provide(loggerKey, func() (any, error) {
			if err := logging.Init(logging.Options{
				Level:     logging.LevelInfo,
				AddSource: false,
			}); err != nil {
				return nil, fmt.Errorf("logger init: %w", err)
			}
			return logging.DefaultHub(), nil
		})

	if _, err := c.Build(); err != nil {
		fmt.Fprintf(os.Stderr, "sipprobe: %v\n", err)
		return ExitUsage
	}
	_ = servicectx.MustGet[*logging.Hub](c, loggerKey)

	wantStatus, err := ParseStatus(*expectStatus)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return ExitUsage
	}

	opts := Options{
		Bind:         *bind,
		SendTo:       *sendTo,
		ExpectStatus: wantStatus,
		Timeout:      *timeout,
		From:         *from,
		To:           *to,
	}
	res, code := Run(context.Background(), opts)
	if code == ExitOK {
		res.Print(os.Stdout)
	}
	return code
}
