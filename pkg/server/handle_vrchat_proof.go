package server

import "net/http"

func (s *Server) handleVRChatProofHTTP(w http.ResponseWriter, r *http.Request) {
	// The shell sets no-store for page addresses, but keep the proof page
	// uncachable even if that ever changes; Referrer-Policy comes with the
	// shell's security headers on every response.
	w.Header().Set("Cache-Control", "no-store")
	s.webUIHandler.ServeHTTP(w, r)
}
