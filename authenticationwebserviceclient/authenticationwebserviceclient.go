package authenticationwebserviceclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/clawio/clawiod/root"
	"github.com/go-kit/kit/log/levels"
	"github.com/patrickmn/go-cache"
	"io/ioutil"
	"math/rand"
	"net/http"
	"time"
)

type webServiceClient struct {
	logger         levels.Levels
	cm             root.ContextManager
	client         *http.Client
	registryDriver root.RegistryDriver
	cache          *cache.Cache
}

// New returns an implementation of DataDriver.
func New(logger levels.Levels, cm root.ContextManager, registryDriver root.RegistryDriver) root.AuthenticationWebServiceClient {
	cache := cache.New(time.Second*10, time.Second*10)
	rand.Seed(time.Now().Unix()) // initialize global pseudo-random generator
	return &webServiceClient{logger: logger, cm: cm, client: http.DefaultClient, registryDriver: registryDriver, cache: cache}
}

func (c *webServiceClient) getAuthenticationURL(ctx context.Context) (string, error) {
	var nodes []root.RegistryNode
	v, ok := c.cache.Get("nodes")
	if ok {
		c.logger.Info().Log("msg", "nodes obtained from cache")
		nodes = v.([]root.RegistryNode)
	} else {
		ns, err := c.registryDriver.GetNodesForRol(ctx, "authentication-node")
		if err != nil {
			return "", err
		}
		if len(ns) == 0 {
			return "", fmt.Errorf("there are not authentication-nodes alive")
		}
		c.logger.Info().Log("msg", "nodes obtained from registry")
		nodes = ns
	}
	c.cache.Set("nodes", nodes, cache.DefaultExpiration)

	c.logger.Info().Log("msg", "got authentication-nodes", "numnodes", len(nodes))
	chosenNode := nodes[rand.Intn(len(nodes))]
	c.logger.Info().Log("msg", "authentication-node chosen", "authentication-node-url", chosenNode.URL())
	chosenURL := chosenNode.URL() + "/auth"
	return chosenURL, nil
}

func (c *webServiceClient) Token(ctx context.Context, username, password string) (string, error) {
	traceID := c.cm.MustGetTraceID(ctx)

	tokenReq := &tokenReq{Username: username, Password: password}
	jsonBody, err := json.Marshal(tokenReq)
	if err != nil {
		c.logger.Error().Log("error", err, "msg", "error encoding token request")
		return "", err
	}

	url, err := c.getAuthenticationURL(ctx)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", url+"/token", bytes.NewReader(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Add("x-clawio-tid", traceID)
	res, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	jsonRes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return "", err
	}

	if res.StatusCode == http.StatusCreated {
		tokenRes := &tokenRes{}
		err := json.Unmarshal(jsonRes, tokenRes)
		if err != nil {
			return "", err
		}
		return tokenRes.AccessToken, nil
	}

	if res.StatusCode == http.StatusUnauthorized {
		return "", unauthorizedError("")
	}
	if res.StatusCode == http.StatusBadRequest {
		return "", badInputDataError("")
	}

	return "", internalError(fmt.Sprintf("http status code: %d", res.StatusCode))
}

func (c *webServiceClient) Ping(ctx context.Context, token string) error {
	traceID := c.cm.MustGetTraceID(ctx)

	url, err := c.getAuthenticationURL(ctx)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("GET", url+"/ping", nil)
	if err != nil {
		return err
	}

	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("x-clawio-tid", traceID)

	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	ioutil.ReadAll(res.Body)

	if res.StatusCode == http.StatusOK {
		return nil
	}
	if res.StatusCode == http.StatusUnauthorized {
		return unauthorizedError("")
	}
	return internalError(fmt.Sprintf("http status code: %d", res.StatusCode))
}

type tokenReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type tokenRes struct {
	AccessToken string `json:"access_token"`
}

type internalError string

func (e internalError) Error() string {
	return string(e)
}
func (e internalError) Code() root.Code {
	return root.Code(root.CodeInternal)
}
func (e internalError) Message() string {
	return string(e)
}

type badInputDataError string

func (e badInputDataError) Error() string {
	return string(e)
}
func (e badInputDataError) Code() root.Code {
	return root.Code(root.CodeBadInputData)
}
func (e badInputDataError) Message() string {
	return string(e)
}

type unauthorizedError string

func (e unauthorizedError) Error() string {
	return string(e)
}
func (e unauthorizedError) Code() root.Code {
	return root.Code(root.CodeUnauthorized)
}
func (e unauthorizedError) Message() string {
	return string(e)
}
