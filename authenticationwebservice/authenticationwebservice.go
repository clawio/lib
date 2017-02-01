package authenticationwebservice

import (
	"encoding/json"
	"net/http"

	"github.com/clawio/lib"
	"github.com/go-kit/kit/log/levels"
)

type service struct {
	cm             lib.ContextManager
	logger         levels.Levels
	userDriver     lib.UserDriver
	tokenDriver    lib.TokenDriver
	am             lib.AuthenticationMiddleware
	wec            lib.WebErrorConverter
	methodAgnostic bool
}

func New(
	cm lib.ContextManager,
	logger levels.Levels,
	userDriver lib.UserDriver,
	tokenDriver lib.TokenDriver,
	am lib.AuthenticationMiddleware,
	wec lib.WebErrorConverter,
	methodAgnostic bool) lib.WebService {
	return &service{
		cm:             cm,
		logger:         logger,
		userDriver:     userDriver,
		tokenDriver:    tokenDriver,
		am:             am,
		wec:            wec,
		methodAgnostic: methodAgnostic,
	}
}

func (s *service) IsProxy() bool {
	return false
}

func (s *service) Endpoints() map[string]map[string]http.HandlerFunc {
	if s.methodAgnostic {
		return map[string]map[string]http.HandlerFunc{
			"/auth/token": {
				"*": s.tokenEndpoint,
			},
			"/auth/ping": {
				"GET": s.am.HandlerFunc(s.pingEndpoint),
			},
		}
	}
	return map[string]map[string]http.HandlerFunc{
		"/auth/token": {
			"POST": s.tokenEndpoint,
		},
		"/auth/ping": {
			"GET": s.am.HandlerFunc(s.pingEndpoint),
		},
	}
}

func (s *service) tokenEndpoint(w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())

	var username, password string

	username, password, ok := r.BasicAuth()
	if ok {
		s.logger.Info().Log("msg", "credentials source is basic auth")
	} else {
		s.logger.Info().Log("msg", "credentials source is request body")
		req := &tokenRequest{}
		if err := json.NewDecoder(r.Body).Decode(req); err != nil {
			logger.Error().Log("error", err)
			err = badRequestError("invalid json")
			jsonError, err := s.wec.ErrorToJSON(err)
			if err != nil {
				logger.Error().Log("error", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			w.Write(jsonError)
			return
		}
		username = req.Username
		password = req.Password
	}

	user, err := s.userDriver.GetByCredentials(username, password)
	if err != nil {
		s.handleTokenEndpointError(err, w, r)
		return
	}

	token, err := s.tokenDriver.CreateToken(user)
	if err != nil {
		s.handleTokenEndpointError(err, w, r)
		return
	}

	logger.Info().Log("msg", "token generated for user", "user", user.Username())
	res := &tokenResponse{AccessToken: token}

	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(res); err != nil {
		logger.Error().Log("error", err)
	}

}

func (s *service) handleTokenEndpointError(err error, w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	logger.Error().Log("error", err)
	err = badRequestError("invalid authentication credentials")
	logger.Error().Log("error", err.Error())
	jsonErr, err := s.wec.ErrorToJSON(err)
	if err != nil {
		logger.Error().Log("error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusUnauthorized)
	w.Write(jsonErr)
	return
}

func (s *service) pingEndpoint(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("pong"))
}

type (
	tokenRequest struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	tokenResponse struct {
		AccessToken string `json:"access_token"`
	}
)

type badRequestError string

func (e badRequestError) Error() string {
	return string(e)
}
func (e badRequestError) Code() lib.Code {
	return lib.Code(lib.CodeBadInputData)
}
func (e badRequestError) Message() string {
	return string(e)
}
