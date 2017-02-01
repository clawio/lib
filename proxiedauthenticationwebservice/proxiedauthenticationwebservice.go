package proxiedauthenticationwebservice

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
	nodes, err := s.registryDriver.GetNodesForRol(ctx, "authentication-node")
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("there are not authentication-nodes alive")
	}
	s.logger.Info().Log("msg", "got authentication-nodes", "numnodes", len(nodes))
	chosenNode := nodes[rand.Intn(len(nodes))]
	s.logger.Info().Log("msg", "authentication-node chosen", "data-node-url", chosenNode.URL())
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
		"/auth/token": {
			"POST": s.tokenEndpoint(),
		},
		"/auth/ping": {
			"GET": s.pingEndpoint(),
		},
	}
}

func (s *service) tokenEndpoint() http.HandlerFunc {
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

func (s *service) pingEndpoint() http.HandlerFunc {
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
