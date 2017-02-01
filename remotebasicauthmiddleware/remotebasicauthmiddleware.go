package remotebasicauthmiddleware

import (
	"github.com/clawio/lib"
	"net/http"
)

type middleware struct {
	cookieName                     string
	cm                             lib.ContextManager
	authenticationWebServiceClient lib.AuthenticationWebServiceClient
	tokenDriver                    lib.TokenDriver
}

func New(cm lib.ContextManager, authenticationWebServiceClient lib.AuthenticationWebServiceClient, tokenDriver lib.TokenDriver, cookieName string) lib.BasicAuthMiddleware {
	return &middleware{cm: cm, authenticationWebServiceClient: authenticationWebServiceClient, tokenDriver: tokenDriver, cookieName: cookieName}
}

func (m *middleware) HandlerFunc(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := m.cm.MustGetLog(r.Context())

		// try to get token from cookie
		authCookie, err := r.Cookie(m.cookieName)
		if err == nil {
			user, err := m.tokenDriver.UserFromToken(authCookie.Value)
			if err == nil {
				l := logger.With("user", user.Username())
				r = r.WithContext(m.cm.SetLog(r.Context(), &l))
				r = r.WithContext(m.cm.SetUser(r.Context(), user))
				r = r.WithContext(m.cm.SetAccessToken(r.Context(), authCookie.Value))
				logger.Info().Log("user", user.Username())
				handler(w, r)
				return
			}
			logger.Warn().Log("err", "cookie token is invalid or not longer valid")

		} else {
			logger.Info().Log("msg", "cookie not set in request")
		}

		// try to get credentials using basic auth
		username, password, ok := r.BasicAuth()
		if !ok {
			logger.Info().Log("msg", "basic auth not provided")
			w.Header().Set("WWW-Authenticate", "Basic Realm='clawio credentials'")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		token, err := m.authenticationWebServiceClient.Token(r.Context(), username, password)
		if err != nil {
			logger.Error().Log("error", err)
			w.Header().Set("WWW-Authenticate", "Basic Realm='clawio credentials'")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		user, err := m.tokenDriver.UserFromToken(token)
		if err != nil {
			logger.Error().Log("error", err)
			w.Header().Set("WWW-Authenticate", "Basic Realm='clawio credentials'")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// save token into cookie for further requests
		cookie := &http.Cookie{}
		cookie.Name = m.cookieName
		cookie.Value = token
		http.SetCookie(w, cookie)

		r = r.WithContext(m.cm.SetUser(r.Context(), user))
		r = r.WithContext(m.cm.SetAccessToken(r.Context(), token))
		logger.Info().Log("user", user.Username(), "msg", "request is authenticated")
		l := logger.With("user", user.Username())
		r = r.WithContext(m.cm.SetLog(r.Context(), &l))
		handler(w, r)
		return
	}
}
