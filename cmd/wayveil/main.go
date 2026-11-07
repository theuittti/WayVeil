// Command wayveil is the Linux client: import a vless:// link from a friend
// and connect with system-wide split tunnelling.
//
//	wayveil import <vless-link | qr-image.png>
//	wayveil connect | disconnect | status
//	wayveil mode smart|all
//	wayveil rules list | proxy <domain> | direct <domain> | rm <domain>
//	wayveil lists update
//	wayveil setup            (one-time permission for the TUN device)
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/theuittti/WayVeil/internal/clientstore"
	"github.com/theuittti/WayVeil/internal/engine"
	"github.com/theuittti/WayVeil/internal/lists"
	"github.com/theuittti/WayVeil/internal/mihomo"
	"github.com/theuittti/WayVeil/internal/profile"
)

type engineAPI interface {
	Connect(context.Context, clientstore.Settings) error
	Disconnect(context.Context) error
	Status(context.Context) engine.Status
}

type deps struct {
	out    io.Writer
	engine engineAPI
	grant  func(ctx context.Context, bin string) error
	update func(context.Context, lists.Options) (lists.UpdateResult, error)
	mihomo func() (string, error)
}

const listsMaxAge = 24 * time.Hour

func main() {
	eng, err := engine.New(engine.Options{Inbound: mihomo.TUN})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	os.Exit(run(os.Args[1:], deps{out: os.Stdout, engine: eng, grant: engine.GrantCaps, update: lists.Update, mihomo: mihomo.Find}))
}

func run(args []string, d deps) int {
	if len(args) == 0 {
		fmt.Fprintln(d.out, "usage: wayveil import|connect|disconnect|status|mode|rules|lists|setup")
		return 2
	}
	ctx := context.Background()
	if err := dispatch(ctx, args, d); err != nil {
		fmt.Fprintln(d.out, "error:", err)
		return 1
	}
	return 0
}

func dispatch(ctx context.Context, args []string, d deps) error {
	s, err := clientstore.Load()
	if err != nil {
		return err
	}
	arg := func(i int) (string, error) {
		if len(args) <= i {
			return "", fmt.Errorf("%s: missing argument", strings.Join(args, " "))
		}
		return args[i], nil
	}
	switch args[0] {
	case "import":
		src, err := arg(1)
		if err != nil {
			return err
		}
		var p profile.Profile
		if strings.HasPrefix(strings.TrimSpace(src), "vless://") {
			p, err = profile.FromLink(src)
		} else {
			p, err = profile.FromQRImage(src)
		}
		if err != nil {
			return err
		}
		s.Profile = &p
		if err := clientstore.Save(s); err != nil {
			return err
		}
		fmt.Fprintf(d.out, "Imported %q (%s). Now run: wayveil connect\n", p.Name, p.Params.Host)
		return nil
	case "connect":
		return connect(ctx, &s, d)
	case "disconnect":
		return d.engine.Disconnect(ctx)
	case "status":
		st := d.engine.Status(ctx)
		fmt.Fprintf(d.out, "%s", st.State)
		if st.State != engine.Disconnected {
			fmt.Fprintf(d.out, " · %s · mode %s · since %s · ↓%s ↑%s", st.Server, st.Mode, st.Since.Format("15:04"), human(st.Down), human(st.Up))
		}
		fmt.Fprintln(d.out)
		if st.Error != "" {
			fmt.Fprintln(d.out, st.Error)
		}
		return nil
	case "mode":
		m, err := arg(1)
		if err != nil {
			return err
		}
		if m != string(clientstore.ModeSmart) && m != string(clientstore.ModeAll) {
			return errors.New("mode must be smart or all")
		}
		s.Mode = clientstore.Mode(m)
		return saveAndApply(ctx, &s, d)
	case "rules":
		sub, err := arg(1)
		if err != nil {
			return err
		}
		if sub == "list" {
			fmt.Fprintln(d.out, "always via VPN: ", strings.Join(s.AlwaysProxy, ", "))
			fmt.Fprintln(d.out, "always direct:  ", strings.Join(s.AlwaysDirect, ", "))
			return nil
		}
		dom, err := arg(2)
		if err != nil {
			return err
		}
		switch sub {
		case "proxy":
			err = s.AddRule(&s.AlwaysProxy, dom)
		case "direct":
			err = s.AddRule(&s.AlwaysDirect, dom)
		case "rm":
			err = s.RemoveRule(dom)
		default:
			return errors.New("rules: list | proxy <domain> | direct <domain> | rm <domain>")
		}
		if err != nil {
			return fmt.Errorf("%q: %w", dom, err)
		}
		return saveAndApply(ctx, &s, d)
	case "lists":
		if sub, _ := arg(1); sub != "update" {
			return errors.New("usage: wayveil lists update")
		}
		return updateLists(ctx, &s, d, true)
	case "setup":
		bin, err := d.mihomo()
		if err != nil {
			return err
		}
		return d.grant(ctx, bin)
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func connect(ctx context.Context, s *clientstore.Settings, d deps) error {
	if s.Profile == nil {
		return errors.New("no connection yet: run wayveil import <link from your friend>")
	}
	if time.Since(s.ListsUpdated) > listsMaxAge {
		if err := updateLists(ctx, s, d, false); err != nil {
			fmt.Fprintln(d.out, "warning: could not refresh block lists, using saved ones:", err)
		}
	}
	err := d.engine.Connect(ctx, *s)
	if errors.Is(err, engine.ErrNeedsElevation) {
		fmt.Fprintln(d.out, "Wayveil needs a one-time permission to create its network interface (TUN).")
		bin, ferr := d.mihomo()
		if ferr != nil {
			return ferr
		}
		if gerr := d.grant(ctx, bin); gerr != nil {
			return fmt.Errorf("permission for the TUN device was not granted: %w", gerr)
		}
		err = d.engine.Connect(ctx, *s)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(d.out, "Connected to %s (%s mode)\n", s.Profile.Name, s.Mode)
	return nil
}

// saveAndApply saves settings and reconnects if a connection is running.
func saveAndApply(ctx context.Context, s *clientstore.Settings, d deps) error {
	if err := clientstore.Save(*s); err != nil {
		return err
	}
	if st := d.engine.Status(ctx).State; st == engine.Connected || st == engine.Connecting {
		if err := d.engine.Disconnect(ctx); err != nil {
			return err
		}
		return connect(ctx, s, d)
	}
	fmt.Fprintln(d.out, "Saved.")
	return nil
}

func updateLists(ctx context.Context, s *clientstore.Settings, d deps, verbose bool) error {
	dir, err := lists.DefaultDir()
	if err != nil {
		return err
	}
	res, err := d.update(ctx, lists.Options{Dir: dir})
	if err != nil {
		return err
	}
	if verbose {
		fmt.Fprintf(d.out, "updated: %s\n", strings.Join(res.Updated, ", "))
		for tag, e := range res.Errors {
			fmt.Fprintf(d.out, "kept %s: %s\n", tag, e)
		}
	}
	if len(res.Updated) == 0 {
		return errors.New("no list could be downloaded")
	}
	s.ListsUpdated = time.Now()
	return clientstore.Save(*s)
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
