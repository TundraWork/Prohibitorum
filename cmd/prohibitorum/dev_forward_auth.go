package main

// dev_forward_auth.go — a hidden `forward-auth-whoami` subcommand used only by
// scripts/dev-forward-auth.sh. Starts a tiny HTTP server that echoes the
// Remote-* identity headers Traefik injects on an allowed forward-auth request,
// so the operator can confirm the full browser flow (app → verify → authorize →
// callback → 200 + Remote-* headers) is wired correctly.
//
// Dev-only: not compiled into production releases (no build tag — the binary is
// always present but Hidden so it does not appear in `prohibitorum --help`).

import (
	"fmt"
	"log"
	"net/http"

	"github.com/spf13/cobra"
)

var _devForwardAuthWhoamiCmd *cobra.Command

func init() {
	var addr string
	cmd := &cobra.Command{
		Use:    "forward-auth-whoami",
		Short:  "DEV: echo ForwardAuth Remote-* headers (protected app stand-in)",
		Hidden: true,
		Run: func(_ *cobra.Command, _ []string) {
			mux := http.NewServeMux()
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				fmt.Fprintf(w, "Remote-User:   %s\n", r.Header.Get("Remote-User"))
				fmt.Fprintf(w, "Remote-Name:   %s\n", r.Header.Get("Remote-Name"))
				fmt.Fprintf(w, "Remote-Email:  %s\n", r.Header.Get("Remote-Email"))
				fmt.Fprintf(w, "Remote-Groups: %s\n", r.Header.Get("Remote-Groups"))
			})
			log.Printf("forward-auth-whoami listening on %s", addr)
			if err := http.ListenAndServe(addr, mux); err != nil {
				log.Fatalf("forward-auth-whoami: %v", err)
			}
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8090", "Listen address for the whoami app.")
	_devForwardAuthWhoamiCmd = cmd
}

func addDevForwardAuthWhoamiCmd(root *cobra.Command) {
	root.AddCommand(_devForwardAuthWhoamiCmd)
}
