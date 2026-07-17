package server

import "net/http"

func (s *Server) handleVRChatProofHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	s.webUIHandler.ServeHTTP(w, r)
}
