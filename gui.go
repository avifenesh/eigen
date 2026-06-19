package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/avifenesh/eigen/internal/gui"
)

// runGUICmd starts the graphical Eigen app preview. This first milestone is a
// local browser-hosted shell over the daemon-backed GUI service; the same
// internal/gui.Service is the seam a Wails/Tauri desktop wrapper will bind to.
func runGUICmd(args []string) {
	fs := flag.NewFlagSet("gui", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:0", "local address for the GUI dev server")
	open := fs.Bool("open", true, "open the GUI window")
	noOpen := fs.Bool("no-open", false, "do not open a GUI window")
	browser := fs.Bool("browser", false, "open in the default browser instead of a desktop-style app window")
	_ = fs.Parse(args)
	if *noOpen {
		*open = false
	}
	if !strings.HasPrefix(*addr, "127.0.0.1:") && !strings.HasPrefix(*addr, "localhost:") && !strings.HasPrefix(*addr, "[::1]:") {
		fmt.Fprintln(os.Stderr, "eigen gui: refusing non-local --addr (use 127.0.0.1 or localhost)")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	svc := gui.NewService(ensureDaemon)
	if *open && !*browser && gui.DesktopAvailable {
		if err := gui.RunDesktop(ctx, svc); err != nil {
			fail(fmt.Errorf("gui desktop: %w", err))
		}
		return
	}
	if *open && !*browser && !gui.DesktopAvailable {
		fmt.Fprintln(os.Stderr, "eigen gui: Wails desktop shell not in this binary; opening local browser preview (build with -tags wails for desktop)")
	}
	srv, err := gui.Serve(ctx, svc, gui.ServeOptions{Addr: *addr, Open: *open})
	if err != nil {
		fail(fmt.Errorf("gui: %w", err))
	}
	fmt.Println("eigen gui:", srv.URL)
	<-ctx.Done()
}
