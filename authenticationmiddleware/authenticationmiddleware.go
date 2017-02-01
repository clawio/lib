package authenticationmiddleware

import (
	"github.com/clawio/clawiod/root"
	"net/http"
	"strings"
)

type middleware struct {
	cm          root.ContextManager
	tokenDriver root.TokenDriver
}

func New(cm root.ContextManager, tokenDriver root.TokenDriver) root.AuthenticationMiddleware {
	return &middleware{cm: cm, tokenDriver: tokenDriver}
}

func (m *middleware) HandlerFunc(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := m.cm.MustGetLog(r.Context())
		token := m.getTokenFromRequest(r)
		user, err := m.tokenDriver.UserFromToken(token)
		if err != nil {
			logger.Warn().Log("err", "token is invalid or not longer valid")
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		r = r.WithContext(m.cm.SetUser(r.Context(), user))
		r = r.WithContext(m.cm.SetAccessToken(r.Context(), token))
		l := logger.With("user", user.Username())
		r = r.WithContext(m.cm.SetLog(r.Context(), &l))
		logger.Info().Log("user", user.Username(), "msg", "request is authenticated")
		handler(w, r)
	}
}

func (m *middleware) getTokenFromRequest(r *http.Request) string {
	if t := m.getTokenFromHeader(r); t != "" {
		return t
	}
	return m.getTokenFromQuery(r)
}
func (a *middleware) getTokenFromQuery(r *http.Request) string {
	return r.URL.Query().Get("access_token")
}
func (a *middleware) getTokenFromHeader(r *http.Request) string {
	header := r.Header.Get("Authorization")
	parts := strings.Split(header, " ")
	if len(parts) < 2 {
		return ""
	}
	if strings.ToLower(parts[0]) != "bearer" {
		return ""
	}
	return parts[1]
}
