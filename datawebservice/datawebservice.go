package datawebservice

import (
	"net/http"

	"encoding/json"
	"fmt"
	"github.com/clawio/lib"
	"github.com/go-kit/kit/log/levels"
	"golang.org/x/net/context"
	"io"
	"path/filepath"
)

type service struct {
	cm                lib.ContextManager
	logger            levels.Levels
	dataDriver        lib.DataDriver
	am                lib.AuthenticationMiddleware
	wec               lib.WebErrorConverter
	uploadMaxFileSize int64
}

func New(
	cm lib.ContextManager,
	logger levels.Levels,
	dataDriver lib.DataDriver,
	am lib.AuthenticationMiddleware,
	wec lib.WebErrorConverter,
	uploadMaxFileSize int64) lib.WebService {
	return &service{
		cm:                cm,
		logger:            logger,
		dataDriver:        dataDriver,
		am:                am,
		wec:               wec,
		uploadMaxFileSize: uploadMaxFileSize,
	}
}

func (s *service) IsProxy() bool {
	return false
}

func (s *service) Endpoints() map[string]map[string]http.HandlerFunc {
	return map[string]map[string]http.HandlerFunc{
		"/data/upload": {
			"POST": s.am.HandlerFunc(s.uploadEndpoint),
		},
		"/data/download": {
			"POST": s.am.HandlerFunc(s.downloadEndpoint),
		},
	}
}

func (s *service) uploadEndpoint(w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())

	req := &pathRequest{}
	if err := json.Unmarshal([]byte(r.Header.Get("clawio-api-arg")), req); err != nil {
		logger.Error().Log("error", err)
		codeErr := badRequestError("invalid json in clawio-api-arg header")
		jsonError, err := s.wec.ErrorToJSON(codeErr)
		if err != nil {
			logger.Error().Log("error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		w.Write(jsonError)
		return
	}

	path := filepath.Clean("/" + req.Path)
	if path == "/" {
		logger.Warn().Log("msg", "can not upload to lib")
		w.WriteHeader(http.StatusForbidden)
		return
	}

	user := s.cm.MustGetUser(r.Context())

	clientChecksum := s.getClientChecksum(r)
	readCloser := http.MaxBytesReader(w, r.Body, s.uploadMaxFileSize)
	context.WithValue(r.Context(), "extra", req.Extra)
	if err := s.dataDriver.UploadFile(r.Context(), user, req.Path, readCloser, clientChecksum); err != nil {
		s.handleUploadEndpointError(err, w, r)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (s *service) handleUploadEndpointError(err error, w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	logger.Error().Log("error", err)

	if err.Error() == "http: request body too large" {
		logger.Error().Log("error", err, "msg", "request body max size exceed")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}

	if codeErr, ok := err.(lib.Error); ok {
		if codeErr.Code() == lib.CodeNotFound {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if codeErr.Code() == lib.CodeBadChecksum {
			logger.Error().Log("error", err, "msg", "file corruption on upload")
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		if codeErr.Code() == lib.CodeUploadIsPartial {
			w.WriteHeader(http.StatusPartialContent)
			return
		}
	}

	logger.Error().Log("error", err, "msg", "unexpected error uploading file")
	w.WriteHeader(http.StatusInternalServerError)
	return
}

func (s *service) downloadEndpoint(w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	user := s.cm.MustGetUser(r.Context())

	req := &pathRequest{}
	if err := json.Unmarshal([]byte(r.Header.Get("clawio-api-arg")), req); err != nil {
		logger.Error().Log("error", err)
		codeErr := badRequestError("invalid json in clawio-api-arg header")
		jsonError, err := s.wec.ErrorToJSON(codeErr)
		if err != nil {
			logger.Error().Log("error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		w.Write(jsonError)
		return
	}

	path := filepath.Clean("/" + req.Path)
	if path == "/" {
		logger.Warn().Log("msg", "can not download from lib")
		w.WriteHeader(http.StatusForbidden)
		return
	}

	readCloser, err := s.dataDriver.DownloadFile(r.Context(), user, req.Path)
	if err != nil {
		s.handleDownloadEndpointError(err, w, r)
		return
	}
	defer readCloser.Close()

	// add security headers
	w.Header().Add("X-Content-Type-Options", "nosniff")
	w.Header().Add("Content-Type", "clawio/file")
	w.Header().Add("Content-Disposition", fmt.Sprintf("attachment; filename='%s'", filepath.Base(req.Path)))
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, readCloser); err != nil {
		logger.Error().Log("error", err, "msg", "error writting response body")
		return
	}
}

func (s *service) handleDownloadEndpointError(err error, w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	if codeErr, ok := err.(lib.Error); ok {
		if codeErr.Code() == lib.CodeNotFound {
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}

		if codeErr.Code() == lib.CodeBadInputData {
			jsonErr, err := s.wec.ErrorToJSON(codeErr)
			if err != nil {
				logger.Error().Log("error", err)
				w.WriteHeader(http.StatusInternalServerError)
			}
			w.WriteHeader(http.StatusBadRequest)
			w.Write(jsonErr)
			return
		}
	}

	logger.Error().Log("error", err, "msg", "unexpected error downloading file")
	w.WriteHeader(http.StatusInternalServerError)
	return
}

func (s *service) getClientChecksum(r *http.Request) string {
	if t := r.Header.Get("checksum"); t != "" {
		return t
	}
	return r.URL.Query().Get("checksum")
}

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

type pathRequest struct {
	Path  string      `json:"path"`
	Extra interface{} `json:"extra"`
}
