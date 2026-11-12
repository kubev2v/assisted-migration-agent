package server

import "net/http"

func HandlerForTest(s *Server) http.Handler { return s.srv.Handler }
