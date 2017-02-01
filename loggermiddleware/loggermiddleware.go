package loggermiddleware

import (
	"errors"
	"fmt"
	"github.com/clawio/lib"
	"github.com/go-kit/kit/log/levels"
	"github.com/satori/go.uuid"
	"net/http"
	"runtime"
	"time"
)

type middleware struct {
	cm     lib.ContextManager
	logger levels.Levels
}

func New(cm lib.ContextManager, logger levels.Levels) lib.AuthenticationMiddleware {
	return &middleware{cm: cm, logger: logger}
}

func (m *middleware) HandlerFunc(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// if tid is sent in request re-use it, what about non trusted servers sending
		// their tid? Solution: a node must trust a cluster to trust re-use tid...
		tid := r.Header.Get("clawio-tid")
		if tid == "" {
			tid = uuid.NewV4().String()
		}
		r = r.WithContext(m.cm.SetTraceID(r.Context(), tid))

		l := m.logger.With("traceid", tid, "url", r.URL.Path)
		r = r.WithContext(m.cm.SetLog(r.Context(), &l))
		start := time.Now()
		l.Info().Log("msg", "request started")
		defer func() {
			elapsed := time.Since(start).Seconds()
			l.Info().Log("msg", "req ended", "reqtime", elapsed)
			// Catch panic and return 500 with corresponding tid for debugging
			var err error
			r := recover()
			if r != nil {
				switch t := r.(type) {
				case string:
					err = errors.New(t)
				case error:
					err = t
				default:
					err = errors.New(fmt.Sprintln(r))
				}
				trace := make([]byte, 2048)
				count := runtime.Stack(trace, true)
				l.Crit().Log("error", fmt.Sprintf("recover from panic: %s\nstack of %d bytes: %s\n", err.Error(), count, trace))
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(tid))
				return
			}
		}()
		handler(w, r)
	}
}
