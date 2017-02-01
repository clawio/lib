package metadatawebserviceclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/clawio/lib"
	"github.com/go-kit/kit/log/levels"
	"github.com/patrickmn/go-cache"
	"io/ioutil"
	"math/rand"
	"net/http"
	"time"
)

type webServiceClient struct {
	logger         levels.Levels
	cm             lib.ContextManager
	client         *http.Client
	registryDriver lib.RegistryDriver
	cache          *cache.Cache
}

func New(logger levels.Levels, cm lib.ContextManager, registryDriver lib.RegistryDriver) lib.MetaDataWebServiceClient {
	cache := cache.New(time.Second*10, time.Second*10)
	return &webServiceClient{logger: logger, cm: cm, registryDriver: registryDriver, client: http.DefaultClient, cache: cache}
}

func (c *webServiceClient) getMetaDataURL(ctx context.Context) (string, error) {
	var nodes []lib.RegistryNode

	v, ok := c.cache.Get("nodes")
	if ok {
		nodes = v.([]lib.RegistryNode)
		c.logger.Info().Log("msg", "nodes obtained from cache")
	} else {
		ns, err := c.registryDriver.GetNodesForRol(ctx, "metadata-node")
		if err != nil {
			return "", err
		}
		if len(ns) == 0 {
			return "", fmt.Errorf("there are not metadata-nodes alive")
		}
		c.logger.Info().Log("msg", "nodes obtained from registry")
		nodes = ns
	}
	c.cache.Set("nodes", nodes, cache.DefaultExpiration)

	c.logger.Info().Log("msg", "got metadata-nodes", "numnodes", len(nodes))
	chosenNode := nodes[rand.Intn(len(nodes))]
	c.logger.Info().Log("msg", "metadata-node chosen", "metadata-node-url", chosenNode.URL())
	chosenURL := chosenNode.URL() + "/meta"
	return chosenURL, nil
}

func (c *webServiceClient) Examine(ctx context.Context, user lib.User, path string) (lib.FileInfo, error) {
	traceID := c.cm.MustGetTraceID(ctx)
	token := c.cm.MustGetAccessToken(ctx)

	pathReq := &pathReq{Path: path}
	jsonBody, err := json.Marshal(pathReq)
	if err != nil {
		c.logger.Error().Log("error", err, "msg", "error encoding path request")
		return nil, err
	}

	url, err := c.getMetaDataURL(ctx)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url+"/examine", bytes.NewReader(jsonBody))
	if err != nil {
		c.logger.Error().Log("error", err)
		return nil, err
	}

	req.Header.Add("authorization", "Bearer "+token)
	req.Header.Add("x-clawio-tid", traceID)
	res, err := c.client.Do(req)
	if err != nil {
		c.logger.Error().Log("error", err)
		return nil, err
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		c.logger.Error().Log("error", err)
		return nil, err
	}

	if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusCreated {
		fi := &fileInfo{}
		err = json.Unmarshal(body, fi)
		return fi, err
	}

	if res.StatusCode == http.StatusNotFound {
		return nil, notFoundError("")
	}

	c.logger.Error().Log("error", "error examining on remote", "httpstatuscode", res.StatusCode)
	return nil, internalError(fmt.Sprintf("error examining on remote"))

}

func (c *webServiceClient) ListFolder(ctx context.Context, user lib.User, path string) ([]lib.FileInfo, error) {
	traceID := c.cm.MustGetTraceID(ctx)
	token := c.cm.MustGetAccessToken(ctx)

	pathReq := &pathReq{Path: path}
	jsonBody, err := json.Marshal(pathReq)
	if err != nil {
		c.logger.Error().Log("error", err, "msg", "error encoding path request")
		return nil, err
	}

	url, err := c.getMetaDataURL(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", url+"/list", bytes.NewReader(jsonBody))
	if err != nil {
		c.logger.Error().Log("error", err)
		return nil, err
	}
	req.Header.Add("authorization", "Bearer "+token)
	req.Header.Add("x-clawio-tid", traceID)

	res, err := c.client.Do(req)
	if err != nil {
		c.logger.Error().Log("error", err)
		return nil, err
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		c.logger.Error().Log("error", err)
		return nil, err
	}

	if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusCreated {
		finfos := []*fileInfo{}
		err = json.Unmarshal(body, &finfos)
		if err != nil {
			c.logger.Error().Log("error", err)
			return nil, err
		}
		fileInfos := []lib.FileInfo{}
		for _, fi := range finfos {
			fileInfos = append(fileInfos, fi)
		}
		return fileInfos, nil
	}

	if res.StatusCode == http.StatusNotFound {
		return nil, notFoundError("")
	}

	c.logger.Error().Log("error", "error listing on remote", "httpstatuscode", res.StatusCode)
	return nil, internalError(fmt.Sprintf("error listing on remote"))
}

func (c *webServiceClient) Delete(ctx context.Context, user lib.User, path string) error {
	traceID := c.cm.MustGetTraceID(ctx)
	token := c.cm.MustGetAccessToken(ctx)

	pathReq := &pathReq{Path: path}
	jsonBody, err := json.Marshal(pathReq)
	if err != nil {
		c.logger.Error().Log("error", err, "msg", "error encoding path request")
		return err
	}

	url, err := c.getMetaDataURL(ctx)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url+"/delete", bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Add("authorization", "Bearer "+token)
	req.Header.Add("x-clawio-tid", traceID)

	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	ioutil.ReadAll(res.Body)

	if res.StatusCode == http.StatusNoContent {
		return nil
	}

	if res.StatusCode == http.StatusNotFound {
		return notFoundError("")
	}

	c.logger.Error().Log("error", "error deleting on remote", "httpstatuscode", res.StatusCode)
	return internalError(fmt.Sprintf("error deleting on remote"))
}

func (c *webServiceClient) Move(ctx context.Context, user lib.User, sourcePath, targetPath string) error {
	traceID := c.cm.MustGetTraceID(ctx)
	token := c.cm.MustGetAccessToken(ctx)

	moveReq := &moveRequest{Source: sourcePath, Target: targetPath}
	jsonBody, err := json.Marshal(moveReq)
	if err != nil {
		c.logger.Error().Log("error", err, "msg", "error encoding path request")
		return err
	}

	url, err := c.getMetaDataURL(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", url+"/move", bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Add("authorization", "Bearer "+token)
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

	if res.StatusCode == http.StatusNotFound {
		return notFoundError("")
	}

	c.logger.Error().Log("error", "error moving on remote", "httpstatuscode", res.StatusCode)
	return internalError(fmt.Sprintf("error moving on remote"))
}

func (c *webServiceClient) CreateFolder(ctx context.Context, user lib.User, path string) error {
	traceID := c.cm.MustGetTraceID(ctx)
	token := c.cm.MustGetAccessToken(ctx)

	pathReq := &pathReq{Path: path}
	jsonBody, err := json.Marshal(pathReq)
	if err != nil {
		c.logger.Error().Log("error", err, "msg", "error encoding path request")
		return err
	}

	url, err := c.getMetaDataURL(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", url+"/makefolder", bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Add("authorization", "Bearer "+token)
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

	c.logger.Error().Log("error", "error creating folder on remote", "httpstatuscode", res.StatusCode)
	return internalError("error creating folder on remote")
}

type pathReq struct {
	Path string `json:"path"`
}

type fileInfo struct {
	XPath            string                 `json:"path"`
	XFolder          bool                   `json:"folder"`
	XSize            int64                  `json:"size"`
	XModified        int64                  `json:"modified"`
	XChecksum        string                 `json:"checksum"`
	XExtraAttributes map[string]interface{} `json:"extra_attributes"`
}

func (f *fileInfo) Path() string {
	return f.XPath
}

func (f *fileInfo) Folder() bool {
	return f.XFolder
}

func (f *fileInfo) Size() int64 {
	return f.XSize
}

func (f *fileInfo) Modified() int64 {
	return f.XModified
}

func (f *fileInfo) Checksum() string {
	return f.XChecksum
}

func (f *fileInfo) ExtraAttributes() map[string]interface{} {
	return f.XExtraAttributes
}

type internalError string

func (e internalError) Error() string {
	return string(e)
}
func (e internalError) Code() lib.Code {
	return lib.Code(lib.CodeNotFound)
}
func (e internalError) Message() string {
	return string(e)
}

type notFoundError string

func (e notFoundError) Error() string {
	return string(e)
}
func (e notFoundError) Code() lib.Code {
	return lib.Code(lib.CodeNotFound)
}
func (e notFoundError) Message() string {
	return string(e)
}

type moveRequest struct {
	Source string `json:"source"`
	Target string `json:"target"`
}
