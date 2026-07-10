package api

import "net/http"

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if !s.decodeJSON(w, r, &body) {
		return
	}
	u, err := s.identities.CreateUser(r.Context(), body.Email, body.Password, body.Role)
	if err != nil {
		s.writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	s.writeJSON(w, http.StatusCreated, u)
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.identities.ListUsers(r.Context())
	if err != nil {
		s.log.Error("list users", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"users": users})
}
