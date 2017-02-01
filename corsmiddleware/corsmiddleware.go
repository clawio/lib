package corsmiddleware

import (
	"github.com/clawio/clawiod/root"
	"github.com/go-kit/kit/log/levels"
	"github.com/rs/cors"
	"net/http"
	"strings"
)

type middleware struct {
	logger         levels.Levels
	allowedOrigins string
	allowedMethods string
	allowedHeaders string
}

func New(logger levels.Levels, origins, methods, headers string) root.CorsMiddleware {
	logger.Info().Log("msg", "cors middleware configured", "allowed-origins", origins, "allowed-methods", methods, "allowed-headers", headers)
	return &middleware{
		logger:         logger,
		allowedOrigins: origins,
		allowedMethods: methods,
		allowedHeaders: headers,
	}
}

func (m *middleware) Handler(handler http.Handler) http.Handler {
	opts := cors.Options{}
	opts.AllowedOrigins = getTokens(m.allowedOrigins)
	opts.AllowedMethods = getTokens(m.allowedMethods)
	opts.AllowedHeaders = getTokens(m.allowedHeaders)
	return cors.New(opts).Handler(handler)
}

func getTokens(val string) []string {
	return strings.Split(val, ",")
}
