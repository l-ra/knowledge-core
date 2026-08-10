package apihttp

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

var applicationQueryRE = regexp.MustCompile(`(?i)application\s*\(\s*code\s*:\s*"([^"]+)"`)

type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

func (s *Server) graphql(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req graphqlRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	query := strings.TrimSpace(req.Query)

	if m := applicationQueryRE.FindStringSubmatch(query); len(m) == 2 {
		data, err := s.engine.ReadLensInstance(r.Context(), "application", m[1])
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"errors": []map[string]string{{"message": err.Error()}},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{"application": data},
		})
		return
	}

	if strings.Contains(query, "domain(") {
		lensCode, key, ok := parseDomainQuery(query, req.Variables)
		if !ok {
			writeError(w, http.StatusBadRequest, "unsupported GraphQL query")
			return
		}
		data, err := s.engine.ReadLensInstance(r.Context(), lensCode, key)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"errors": []map[string]string{{"message": err.Error()}},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{"domain": data},
		})
		return
	}

	writeError(w, http.StatusBadRequest, "unsupported GraphQL query")
}

var domainQueryRE = regexp.MustCompile(`domain\s*\(\s*lens\s*:\s*"([^"]+)"\s*,\s*key\s*:\s*"([^"]+)"`)

func parseDomainQuery(query string, vars map[string]any) (lens, key string, ok bool) {
	if m := domainQueryRE.FindStringSubmatch(query); len(m) == 3 {
		return m[1], m[2], true
	}
	if vars != nil {
		l, _ := vars["lens"].(string)
		k, _ := vars["key"].(string)
		if l != "" && k != "" {
			return l, k, true
		}
	}
	return "", "", false
}
