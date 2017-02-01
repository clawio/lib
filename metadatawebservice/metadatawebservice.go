package metadatawebservice

import (
	"net/http"

	"encoding/json"
	"github.com/clawio/lib"
	"github.com/go-kit/kit/log/levels"
	"path/filepath"
)

type service struct {
	cm             lib.ContextManager
	logger         levels.Levels
	metaDataDriver lib.MetaDataDriver
	am             lib.AuthenticationMiddleware
	wec            lib.WebErrorConverter
}

func New(
	cm lib.ContextManager,
	logger levels.Levels,
	metaDataDriver lib.MetaDataDriver,
	am lib.AuthenticationMiddleware,
	wec lib.WebErrorConverter) lib.WebService {
	return &service{
		cm:             cm,
		logger:         logger,
		metaDataDriver: metaDataDriver,
		am:             am,
		wec:            wec,
	}
}

func (s *service) IsProxy() bool {
	return false
}

func (s *service) Endpoints() map[string]map[string]http.HandlerFunc {
	return map[string]map[string]http.HandlerFunc{
		"/meta/examine": {
			"POST": s.am.HandlerFunc(s.examineEndpoint),
		},
		"/meta/list": {
			"POST": s.am.HandlerFunc(s.listFolderEndpoint),
		},
		"/meta/move": {
			"POST": s.am.HandlerFunc(s.moveEndpoint),
		},
		"/meta/delete": {
			"POST": s.am.HandlerFunc(s.deleteEndpoint),
		},
		"/meta/makefolder": {
			"POST": s.am.HandlerFunc(s.makeFolderEndpoint),
		},
	}
}

func (s *service) examineEndpoint(w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	user := s.cm.MustGetUser(r.Context())

	req := &pathRequest{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		logger.Error().Log("error", err)
		codeErr := badRequestError("invalid json")
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

	fileInfo, err := s.metaDataDriver.Examine(r.Context(), user, req.Path)
	if err != nil {
		s.handleExamineEndpointError(err, w, r)
		return
	}
	fileInfoResponse := fileInfoToFileInfoResponse(fileInfo)
	fileInfoJSON, err := json.Marshal(fileInfoResponse)
	if err != nil {
		logger.Error().Log("error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(fileInfoJSON)
}

func (s *service) handleExamineEndpointError(err error, w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	if codeErr, ok := err.(lib.Error); ok {
		if codeErr.Code() == lib.CodeNotFound {
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}

	logger.Error().Log("error", err, "msg", "unexpected error examining file")
	w.WriteHeader(http.StatusInternalServerError)
	return
}

func (s *service) listFolderEndpoint(w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	user := s.cm.MustGetUser(r.Context())

	req := &pathRequest{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		logger.Error().Log("error", err)
		codeErr := badRequestError("invalid json")
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

	fileInfos, err := s.metaDataDriver.ListFolder(r.Context(), user, req.Path)
	if err != nil {
		s.handleListFolderEndpointError(err, w, r)
		return
	}
	fileInfoResponses := []*fileInfoResponse{}
	for _, fi := range fileInfos {
		fileInfoResponses = append(fileInfoResponses, fileInfoToFileInfoResponse(fi))
	}
	fileInfosJSON, err := json.Marshal(fileInfoResponses)
	if err != nil {
		logger.Error().Log("error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(fileInfosJSON)
}

func (s *service) handleListFolderEndpointError(err error, w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	logger.Error().Log("error", err)
	if codeErr, ok := err.(lib.Error); ok {
		if codeErr.Code() == lib.CodeNotFound {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if codeErr.Code() == lib.CodeBadInputData {
			jsonErr, err := s.wec.ErrorToJSON(codeErr)
			if err != nil {
				s.logger.Error().Log("error", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			w.WriteHeader(http.StatusBadRequest)
			w.Write(jsonErr)
			return
		}
	}

	logger.Error().Log("error", err, "msg", "unexpected error listing folder")
	w.WriteHeader(http.StatusInternalServerError)
	return
}

func (s *service) moveEndpoint(w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	user := s.cm.MustGetUser(r.Context())

	req := &moveRequest{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		logger.Error().Log("error", err)
		codeErr := badRequestError("invalid json")
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

	sourcePath := filepath.Clean("/" + req.Source)
	targetPath := filepath.Clean("/" + req.Target)
	if sourcePath == "/" || targetPath == "/" {
		logger.Warn().Log("msg", "lib can not be moved")
		w.WriteHeader(http.StatusForbidden)
		return
	}

	err := s.metaDataDriver.Move(r.Context(), user, sourcePath, targetPath)
	if err != nil {
		s.handleMoveEndpointError(err, w, r)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *service) handleMoveEndpointError(err error, w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	logger.Error().Log("error", err)
	if codeErr, ok := err.(lib.Error); ok {
		if codeErr.Code() == lib.CodeNotFound {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if codeErr.Code() == lib.CodeBadInputData {
			jsonErr, err := s.wec.ErrorToJSON(codeErr)
			if err != nil {
				s.logger.Error().Log("error", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			w.WriteHeader(http.StatusBadRequest)
			w.Write(jsonErr)
			return
		}
	}

	logger.Error().Log("error", err, "msg", "unexpected error moving file")
	w.WriteHeader(http.StatusInternalServerError)
	return
}

func (s *service) deleteEndpoint(w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	user := s.cm.MustGetUser(r.Context())

	req := &pathRequest{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		logger.Error().Log("error", err)
		codeErr := badRequestError("invalid json")
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
		logger.Warn().Log("msg", "lib can not be deleted")
		w.WriteHeader(http.StatusForbidden)
		return
	}

	err := s.metaDataDriver.Delete(r.Context(), user, req.Path)
	if err != nil {
		s.handleDeleteEndpointError(err, w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *service) handleDeleteEndpointError(err error, w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	logger.Error().Log("error", err, "msg", "unexpected error deleting file")
	w.WriteHeader(http.StatusInternalServerError)
	return
}

func (s *service) makeFolderEndpoint(w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	user := s.cm.MustGetUser(r.Context())

	req := &pathRequest{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		logger.Error().Log("error", err)
		codeErr := badRequestError("invalid json")
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

	err := s.metaDataDriver.CreateFolder(r.Context(), user, req.Path)
	if err != nil {
		s.handleMakeFolderEndpointError(err, w, r)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *service) handleMakeFolderEndpointError(err error, w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	if codeErr, ok := err.(lib.Error); ok {
		if codeErr.Code() == lib.CodeNotFound {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if codeErr.Code() == lib.CodeBadInputData {
			jsonErr, err := s.wec.ErrorToJSON(codeErr)
			if err != nil {
				s.logger.Error().Log("error", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			w.WriteHeader(http.StatusBadRequest)
			w.Write(jsonErr)
			return
		}
		if codeErr.Code() == lib.CodeAlreadyExist {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	logger.Error().Log("error", err, "msg", "unexpected error making folder")
	w.WriteHeader(http.StatusInternalServerError)
	return
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

func fileInfoToFileInfoResponse(fileInfo lib.FileInfo) *fileInfoResponse {
	return &fileInfoResponse{
		Path:            fileInfo.Path(),
		Folder:          fileInfo.Folder(),
		Size:            fileInfo.Size(),
		Modified:        fileInfo.Modified(),
		Checksum:        fileInfo.Checksum(),
		ExtraAttributes: fileInfo.ExtraAttributes(),
	}
}

type fileInfoResponse struct {
	Path            string                 `json:"path"`
	Folder          bool                   `json:"folder"`
	Size            int64                  `json:"size"`
	Modified        int64                  `json:"modified"`
	Checksum        string                 `json:"checksum"`
	ExtraAttributes map[string]interface{} `json:"extra_attributes"`
}

type pathRequest struct {
	Path string `json:"path"`
}

type moveRequest struct {
	Source string `json:"source"`
	Target string `json:"target"`
}
