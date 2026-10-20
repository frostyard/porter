// Command porterd is the Porter daemon, lodge's porter app
// (https://github.com/frostyard/lodge/blob/main/docs/specs/app-contract.md).
// It serves lodge on its app socket, the one systemd passes or
// /run/lodge/apps/porter.sock under LODGE_ROOT, and its own clients on a
// private socket with --socket. It reaches the other apps' MCP through
// lodge's agent socket, with a token per app kept in its state directory,
// and thinks through lodge's inference socket (docs/design/overview.md).
//
//	porterd [--socket PATH] [--debug]
//	porterd token set APP < token
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/frostyard/lodge/kit/inference"
	"github.com/frostyard/lodge/kit/jsonrpc"
	"github.com/frostyard/lodge/kit/lodgeapp"

	"github.com/frostyard/porter/api"
	"github.com/frostyard/porter/internal/agent"
	"github.com/frostyard/porter/internal/config"
	"github.com/frostyard/porter/internal/rpcserver"
	"github.com/frostyard/porter/internal/service"
	"github.com/frostyard/porter/internal/thread"
	"github.com/frostyard/porter/internal/tools"
)

// ID is the app's id, as its manifest says.
const ID = "porter"

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	if len(os.Args) > 1 && os.Args[1] == "token" {
		err = tokenCmd(os.Args[2:], os.Stdin, os.Stdout)
	} else {
		err = run(ctx, os.Args[1:])
	}
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, "porterd:", err)
		os.Exit(1)
	}
}

// tokenCmd sets an app's token from stdin: porterd token set APP.
func tokenCmd(args []string, in io.Reader, out io.Writer) error {
	if len(args) != 2 || args[0] != "set" {
		return errors.New("usage: porterd token set APP < token (lodgectl token issue NAME --app APP --scope … --agent frostporter)")
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if err := config.SetToken(config.StateDir(), args[1], line); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "porterd: %s's token is set; restart porterd to use it\n", args[1])
	return err
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("porterd", flag.ContinueOnError)
	socket := fs.String("socket", "", "also serve local clients on this private socket")
	debug := fs.Bool("debug", false, "log at debug level")
	if err := fs.Parse(args); err != nil {
		return err
	}
	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	cfg, err := config.Load("")
	if err != nil {
		return err
	}
	loc, err := cfg.Location()
	if err != nil {
		return err
	}
	env, err := lodgeapp.LoadEnv()
	if err != nil {
		return err
	}
	state := config.StateDir()
	if err := os.MkdirAll(state, 0o700); err != nil {
		return err
	}
	store, err := thread.Open(ctx, filepath.Join(state, "thread.db"))
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	var apps []*tools.App
	for _, id := range cfg.Apps {
		tok, err := config.Token(state, id)
		if err != nil {
			return err
		}
		if tok == "" {
			log.Warn("no token for an app: Porter cannot reach it", "app", id, "set", "porterd token set "+id)
			continue
		}
		apps = append(apps, &tools.App{ID: id, Endpoint: lodgeapp.AgentURL(id), HTTP: env.AgentClient(tok), Version: version})
	}
	ai := inference.New(env, ID)
	defer ai.Close()
	ag := &agent.Agent{Store: store, Model: agent.Lodge{C: ai}, Alias: cfg.ChatAlias, Owner: cfg.Owner,
		Location: loc, Log: log}
	var notes *tools.App
	for _, a := range apps {
		if a.ID == "notes" {
			notes = a
		}
	}
	reg := tools.NewRegistry(log, apps, agent.OwnTools(store, loc, notes, nil, ag.ForgetMemory)...)
	defer reg.Close()
	ag.Registry = reg

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	svc := service.New(ctx, store, ag, log)
	defer svc.Wait()

	app, err := newApp(log, svc)
	if err != nil {
		return err
	}
	var private net.Listener
	if *socket != "" {
		if private, err = jsonrpc.ListenWith(*socket, jsonrpc.ListenOptions{Name: "porterd", Logger: log}); err != nil {
			return err
		}
	}
	ln, err := env.Listen(ID)
	if err != nil {
		if private != nil {
			_ = private.Close()
		}
		return fmt.Errorf("lodge socket: %w", err)
	}
	errs := make(chan error, 2)
	go func() { errs <- app.lodge.Serve(ctx, ln) }()
	served := 1
	if private != nil {
		go func() { errs <- app.rpc.Serve(ctx, private, app.router) }()
		served++
	}
	log.Info("serving lodge's "+ID+" app", "socket", ln.Addr().String(), "version", version, "apps", len(apps))
	var first error
	for range served {
		if err := <-errs; err != nil && first == nil {
			first = err
			cancel() // one socket failed: stop the other
		}
	}
	return first
}

// app is the daemon's API, and lodge's socket server for it.
type app struct {
	rpc    *rpcserver.Server
	router *api.Router
	lodge  *lodgeapp.Server
}

// newApp builds the API and the server lodge reaches it through: GET
// /health, and GET /api, a browser session's JSON-RPC.
func newApp(log *slog.Logger, threads api.ThreadService) (*app, error) {
	rpc := rpcserver.New(rpcserver.Options{Name: "porterd " + version, Logger: log})
	router, err := api.NewRouter(api.Services{RPC: rpc, Thread: threads})
	if err != nil {
		return nil, err
	}
	return &app{rpc: rpc, router: router, lodge: &lodgeapp.Server{
		Health: func(context.Context) lodgeapp.Health {
			return lodgeapp.Health{OK: true, Version: version, Protocol: api.Protocol}
		},
		API: func(ctx context.Context, nc net.Conn, _ lodgeapp.Caller) {
			rpc.ServeConn(ctx, nc, router, api.CallerSession)
		},
		Logger: log,
	}}, nil
}
