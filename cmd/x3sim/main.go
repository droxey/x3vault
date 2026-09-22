package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/droxey/x3vault/internal/sim"
)

func main() {
	os.Exit(run())
}

func run() int {
	listen := flag.String("listen", "127.0.0.1:8080", "host:port to bind (loopback default)")
	seedRoot := flag.String("seed-root", "", "optional directory to create at start (e.g. /ereader)")
	allowLAN := flag.Bool("lan", false, "allow non-loopback bind addresses")
	flag.Parse()

	if err := checkListen(*listen, *allowLAN); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	srv := sim.NewServer()
	if *seedRoot != "" {
		parent := "/"
		name := strings.Trim(*seedRoot, "/")
		if strings.Contains(name, "/") || name == "" || name == "." || name == ".." {
			fmt.Fprintln(os.Stderr, "seed-root must be a single path segment")
			return 2
		}
		if err := srv.Store.Mkdir(parent, name); err != nil {
			fmt.Fprintf(os.Stderr, "seed-root: %v\n", err)
			return 1
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpSrv := &http.Server{
		Addr:              *listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 16,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		<-ctx.Done()
		_ = httpSrv.Close()
	}()

	fmt.Fprintf(os.Stderr, "x3sim listening on http://%s\n", *listen)
	fmt.Fprintf(os.Stderr, "open the file browser, then set device.base_url to that URL\n")
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func checkListen(addr string, allowLAN bool) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	if port == "" {
		return fmt.Errorf("listen: missing port")
	}
	if allowLAN {
		return nil
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("refusing non-loopback bind %q (pass --lan to override)", host)
	}
	return nil
}
