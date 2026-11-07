// Command wayveil-dev drives the Wayveil server core from a terminal. It is a
// developer tool; end users get the GUI (Plan 3).
//
//	wayveil-dev install      -host H [-port 22 -user root] -sni D
//	wayveil-dev targets      -host H
//	wayveil-dev check-target -host H -domain D
//	wayveil-dev clients list|add NAME|rm ID|reissue ID|link ID -host H
//	wayveil-dev set-sni      -host H -domain D
//	wayveil-dev uninstall    -host H
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/theuittti/WayVeil/internal/ownerstore"
	"github.com/theuittti/WayVeil/internal/remote"
	"github.com/theuittti/WayVeil/internal/server"
	"github.com/theuittti/WayVeil/internal/sshx"
	"github.com/theuittti/WayVeil/rules"
)

type conn interface {
	remote.Runner
	HostKey() string
	Fingerprint() string
	Close() error
}

type deps struct {
	out          io.Writer
	connect      func(context.Context, sshx.Target) (conn, error)
	readPassword func() (string, error)
	confirm      func(prompt string) bool
	install      func(context.Context, remote.Runner, server.InstallOptions, func(server.StepEvent)) (server.InstallResult, error)
	addClient    func(context.Context, remote.Runner, string) (server.Client, error)
	info         func(context.Context, remote.Runner) (server.ServerInfo, error)
}

func main() {
	stdin := bufio.NewReader(os.Stdin)
	os.Exit(run(os.Args[1:], deps{
		out: os.Stdout,
		connect: func(ctx context.Context, t sshx.Target) (conn, error) {
			return sshx.Connect(ctx, t)
		},
		readPassword: func() (string, error) {
			fmt.Fprint(os.Stderr, "SSH password: ")
			defer fmt.Fprintln(os.Stderr)
			if term.IsTerminal(int(os.Stdin.Fd())) {
				b, err := term.ReadPassword(int(os.Stdin.Fd()))
				return string(b), err
			}
			s, err := stdin.ReadString('\n')
			return strings.TrimRight(s, "\r\n"), err
		},
		confirm: func(prompt string) bool {
			fmt.Fprint(os.Stderr, prompt+" [y/N]: ")
			s, _ := stdin.ReadString('\n')
			return strings.EqualFold(strings.TrimSpace(s), "y")
		},
		install:   server.Install,
		addClient: server.AddClient,
		info:      server.Info,
	}))
}

func run(args []string, d deps) int {
	if len(args) == 0 {
		fmt.Fprintln(d.out, "usage: wayveil-dev install|targets|check-target|clients|set-sni|uninstall ...")
		return 2
	}
	ctx := context.Background()
	var err error
	switch args[0] {
	case "install":
		err = cmdInstall(ctx, args[1:], d)
	case "targets", "check-target", "set-sni", "uninstall":
		err = withProfile(ctx, args[0], args[1:], d)
	case "clients":
		if len(args) < 2 {
			return 2
		}
		err = withProfile(ctx, args[1], args[2:], d)
	default:
		err = errors.New("unknown command " + args[0])
	}
	if err != nil {
		fmt.Fprintln(d.out, "error:", describe(err))
		return 1
	}
	return 0
}

func describe(err error) string {
	var se *server.ScriptError
	if errors.As(err, &se) && se.Message != "" {
		return se.Code + ": " + se.Message
	}
	return err.Error()
}

func cmdInstall(ctx context.Context, args []string, d deps) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	host := fs.String("host", "", "server IP or hostname")
	port := fs.Int("port", 22, "SSH port")
	user := fs.String("user", "root", "SSH user (root or passwordless sudo)")
	sni := fs.String("sni", "", "REALITY target domain")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *host == "" || *sni == "" {
		return errors.New("-host and -sni are required")
	}
	pw, err := d.readPassword()
	if err != nil {
		return err
	}
	c, err := d.connect(ctx, sshx.Target{Host: *host, Port: *port, User: *user, Password: pw})
	pw = ""
	if err != nil {
		return err
	}
	defer c.Close()
	fmt.Fprintf(d.out, "Host key fingerprint: %s\n", c.Fingerprint())
	if !d.confirm("Is this your server's key?") {
		return errors.New("host key not confirmed")
	}
	keyPEM, line, err := sshx.GenerateKey()
	if err != nil {
		return err
	}
	// Save the profile first: the app key is authorized at the start of the
	// install, so even a failed install can be cleaned up with `uninstall`.
	if err := ownerstore.Save(ownerstore.ServerProfile{Host: *host, Port: *port, User: *user,
		HostKey: c.HostKey(), PrivateKeyPEM: keyPEM, AuthorizedLine: line}); err != nil {
		return err
	}
	start := time.Now()
	_, err = d.install(ctx, c, server.InstallOptions{Address: *host, SNI: *sni, AuthUser: *user, AuthKeyLine: line},
		func(s server.StepEvent) {
			if s.Status != server.StepStart {
				fmt.Fprintf(d.out, "  %-12s %s %s\n", s.ID, s.Status, s.Message)
			}
		})
	if errors.Is(err, server.ErrPartialInstall) {
		return fmt.Errorf("a previous install did not finish; run: wayveil-dev uninstall -host %s -port %d, then install again", *host, *port)
	}
	if err != nil {
		return fmt.Errorf("%s (clean up with: wayveil-dev uninstall -host %s -port %d)", describe(err), *host, *port)
	}
	cl, err := d.addClient(ctx, c, "Я")
	if err != nil {
		return err
	}
	info, err := d.info(ctx, c)
	if err != nil {
		return err
	}
	fmt.Fprintf(d.out, "Installed in %s. Your link:\n%s\n", time.Since(start).Round(time.Second), server.ClientLink(info, cl))
	return nil
}

// withProfile connects with the saved key and runs one management command.
func withProfile(ctx context.Context, cmd string, args []string, d deps) error {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	host := fs.String("host", "", "server")
	port := fs.Int("port", 22, "SSH port")
	domain := fs.String("domain", "", "domain")
	if err := fs.Parse(reorder(args)); err != nil {
		return err
	}
	rest := fs.Args()
	p, err := ownerstore.Load(*host, *port)
	if err != nil {
		return fmt.Errorf("no saved profile for %s:%d (run install first): %w", *host, *port, err)
	}
	c, err := d.connect(ctx, sshx.Target{Host: p.Host, Port: p.Port, User: p.User, PrivateKeyPEM: p.PrivateKeyPEM, HostKey: p.HostKey})
	if err != nil {
		return err
	}
	defer c.Close()
	out := d.out
	arg := func() (string, error) {
		if len(rest) == 0 {
			return "", errors.New(cmd + ": argument required")
		}
		return rest[0], nil
	}
	switch cmd {
	case "targets":
		h, err := server.Precheck(ctx, c)
		if err != nil {
			return err
		}
		return server.SuggestTargets(ctx, c, h, rules.DefaultTargets(), func(s server.Suggestion) {
			mark := ""
			if s.Recommended {
				mark = "  ★ recommended"
			}
			fmt.Fprintf(out, "%-8s %-28s %4d ms%s\n", s.Kind, s.Domain, s.Report.HandshakeMs, mark)
		})
	case "check-target":
		h, err := server.Precheck(ctx, c)
		if err != nil {
			return err
		}
		rep, err := server.CheckTarget(ctx, c, h.Arch, *domain)
		if err != nil {
			return err
		}
		defer server.CleanupScanner(ctx, c)
		fmt.Fprintf(out, "%s ok=%v tls13=%v http2=%v %dms problems=%v\n", rep.Domain, rep.OK, rep.TLS13, rep.HTTP2, rep.HandshakeMs, rep.Problems)
		return nil
	case "set-sni":
		if !d.confirm("All client links will change. Continue?") {
			return errors.New("cancelled")
		}
		return server.SetSNI(ctx, c, *domain)
	case "uninstall":
		if !d.confirm("Remove Wayveil from " + p.Host + "? All friends lose access.") {
			return errors.New("cancelled")
		}
		if err := server.Uninstall(ctx, c, p.AuthorizedLine, func(s server.StepEvent) {
			if s.Status != server.StepStart {
				fmt.Fprintf(out, "  %-10s %s %s\n", s.ID, s.Status, s.Message)
			}
		}); err != nil {
			return err
		}
		return ownerstore.Delete(p.Host, p.Port)
	case "list":
		list, err := server.ListClients(ctx, c)
		if err != nil {
			return err
		}
		tr, _ := server.TrafficAll(ctx, c)
		seen, _ := server.LastSeen(ctx, c)
		for _, cl := range list {
			last := "never"
			if s, ok := seen[cl.ID]; ok {
				last = s.At.Local().Format("2006-01-02 15:04") + " " + s.IP
			}
			fmt.Fprintf(out, "%-12s %-20s ↓%s ↑%s  %s\n", cl.ID, cl.Name, human(tr[cl.ID].Down), human(tr[cl.ID].Up), last)
		}
		return nil
	case "add":
		name, err := arg()
		if err != nil {
			return err
		}
		cl, err := server.AddClient(ctx, c, name)
		if err != nil {
			return err
		}
		return printLink(ctx, c, out, cl)
	case "rm":
		id, err := arg()
		if err != nil {
			return err
		}
		return server.RemoveClient(ctx, c, id)
	case "reissue":
		id, err := arg()
		if err != nil {
			return err
		}
		cl, err := server.ReissueClient(ctx, c, id)
		if err != nil {
			return err
		}
		return printLink(ctx, c, out, cl)
	case "link":
		id, err := arg()
		if err != nil {
			return err
		}
		list, err := server.ListClients(ctx, c)
		if err != nil {
			return err
		}
		for _, cl := range list {
			if cl.ID == id {
				return printLink(ctx, c, out, cl)
			}
		}
		return errors.New("no such client " + id)
	}
	return errors.New("unknown command " + cmd)
}

func printLink(ctx context.Context, r remote.Runner, out io.Writer, cl server.Client) error {
	info, err := server.Info(ctx, r)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, server.ClientLink(info, cl))
	return nil
}

// reorder moves flags before positional args so `clients add Вася -host H` works.
func reorder(args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			if !strings.Contains(args[i], "=") && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
		} else {
			pos = append(pos, args[i])
		}
	}
	return append(flags, pos...)
}

func human(n int64) string {
	const u = 1024
	if n < u {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(u), 0
	for m := n / u; m >= u; m /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}
