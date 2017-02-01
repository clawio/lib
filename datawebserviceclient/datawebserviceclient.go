package datawebserviceclient

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/clawio/clawiod/root"
	"github.com/go-kit/kit/log/levels"
	"github.com/patrickmn/go-cache"
	"io"
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
func New(logger levels.Levels, cm root.ContextManager, registryDriver root.RegistryDriver) root.DataWebServiceClient {
	cache := cache.New(time.Second*10, time.Second*10)
	rand.Seed(time.Now().Unix()) // initialize global pseudorandom generator
	return &webServiceClient{logger: logger, cm: cm, client: http.DefaultClient, registryDriver: registryDriver, cache: cache}
}

func (c *webServiceClient) getDataURL(ctx context.Context) (string, error) {
	var nodes []root.RegistryNode

	v, ok := c.cache.Get("nodes")
	if ok {
		c.logger.Info().Log("msg", "nodes obtained from cache")
		nodes = v.([]root.RegistryNode)
	} else {
		ns, err := c.registryDriver.GetNodesForRol(ctx, "data-node")
		if err != nil {
			return "", err
		}
		if len(ns) == 0 {
			return "", fmt.Errorf("there are not data-nodes alive")
		}
		c.logger.Info().Log("msg", "nodes obtained from registry")
		nodes = ns
	}
	c.cache.Set("nodes", nodes, cache.DefaultExpiration)

	c.logger.Info().Log("msg", "got data-nodes", "numnodes", len(nodes))
	chosenNode := nodes[rand.Intn(len(nodes))]
	c.logger.Info().Log("msg", "data-node chosen", "data-node-url", chosenNode.URL())
	chosenURL := chosenNode.URL() + "/data"
	return chosenURL, nil
}
func (c *webServiceClient) UploadFile(ctx context.Context, user root.User, path string, r io.ReadCloser, clientChecksum string) error {
	traceID := c.cm.MustGetTraceID(ctx)
	token := c.cm.MustGetAccessToken(ctx)

	pathReq := &pathReq{Path: path}
	jsonHeader, err := json.Marshal(pathReq)
	if err != nil {
		c.logger.Error().Log("error", err, "msg", "error encoding path request")
		return err
	}

	url, err := c.getDataURL(ctx)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url+"/upload", r)
	if err != nil {
		return err
	}

	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("x-clawio-tid", traceID)
	req.Header.Add("clawio-api-arg", string(jsonHeader))
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	ioutil.ReadAll(res.Body)

	if res.StatusCode == http.StatusCreated {
		return nil
	}
	if res.StatusCode == http.StatusNotFound {
		return notFoundError("")
	}
	if res.StatusCode == http.StatusPartialContent {
		return partialUploadError("")
	}
	if res.StatusCode == http.StatusPreconditionFailed {
		return checksumError("checksum mismatch")
	}
	if res.StatusCode == http.StatusRequestEntityTooLarge {
		return tooBigError("maximun file size exceeded")
	}
	if res.StatusCode == http.StatusForbidden {
		return forbiddenError("")
	}
	if res.StatusCode == http.StatusBadRequest {
		return badInputDataError("")
	}

	return internalError(fmt.Sprintf("http status code: %d", res.StatusCode))
}

func (c *webServiceClient) DownloadFile(ctx context.Context, user root.User, path string) (io.ReadCloser, error) {
	traceID := c.cm.MustGetTraceID(ctx)
	token := c.cm.MustGetAccessToken(ctx)

	pathReq := &pathReq{Path: path}
	jsonHeader, err := json.Marshal(pathReq)
	if err != nil {
		c.logger.Error().Log("error", err, "msg", "error encoding path request")
		return nil, err
	}

	url, err := c.getDataURL(ctx)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url+"/download", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("x-clawio-tid", traceID)
	req.Header.Add("clawio-api-arg", string(jsonHeader))

	res, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	// it is the responsability of the caller to close the ReadCloser
	// so we don't close the the body here.

	if res.StatusCode != http.StatusOK {
		return nil, internalError(fmt.Sprintf("http status code: %d", res.StatusCode))
	}
	return res.Body, nil
}

type pathReq struct {
	Path string `json:"path"`
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

type checksumError string

func (e checksumError) Error() string {
	return string(e)
}
func (e checksumError) Code() root.Code {
	return root.Code(root.CodeBadChecksum)
}
func (e checksumError) Message() string {
	return string(e)
}

type notFoundError string

func (e notFoundError) Error() string {
	return string(e)
}
func (e notFoundError) Code() root.Code {
	return root.Code(root.CodeNotFound)
}
func (e notFoundError) Message() string {
	return string(e)
}

type isFolderError string

func (e isFolderError) Error() string {
	return string(e)
}
func (e isFolderError) Code() root.Code {
	return root.Code(root.CodeBadInputData)
}
func (e isFolderError) Message() string {
	return string(e)
}

type tooBigError string

func (e tooBigError) Error() string {
	return string(e)
}
func (e tooBigError) Code() root.Code {
	return root.Code(root.CodeTooBig)
}
func (e tooBigError) Message() string {
	return string(e)
}

type forbiddenError string

func (e forbiddenError) Error() string {
	return string(e)
}
func (e forbiddenError) Code() root.Code {
	return root.Code(root.CodeForbidden)
}
func (e forbiddenError) Message() string {
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

type partialUploadError string

func (e partialUploadError) Error() string {
	return string(e)
}
func (e partialUploadError) Code() root.Code {
	return root.Code(root.CodeUploadIsPartial)
}
func (e partialUploadError) Message() string {
	return string(e)
}
