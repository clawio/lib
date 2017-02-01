package proxiedocwebservice

import (
	"context"
	"fmt"
	"github.com/clawio/clawiod/root"
	"github.com/go-kit/kit/log/levels"
	"github.com/patrickmn/go-cache"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

type service struct {
	logger         levels.Levels
	registryDriver root.RegistryDriver
	cache          *cache.Cache
}

func New(logger levels.Levels, registryDriver root.RegistryDriver) (root.WebService, error) {
	cache := cache.New(time.Second*10, time.Second*10)
	return &service{
		logger:         logger,
		registryDriver: registryDriver,
		cache:          cache,
	}, nil
}

func (s *service) getProxy(ctx context.Context) (*httputil.ReverseProxy, error) {
	p, ok := s.cache.Get("proxy")
	if ok {
		s.logger.Info().Log("msg", "chosen proxy from cache")
		return p.(*httputil.ReverseProxy), nil
	}

	// TODO(labkode) the logic for choosing a node is very rudimentary.
	// In the future would be nice to have at least RoundRobin.
	// Thanks that clients are registry aware we an use our own algorithms
	// based on some prometheus metrics like load.
	// TODO(labkode) add caching behaviour
	nodes, err := s.registryDriver.GetNodesForRol(ctx, "owncloud-node")
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("there are not owncloud-nodes alive")
	}
	s.logger.Info().Log("msg", "got owncloud-nodes", "numnodes", len(nodes))
	chosenNode := nodes[rand.Intn(len(nodes))]
	s.logger.Info().Log("msg", "owncloud-node chosen", "data-node-url", chosenNode.URL())
	u, err := url.Parse(chosenNode.URL())
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	s.cache.Set("proxy", proxy, cache.DefaultExpiration)
	return proxy, nil
}

func (s *service) IsProxy() bool {
	return true
}

func (s *service) Endpoints() map[string]map[string]http.HandlerFunc {
	return map[string]map[string]http.HandlerFunc{
		"/ocwebdav/status.php": {
			"GET": s.statusEndpoint(),
		},
		"/ocwebdav/ocs/v1.php/cloud/capabilities": {
			"GET": s.capabilitiesEndpoint(),
		},
		"/ocwebdav/remote.php/webdav/{path:.*}": {
			"GET":       s.getEndpoint(),
			"PUT":       s.putEndpoint(),
			"OPTIONS":   s.optionsEndpoint(),
			"LOCK":      s.lockEndpoint(),
			"UNLOCK":    s.unlockEndpoint(),
			"HEAD":      s.headEndpoint(),
			"MKCOL":     s.mkcolEndpoint(),
			"PROPPATCH": s.proppatchEndpoint(),
			"PROPFIND":  s.propfindEndpoint(),
			"DELETE":    s.deleteEndpoint(),
			"MOVE":      s.moveEndpoint(),
		},
	}
}

func (s *service) statusEndpoint() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy, err := s.getProxy(r.Context())
		if err != nil {
			s.logger.Crit().Log("error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(w, r)
		return
	}
}
func (s *service) capabilitiesEndpoint() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy, err := s.getProxy(r.Context())
		if err != nil {
			s.logger.Crit().Log("error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(w, r)
		return
	}
}
func (s *service) getEndpoint() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy, err := s.getProxy(r.Context())
		if err != nil {
			s.logger.Crit().Log("error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(w, r)
		return
	}
}

func (s *service) putEndpoint() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy, err := s.getProxy(r.Context())
		if err != nil {
			s.logger.Crit().Log("error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(w, r)
		return
	}
}

func (s *service) optionsEndpoint() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy, err := s.getProxy(r.Context())
		if err != nil {
			s.logger.Crit().Log("error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(w, r)
		return
	}
}

func (s *service) lockEndpoint() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy, err := s.getProxy(r.Context())
		if err != nil {
			s.logger.Crit().Log("error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(w, r)
		return
	}
}
func (s *service) unlockEndpoint() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy, err := s.getProxy(r.Context())
		if err != nil {
			s.logger.Crit().Log("error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(w, r)
		return
	}
}
func (s *service) headEndpoint() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy, err := s.getProxy(r.Context())
		if err != nil {
			s.logger.Crit().Log("error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(w, r)
		return
	}
}
func (s *service) mkcolEndpoint() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy, err := s.getProxy(r.Context())
		if err != nil {
			s.logger.Crit().Log("error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(w, r)
		return
	}
}
func (s *service) proppatchEndpoint() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy, err := s.getProxy(r.Context())
		if err != nil {
			s.logger.Crit().Log("error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(w, r)
		return
	}
}
func (s *service) propfindEndpoint() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy, err := s.getProxy(r.Context())
		if err != nil {
			s.logger.Crit().Log("error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(w, r)
		return
	}
}
func (s *service) deleteEndpoint() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy, err := s.getProxy(r.Context())
		if err != nil {
			s.logger.Crit().Log("error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(w, r)
		return
	}
}
func (s *service) moveEndpoint() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy, err := s.getProxy(r.Context())
		if err != nil {
			s.logger.Crit().Log("error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(w, r)
		return
	}
}
