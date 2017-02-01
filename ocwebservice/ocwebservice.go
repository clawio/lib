package ocwebservice

import (
	"net/http"

	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/clawio/lib"
	"github.com/go-kit/kit/log/levels"
	"github.com/gorilla/mux"
	"io"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type service struct {
	cm                lib.ContextManager
	logger            levels.Levels
	dataDriver        lib.DataDriver
	metaDataDriver    lib.MetaDataDriver
	bam               lib.BasicAuthMiddleware
	wec               lib.WebErrorConverter
	mg                lib.MimeGuesser
	uploadMaxFileSize int64
}

func New(
	cm lib.ContextManager,
	logger levels.Levels,
	dataDriver lib.DataDriver,
	metaDataDriver lib.MetaDataDriver,
	bam lib.BasicAuthMiddleware,
	wec lib.WebErrorConverter,
	mg lib.MimeGuesser,
	uploadMaxFileSize int64) lib.WebService {
	return &service{
		cm:                cm,
		logger:            logger,
		dataDriver:        dataDriver,
		metaDataDriver:    metaDataDriver,
		bam:               bam,
		wec:               wec,
		mg:                mg,
		uploadMaxFileSize: uploadMaxFileSize,
	}
}

func (s *service) IsProxy() bool {
	return false
}

func (s *service) Endpoints() map[string]map[string]http.HandlerFunc {
	return map[string]map[string]http.HandlerFunc{
		"/ocwebdav/status.php": {
			"GET": s.statusEndpoint,
		},
		"/ocwebdav/ocs/v1.php/cloud/capabilities": {
			"GET": s.capabilitiesEndpoint,
		},
		"/ocwebdav/remote.php/webdav/{path:.*}": {
			"GET":       s.bam.HandlerFunc(s.getEndpoint),
			"PUT":       s.bam.HandlerFunc(s.putEndpoint),
			"OPTIONS":   s.bam.HandlerFunc(s.optionsEndpoint),
			"LOCK":      s.bam.HandlerFunc(s.lockEndpoint),
			"UNLOCK":    s.bam.HandlerFunc(s.unlockEndpoint),
			"HEAD":      s.bam.HandlerFunc(s.headEndpoint),
			"MKCOL":     s.bam.HandlerFunc(s.mkcolEndpoint),
			"PROPPATCH": s.bam.HandlerFunc(s.proppatchEndpoint),
			"PROPFIND":  s.bam.HandlerFunc(s.propfindEndpoint),
			"DELETE":    s.bam.HandlerFunc(s.deleteEndpoint),
			"MOVE":      s.bam.HandlerFunc(s.moveEndpoint),
		},
	}
}

func (s *service) statusEndpoint(w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())

	major := "8"
	minor := "2"
	micro := "1"
	edition := ""

	version := fmt.Sprintf("%s.%s.%s.4", major, minor, micro)
	versionString := fmt.Sprintf("%s.%s.%s", major, minor, micro)

	status := &struct {
		Installed     bool   `json:"installed"`
		Maintenance   bool   `json:"maintenance"`
		Version       string `json:"version"`
		VersionString string `json:"versionstring"`
		Edition       string `json:"edition"`
	}{
		true,
		false,
		version,
		versionString,
		edition,
	}

	statusJSON, err := json.MarshalIndent(status, "", "    ")
	if err != nil {
		logger.Error().Log("error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(statusJSON)

}

func (s *service) capabilitiesEndpoint(w http.ResponseWriter, r *http.Request) {
	capabilities := `
	{
	  "ocs": {
	    "data": {
	      "capabilities": {
	        "core": {
	          "pollinterval": 60
	        },
	        "files": {
	          "bigfilechunking": true,
	          "undelete": true,
	          "versioning": true
	        }
	      },
	      "version": {
	        "edition": "",
	        "major": 8,
	        "micro": 1,
	        "minor": 2,
	        "string": "8.2.1"
	      }
	    },
	    "meta": {
	      "message": null,
	      "status": "ok",
	      "statuscode": 100
	    }
	  }
	}`

	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(capabilities))
}

func (s *service) getEndpoint(w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	user := s.cm.MustGetUser(r.Context())
	path := mux.Vars(r)["path"]

	fileInfo, err := s.metaDataDriver.Examine(r.Context(), user, path)
	if err != nil {
		s.handleGetEndpointError(err, w, r)
		return
	}

	extraAttributes := fileInfo.ExtraAttributes()
	if extraAttributes == nil {
		logger.Crit().Log("error", "no extra attributes in file fileInfo", "msg", "ocwebservice running without a valid metadata driver")
		w.WriteHeader(http.StatusInternalServerError)
	}
	etag, ok := extraAttributes["etag"].(string)
	if !ok {
		codeErr := metaDataDriverNotSupportedError("etag attribute is not a string")
		logger.Crit().Log("error", codeErr, "msg", "ocwebservice running without a valid metadata driver")
		w.WriteHeader(http.StatusInternalServerError)
	}
	id, ok := extraAttributes["id"].(string)
	if !ok {
		codeErr := metaDataDriverNotSupportedError("id attribute is not a string")
		logger.Crit().Log("error", codeErr, "msg", "ocwebservice running without a valid metadata driver")
		w.WriteHeader(http.StatusInternalServerError)
	}
	if !ok {
		codeErr := metaDataDriverNotSupportedError("etag attribute is not a string")
		logger.Crit().Log("error", codeErr, "msg", "ocwebservice running without a valid metadata driver")
		w.WriteHeader(http.StatusInternalServerError)
	}
	if etag == "" {
		logger.Crit().Log("error", "etag is empty")
		w.WriteHeader(http.StatusInternalServerError)
	}
	if id == "" {
		logger.Crit().Log("error", "id is empty")
		w.WriteHeader(http.StatusInternalServerError)
	}

	if fileInfo.Folder() {
		logger.Warn().Log("file is a folder")
		w.WriteHeader(http.StatusNotImplemented)
		return
	}

	readCloser, err := s.dataDriver.DownloadFile(r.Context(), user, path)
	if err != nil {
		s.handleGetEndpointError(err, w, r)
		return
	}
	defer readCloser.Close()

	w.Header().Set("Content-Type", s.mg.FromString(fileInfo.Path()))
	w.Header().Set("ETag", etag)
	w.Header().Set("OC-FileId", id)
	w.Header().Set("OC-ETag", etag)
	t := time.Unix(fileInfo.Modified()/1000000000, fileInfo.Modified()%1000000000)
	lastModifiedString := t.Format(time.RFC1123)
	w.Header().Set("Last-Modified", lastModifiedString)
	if fileInfo.Checksum() != "" {
		w.Header().Set("OC-Checksum", fileInfo.Checksum())
	}

	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, readCloser); err != nil {
		logger.Error().Log("error", err, "msg", "error writting response body")
	}
}

func (s *service) headEndpoint(w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	user := s.cm.MustGetUser(r.Context())
	path := mux.Vars(r)["path"]

	fileInfo, err := s.metaDataDriver.Examine(r.Context(), user, path)
	if err != nil {
		s.handleHeadEndpointError(err, w, r)
		return
	}

	extraAttributes := fileInfo.ExtraAttributes()
	if extraAttributes == nil {
		logger.Crit().Log("error", "no extra attributes in file fileInfo", "msg", "ocwebservice running without a valid metadata driver")
		w.WriteHeader(http.StatusInternalServerError)
	}
	etag, ok := extraAttributes["etag"].(string)
	if !ok {
		codeErr := metaDataDriverNotSupportedError("etag attribute is not a string")
		logger.Crit().Log("error", codeErr, "msg", "ocwebservice running without a valid metadata driver")
		w.WriteHeader(http.StatusInternalServerError)
	}
	id, ok := extraAttributes["id"].(string)
	if !ok {
		codeErr := metaDataDriverNotSupportedError("id attribute is not a string")
		logger.Crit().Log("error", codeErr, "msg", "ocwebservice running without a valid metadata driver")
		w.WriteHeader(http.StatusInternalServerError)
	}
	if !ok {
		codeErr := metaDataDriverNotSupportedError("id attribute is not a string")
		logger.Crit().Log("error", codeErr, "msg", "ocwebservice running without a valid metadata driver")
		w.WriteHeader(http.StatusInternalServerError)
	}
	if etag == "" {
		logger.Crit().Log("error", "etag is empty")
		w.WriteHeader(http.StatusInternalServerError)
	}
	if id == "" {
		logger.Crit().Log("error", "id is empty")
		w.WriteHeader(http.StatusInternalServerError)
	}

	w.Header().Add("Content-Type", s.mg.FromFileInfo(fileInfo))
	w.Header().Set("ETag", etag)
	w.Header().Set("OC-FileId", id)
	w.Header().Set("OC-ETag", etag)
	t := time.Unix(fileInfo.Modified()/1000000000, fileInfo.Modified()%1000000000)
	lastModifiedString := t.Format(time.RFC1123)
	w.Header().Set("Last-Modified", lastModifiedString)
	w.WriteHeader(http.StatusOK)
}

func (s *service) optionsEndpoint(w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	user := s.cm.MustGetUser(r.Context())
	path := mux.Vars(r)["path"]

	fileInfo, err := s.metaDataDriver.Examine(r.Context(), user, path)
	if err != nil {
		s.handleOptionsEndpointError(err, w, r)
		return
	}

	extraAttributes := fileInfo.ExtraAttributes()
	if extraAttributes == nil {
		logger.Crit().Log("error", "no extra attributes in file fileInfo", "msg", "ocwebservice running without a valid metadata driver")
		w.WriteHeader(http.StatusInternalServerError)
	}
	etag, ok := extraAttributes["etag"]
	if !ok {
		codeErr := metaDataDriverNotSupportedError("etag attribute is not a string")
		logger.Crit().Log("error", codeErr, "msg", "ocwebservice running without a valid metadata driver")
		w.WriteHeader(http.StatusInternalServerError)
	}
	id, ok := extraAttributes["id"]
	if !ok {
		codeErr := metaDataDriverNotSupportedError("id attribute is not a string")
		logger.Crit().Log("error", codeErr, "msg", "ocwebservice running without a valid metadata driver")
		w.WriteHeader(http.StatusInternalServerError)
	}
	if etag == "" {
		logger.Crit().Log("error", "etag is empty")
		w.WriteHeader(http.StatusInternalServerError)
	}
	if id == "" {
		logger.Crit().Log("error", "id is empty")
		w.WriteHeader(http.StatusInternalServerError)
	}

	allow := "OPTIONS, LOCK, GET, HEAD, POST, DELETE, PROPPATCH, COPY,"
	allow += " MOVE, UNLOCK, PROPFIND"
	if !fileInfo.Folder() {
		allow += ", PUT"
	}

	w.Header().Set("Allow", allow)
	w.Header().Set("DAV", "1, 2")
	w.Header().Set("MS-Author-Via", "DAV")
	w.WriteHeader(http.StatusOK)
	return
}

func (s *service) deleteEndpoint(w http.ResponseWriter, r *http.Request) {
	user := s.cm.MustGetUser(r.Context())
	path := mux.Vars(r)["path"]

	err := s.metaDataDriver.Delete(r.Context(), user, path)
	if err != nil {
		s.handleDeleteEndpointError(err, w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *service) lockEndpoint(w http.ResponseWriter, r *http.Request) {
	xml := `<?xml version="1.0" encoding="utf-8"?>
	<prop xmlns="DAV:">
		<lockdiscovery>
			<activelock>
				<allprop/>
				<timeout>Second-604800</timeout>
				<depth>Infinity</depth>
				<locktoken>
				<href>opaquelocktoken:00000000-0000-0000-0000-000000000000</href>
				</locktoken>
			</activelock>
		</lockdiscovery>
	</prop>`

	w.Header().Set("Content-Type", "text/xml; charset=\"utf-8\"")
	w.Header().Set("Lock-Token",
		"opaquelocktoken:00000000-0000-0000-0000-000000000000")
	w.Write([]byte(xml))
}

func (s *service) unlockEndpoint(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *service) mkcolEndpoint(w http.ResponseWriter, r *http.Request) {
	user := s.cm.MustGetUser(r.Context())
	path := mux.Vars(r)["path"]

	err := s.metaDataDriver.CreateFolder(r.Context(), user, path)
	if err != nil {
		s.handleMkcolEndpointError(err, w, r)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *service) proppatchEndpoint(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
func (s *service) moveEndpoint(w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	user := s.cm.MustGetUser(r.Context())
	path := mux.Vars(r)["path"]

	destination := r.Header.Get("Destination")
	overwrite := r.Header.Get("Overwrite")

	if destination == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	destinationURL, err := url.ParseRequestURI(destination)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	overwrite = strings.ToUpper(overwrite)
	if overwrite == "" {
		overwrite = "T"
	}

	if overwrite != "T" && overwrite != "F" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// remove api base and service base to get real path
	//toTrim := filepath.Join("/", dirs.Server.BaseURL, dirs.OCWebDAV.BaseURL) + "/ocwebdav/remote.php/webdav/"
	toTrim := "/ocwebdav/remote.php/webdav/"
	destination = strings.TrimPrefix(destinationURL.Path, toTrim)

	err = s.metaDataDriver.Move(r.Context(), user, path, destination)
	if err != nil {
		s.handleMoveEndpointError(err, w, r)
		return
	}

	fileInfo, err := s.metaDataDriver.Examine(r.Context(), user, destination)
	if err != nil {
		s.handleMoveEndpointError(err, w, r)
		return
	}

	extraAttributes := fileInfo.ExtraAttributes()
	if extraAttributes == nil {
		logger.Crit().Log("error", "no extra attributes in file fileInfo", "msg", "ocwebservice running without a valid metadata driver")
		w.WriteHeader(http.StatusInternalServerError)
	}
	etag, ok := extraAttributes["etag"].(string)
	if !ok {
		codeErr := metaDataDriverNotSupportedError("etag attribute is not a string")
		logger.Crit().Log("error", codeErr, "msg", "ocwebservice running without a valid metadata driver")
		w.WriteHeader(http.StatusInternalServerError)
	}
	id, ok := extraAttributes["id"].(string)
	if !ok {
		codeErr := metaDataDriverNotSupportedError("id attribute is not a string")
		logger.Crit().Log("error", codeErr, "msg", "ocwebservice running without a valid metadata driver")
		w.WriteHeader(http.StatusInternalServerError)
	}
	if etag == "" {
		logger.Crit().Log("error", "etag is empty")
		w.WriteHeader(http.StatusInternalServerError)
	}
	if id == "" {
		logger.Crit().Log("error", "id is empty")
		w.WriteHeader(http.StatusInternalServerError)
	}

	w.Header().Set("ETag", etag)
	w.Header().Set("OC-FileId", id)
	w.Header().Set("OC-ETag", etag)

	// ownCloud want a 201 instead of 204
	w.WriteHeader(http.StatusCreated)
}

func (s *service) putEndpoint(w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	if r.Body == nil {
		logger.Error().Log("error", "body is <nil>")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	user := s.cm.MustGetUser(r.Context())
	path := mux.Vars(r)["path"]

	// if request is a chunk upload we handle it in another method
	isChunked, err := s.isChunkedUpload(path)
	if err != nil {
		logger.Error().Log("error", "error applying chunk regex to path", "path", path)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if isChunked {
		logger.Info().Log("msg", "upload is chunked")
		s.putChunkedEndpoint(w, r)
		return
	}

	if s.requestHasContentRange(r) {
		logger.Warn().Log("msg", "content-range header is not accepted on put requests")
		w.WriteHeader(http.StatusNotImplemented)
		return
	}

	if s.requestSuffersFinderProblem(r) {
		if err := s.handleFinderRequest(w, r); err != nil {
			return
		}
	}

	fileInfo, err := s.metaDataDriver.Examine(r.Context(), user, path)
	// if err is not found it is okay to continue
	if err != nil {
		if !s.isNotFoundError(err) {
			s.handlePutEndpointError(err, w, r)
			return
		}
	}

	if fileInfo != nil && fileInfo.Folder() {
		logger.Warn().Log("msg", "file already exists and is a folder", "path", fileInfo.Path())
		w.WriteHeader(http.StatusConflict)
		return
	}

	// if If-Match header contains an Etag we need to check it against the ETag from the server
	// so see if they match or not. If they do not match, StatusPreconditionFailed is returned
	if fileInfo != nil {
		extraAttributes := fileInfo.ExtraAttributes()
		if extraAttributes == nil {
			logger.Crit().Log("error", "no extra attributes in file fileInfo", "msg", "ocwebservice running without a valid metadata driver")
			w.WriteHeader(http.StatusInternalServerError)
		}
		etag, ok := extraAttributes["etag"].(string)
		if !ok {
			codeErr := metaDataDriverNotSupportedError("etag attribute is not a string")
			logger.Crit().Log("error", codeErr, "msg", "ocwebservice running without a valid metadata driver")
			w.WriteHeader(http.StatusInternalServerError)
		}
		id, ok := extraAttributes["id"].(string)
		if !ok {
			codeErr := metaDataDriverNotSupportedError("id attribute is not a string")
			logger.Crit().Log("error", codeErr, "msg", "ocwebservice running without a valid metadata driver")
			w.WriteHeader(http.StatusInternalServerError)
		}
		if etag == "" {
			logger.Crit().Log("error", "etag is empty")
			w.WriteHeader(http.StatusInternalServerError)
		}
		if id == "" {
			logger.Crit().Log("error", "id is empty")
			w.WriteHeader(http.StatusInternalServerError)
		}

		clientETag := r.Header.Get("If-Match")
		serverETag := etag
		if clientETag != "" {
			if err := s.handleIfMatchHeader(clientETag, serverETag, w, r); err != nil {
				return
			}
		}
	}

	readCloser := http.MaxBytesReader(w, r.Body, s.uploadMaxFileSize)
	if err := s.dataDriver.UploadFile(r.Context(), user, path, readCloser, ""); err != nil {
		s.handlePutEndpointError(err, w, r)
		return
	}

	newInfo, err := s.metaDataDriver.Examine(r.Context(), user, path)
	if err != nil {
		s.handlePutEndpointError(err, w, r)
		return
	}

	extraAttributes := newInfo.ExtraAttributes()
	if extraAttributes == nil {
		logger.Crit().Log("error", "no extra attributes in file newInfo", "msg", "ocwebservice running without a valid metadata driver")
		w.WriteHeader(http.StatusInternalServerError)
	}
	etag, ok := extraAttributes["etag"].(string)
	if !ok {
		codeErr := metaDataDriverNotSupportedError("etag is not a string")
		logger.Crit().Log("error", codeErr, "msg", "ocwebservice running without a valid metadata driver")
		w.WriteHeader(http.StatusInternalServerError)
	}
	id, ok := extraAttributes["id"].(string)
	if !ok {
		codeErr := metaDataDriverNotSupportedError("id attribute is not a string")
		logger.Crit().Log("error", codeErr, "msg", "ocwebservice running without a valid metadata driver")
		w.WriteHeader(http.StatusInternalServerError)
	}
	if etag == "" {
		logger.Crit().Log("error", "etag is empty")
		w.WriteHeader(http.StatusInternalServerError)
	}
	if id == "" {
		logger.Crit().Log("error", "id is empty")
		w.WriteHeader(http.StatusInternalServerError)
	}

	w.Header().Add("Content-Type", s.mg.FromString(newInfo.Path()))
	w.Header().Set("ETag", etag)
	w.Header().Set("OC-FileId", id)
	w.Header().Set("OC-ETag", etag)
	t := time.Unix(newInfo.Modified()/1000000000, newInfo.Modified()%1000000000)
	lastModifiedString := t.Format(time.RFC1123)
	w.Header().Set("Last-Modified", lastModifiedString)
	w.Header().Set("X-OC-MTime", "accepted")

	// if object did not exist, http code is 201, else 204.
	if fileInfo == nil {
		w.WriteHeader(http.StatusCreated)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	return

}
func (s *service) putChunkedEndpoint(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	user := s.cm.MustGetUser(r.Context())
	logger := s.cm.MustGetLog(r.Context())
	path := mux.Vars(r)["path"]

	chunkInfo, err := getChunkBLOBInfo(path)
	if err != nil {
		logger.Error().Log("error", err, "msg", "error getting chunk fileInfo from path")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	readCloser := http.MaxBytesReader(w, r.Body, s.uploadMaxFileSize)
	err = s.dataDriver.UploadFile(r.Context(), user, path, readCloser, "")
	if err != nil {
		s.handlePutChunkedEndpointError(err, w, r)
		return
	}

	fileInfo, err := s.metaDataDriver.Examine(r.Context(), user, chunkInfo.path)
	if err != nil {
		s.handlePutEndpointError(err, w, r)
		return
	}

	extraAttributes := fileInfo.ExtraAttributes()
	if extraAttributes == nil {
		logger.Crit().Log("error", "no extra attributes in file fileInfo", "msg", "ocwebservice running without a valid metadata driver")
		w.WriteHeader(http.StatusInternalServerError)
	}
	etag, ok := extraAttributes["etag"].(string)
	if !ok {
		codeErr := metaDataDriverNotSupportedError("etag attribute is not a string")
		logger.Crit().Log("error", codeErr, "msg", "ocwebservice running without a valid metadata driver")
		w.WriteHeader(http.StatusInternalServerError)
	}
	id, ok := extraAttributes["id"].(string)
	if !ok {
		codeErr := metaDataDriverNotSupportedError("id attribute is not a string")
		logger.Crit().Log("error", codeErr, "msg", "ocwebservice running without a valid metadata driver")
		w.WriteHeader(http.StatusInternalServerError)
	}
	if etag == "" {
		logger.Crit().Log("error", "etag is empty")
		w.WriteHeader(http.StatusInternalServerError)
	}
	if id == "" {
		logger.Crit().Log("error", "id is empty")
		w.WriteHeader(http.StatusInternalServerError)
	}

	w.Header().Add("Content-Type", s.mg.FromString(fileInfo.Path()))
	w.Header().Set("ETag", etag)
	w.Header().Set("OC-FileId", id)
	w.Header().Set("OC-ETag", etag)
	t := time.Unix(fileInfo.Modified()/1000000000, fileInfo.Modified()%1000000000)
	lastModifiedString := t.Format(time.RFC1123)
	w.Header().Set("Last-Modified", lastModifiedString)
	w.Header().Set("X-OC-MTime", "accepted")

	// if object did not exist, http code is 201, else 204.
	if fileInfo == nil {
		w.WriteHeader(http.StatusCreated)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *service) propfindEndpoint(w http.ResponseWriter, r *http.Request) {
	user := s.cm.MustGetUser(r.Context())
	path := mux.Vars(r)["path"]

	var children bool
	depth := r.Header.Get("Depth")
	// TODO(labkode) Check default for infinity header
	if depth == "1" {
		children = true
	}

	var fileInfos []lib.FileInfo
	fileInfo, err := s.metaDataDriver.Examine(r.Context(), user, path)
	if err != nil {
		s.handlePropfindEndpointError(err, w, r)
		return
	}
	fileInfos = append(fileInfos, fileInfo)

	if children && fileInfo.Folder() {
		childrenInfos, err := s.metaDataDriver.ListFolder(r.Context(), user, path)
		if err != nil {
			s.handlePropfindEndpointError(err, w, r)
			return
		}
		fileInfos = append(fileInfos, childrenInfos...)
	}

	fileInfosInXML, err := s.fileInfosToXML(r.Context(), fileInfos)
	if err != nil {
		s.handlePropfindEndpointError(err, w, r)
		return
	}

	w.Header().Set("DAV", "1, 3, extended-mkcol")
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(207)
	w.Write([]byte(fileInfosInXML))

}

func (s *service) isChunkedUpload(path string) (bool, error) {
	return regexp.MatchString(`-chunking-\w+-[0-9]+-[0-9]+$`, path)
}

func (s *service) handleIfMatchHeader(clientETag, serverETag string, w http.ResponseWriter, r *http.Request) error {
	logger := s.cm.MustGetLog(r.Context())

	// ownCloud adds double quotes around ETag value
	serverETag = fmt.Sprintf(`"%s"`, serverETag)
	if clientETag != serverETag {
		err := fmt.Errorf("etags do not match")
		logger.Error().Log("msg", "can not accept conditional request", "client-etag", clientETag, "server-etag", serverETag)
		w.WriteHeader(http.StatusPreconditionFailed)
		return err
	}

	return nil
}

func (s *service) handleFinderRequest(w http.ResponseWriter, r *http.Request) error {
	logger := s.cm.MustGetLog(r.Context())

	/*
	   Many webservers will not cooperate well with Finder PUT requests,
	   because it uses 'Chunked' transfer encoding for the request body.
	   The symptom of this problem is that Finder sends files to the
	   server, but they arrive as 0-length files in PHP.
	   If we don't do anything, the user might think they are uploading
	   files successfully, but they end up empty on the server. Instead,
	   we throw back an error if we detect this.
	   The reason Finder uses Chunked, is because it thinks the files
	   might change as it's being uploaded, and therefore the
	   Content-Length can vary.
	   Instead it sends the X-Expected-Entity-Length header with the size
	   of the file at the very start of the request. If this header is set,
	   but we don't get a request body we will fail the request to
	   protect the end-user.
	*/
	logger.Warn().Log("msg", "finder problem intercepted", "content-length", r.Header.Get("Content-Length"), "x-expected-entity-length", r.Header.Get("X-Expected-Entity-Length"))

	// The best mitigation to this problem is to tell users to not use crappy Finder.
	// Another possible mitigation is to change the use the value of X-Expected-Entity-Length header in the Content-Length header.
	expected := r.Header.Get("X-Expected-Entity-Length")
	expectedInt, err := strconv.ParseInt(expected, 10, 64)
	if err != nil {
		logger.Error().Log("error", err)
		w.WriteHeader(http.StatusBadRequest)
		return err
	}
	r.ContentLength = expectedInt
	return nil
}

func (s *service) requestSuffersFinderProblem(r *http.Request) bool {
	return r.Header.Get("X-Expected-Entity-Length") != ""
}

func (s *service) requestHasContentRange(r *http.Request) bool {
	/*
	   Content-Range is dangerous for PUT requests:  PUT per definition
	   stores a full resource.  draft-ietf-httpbis-p2-semantics-15 says
	   in section 7.6:
	     An origin server SHOULD reject any PUT request that contains a
	     Content-Range header field, since it might be misinterpreted as
	     partial content (or might be partial content that is being mistakenly
	     PUT as a full representation).  Partial content updates are possible
	     by targeting a separately identified resource with state that
	     overlaps a portion of the larger resource, or by using a different
	     method that has been specifically defined for partial updates (for
	     example, the PATCH method defined in [RFC5789]).
	   This clarifies RFC2616 section 9.6:
	     The recipient of the entity MUST NOT ignore any Content-*
	     (e.g. Content-Range) headers that it does not understand or implement
	     and MUST return a 501 (Not Implemented) response in such cases.
	   OTOH is a PUT request with a Content-Range currently the only way to
	   continue an aborted upload request and is supported by curl, mod_dav,
	   Tomcat and others.  Since some clients do use this feature which results
	   in unexpected behaviour (cf PEAR::HTTP_WebDAV_Client 1.0.1), we reject
	   all PUT requests with a Content-Range for now.
	*/
	return r.Header.Get("Content-Range") != ""
}

func (s *service) isNotFoundError(err error) bool {
	codeErr, ok := err.(lib.Error)
	if !ok {
		return false
	}
	if codeErr.Code() == lib.CodeNotFound {
		return true
	}
	return false
}

type chunkHeaderInfo struct {
	// OC-Chunked = 1
	ochunked bool

	// OC-Chunk-Size
	ocChunkSize uint64

	// OC-Total-Length
	ocTotalLength uint64
}

type chunkBLOBInfo struct {
	path         string
	transferID   string
	totalChunks  int64
	currentChunk int64
}

// not using the resource path in the chunk folder name allows uploading
// to the same folder after a move without having to restart the chunk
// upload
func (c *chunkBLOBInfo) uploadID() string {
	return fmt.Sprintf("chunking-%s-%d", c.transferID, c.totalChunks)
}

func getChunkBLOBInfo(path string) (*chunkBLOBInfo, error) {
	parts := strings.Split(path, "-chunking-")
	tail := strings.Split(parts[1], "-")

	totalChunks, err := strconv.ParseInt(tail[1], 10, 64)
	if err != nil {
		return nil, err
	}

	currentChunk, err := strconv.ParseInt(tail[2], 10, 64)
	if err != nil {
		return nil, err
	}

	if currentChunk >= totalChunks {
		return nil, fmt.Errorf("current chunk:%d exceeds total number of chunks:%d", currentChunk, totalChunks)
	}

	return &chunkBLOBInfo{
		path:         parts[0],
		transferID:   tail[0],
		totalChunks:  totalChunks,
		currentChunk: currentChunk,
	}, nil
}

func (s *service) handleGetEndpointError(err error, w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	if codeErr, ok := err.(lib.Error); ok {
		if codeErr.Code() == lib.CodeNotFound {
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}
	logger.Error().Log("error", "unexpected error getting file")
	w.WriteHeader(http.StatusInternalServerError)
	return
}

func (s *service) handleHeadEndpointError(err error, w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	if codeErr, ok := err.(lib.Error); ok {
		if codeErr.Code() == lib.CodeNotFound {
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}
	logger.Error().Log("error", "unexpected error heading file")
	w.WriteHeader(http.StatusInternalServerError)
	return
}

func (s *service) handleDeleteEndpointError(err error, w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	logger.Error().Log("error", "unexpected error deleting file")
	w.WriteHeader(http.StatusInternalServerError)
	return
}

func (s *service) handleMkcolEndpointError(err error, w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	logger.Error().Log("error", "unexpected error creating folder")
	w.WriteHeader(http.StatusInternalServerError)
	return
}

func (s *service) handleMoveEndpointError(err error, w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	logger.Error().Log("error", "unexpected error moving file")
	w.WriteHeader(http.StatusInternalServerError)
	return
}

func (s *service) handleOptionsEndpointError(err error, w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	if codeErr, ok := err.(lib.Error); ok {
		if codeErr.Code() == lib.CodeNotFound {
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}
	logger.Error().Log("error", "unexpected error optioning file")
	w.WriteHeader(http.StatusInternalServerError)
	return
}

func (s *service) handlePutEndpointError(err error, w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	logger.Error().Log("error", err)

	if err.Error() == "http: request body too large" {
		logger.Error().Log("error", "request body max size exceed")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}

	if codeErr, ok := err.(lib.Error); ok {
		if codeErr.Code() == lib.CodeNotFound {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if codeErr.Code() == lib.CodeBadChecksum {
			logger.Error().Log("error", "checksum mismatch")
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		if codeErr.Code() == lib.CodeBadChecksum {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		if codeErr.Code() == lib.CodeUploadIsPartial {
			w.WriteHeader(http.StatusPartialContent)
			return
		}
		if codeErr.Code() == lib.CodeForbidden {
			w.WriteHeader(http.StatusForbidden)
			return
		}
	}

	logger.Error().Log("unexpected error puting file")
	w.WriteHeader(http.StatusInternalServerError)
	return
}

func (s *service) handlePutChunkedEndpointError(err error, w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	logger.Error().Log("error", err)

	if codeErr, ok := err.(lib.Error); ok {
		if codeErr.Code() == lib.CodeUploadIsPartial {
			w.WriteHeader(http.StatusCreated)
			return
		}
	}
	s.handlePutEndpointError(err, w, r)
	return
}

func (s *service) handlePropfindEndpointError(err error, w http.ResponseWriter, r *http.Request) {
	logger := s.cm.MustGetLog(r.Context())
	logger.Error().Log("error", err)
	if codeErr, ok := err.(lib.Error); ok {
		if codeErr.Code() == lib.CodeNotFound {
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}
	logger.Error().Log("msg", "unexpected error propfinding file")
	w.WriteHeader(http.StatusInternalServerError)
	return
}

func (s *service) fileInfosToXML(ctx context.Context, fileInfos []lib.FileInfo) (string, error) {
	responses := []*responseXML{}
	for _, fileInfo := range fileInfos {
		res, err := s.fileInfoToPropResponse(ctx, fileInfo)
		if err != nil {
			return "", err
		}
		responses = append(responses, res)
	}
	responsesXML, err := xml.Marshal(&responses)
	if err != nil {
		return "", err
	}

	msg := `<?xml version="1.0" encoding="utf-8"?><d:multistatus xmlns:d="DAV:" `
	msg += `xmlns:s="http://sabredav.org/ns" xmlns:oc="http://owncloud.org/ns">`
	msg += string(responsesXML) + `</d:multistatus>`
	return msg, nil
}

func (s *service) fileInfoToPropResponse(ctx context.Context, fileInfo lib.FileInfo) (*responseXML, error) {
	logger := s.cm.MustGetLog(ctx)
	extraAttributes := fileInfo.ExtraAttributes()
	if extraAttributes == nil {
		codeErr := metaDataDriverNotSupportedError("no extra attributes in file fileInfo")
		logger.Crit().Log("error", codeErr, "msg", "ocwebservice running without a valid metadata driver")
		return nil, codeErr
	}

	etag, ok := extraAttributes["etag"].(string)
	if !ok {
		codeErr := metaDataDriverNotSupportedError("etag attribute is not a string")
		logger.Crit().Log("error", codeErr, "msg", "ocwebservice running without a valid metadata driver")
		return nil, codeErr
	}
	id, ok := extraAttributes["id"].(string)
	if !ok {
		codeErr := metaDataDriverNotSupportedError("id attribute is not a string")
		logger.Crit().Log("error", codeErr, "msg", "ocwebservice running without a valid metadata driver")
		return nil, codeErr
	}

	if etag == "" {
		codeErr := metaDataDriverNotSupportedError("etag is empty")
		logger.Crit().Log("error", codeErr, "msg", "ocwebservice running without a valid metadata driver")
		return nil, codeErr
	}
	if id == "" {
		codeErr := metaDataDriverNotSupportedError("id is empty")
		logger.Crit().Log("error", codeErr, "msg", "ocwebservice running without a valid metadata driver")
		return nil, codeErr
	}

	// TODO: clean a little bit this and refactor creation of properties
	propList := []propertyXML{}

	getETag := propertyXML{
		xml.Name{Space: "", Local: "d:getetag"},
		"", []byte(etag)}

	ocPermissions := propertyXML{xml.Name{Space: "", Local: "oc:permissions"},
		"", []byte("RDNVW")}

	quotaUsedBytes := propertyXML{
		xml.Name{Space: "", Local: "d:quota-used-bytes"}, "", []byte("0")}

	quotaAvailableBytes := propertyXML{
		xml.Name{Space: "", Local: "d:quota-available-bytes"}, "",
		[]byte("1000000000")}

	getContentLegnth := propertyXML{
		xml.Name{Space: "", Local: "d:getcontentlength"},
		"", []byte(fmt.Sprintf("%d", fileInfo.Size()))}

	getContentType := propertyXML{
		xml.Name{Space: "", Local: "d:getcontenttype"},
		"", []byte(s.mg.FromFileInfo(fileInfo))}

	// Finder needs the the getLastModified property to work.
	t := time.Unix(int64(fileInfo.Modified()/1000000000), int64(fileInfo.Modified()%1000000000))
	lasModifiedString := t.Format(time.RFC1123)
	getLastModified := propertyXML{
		xml.Name{Space: "", Local: "d:getlastmodified"},
		"", []byte(lasModifiedString)}

	getResourceType := propertyXML{
		xml.Name{Space: "", Local: "d:resourcetype"},
		"", []byte("")}

	if fileInfo.Folder() {
		getResourceType.InnerXML = []byte("<d:collection/>")
		getContentType.InnerXML = []byte(s.mg.FromFileInfo(fileInfo))
		ocPermissions.InnerXML = []byte("RDNVCK")
	}

	ocID := propertyXML{xml.Name{Space: "", Local: "oc:id"}, "",
		[]byte(id)}

	ocDownloadURL := propertyXML{xml.Name{Space: "", Local: "oc:downloadURL"},
		"", []byte("")}

	ocDC := propertyXML{xml.Name{Space: "", Local: "oc:dDC"},
		"", []byte("")}

	propList = append(propList, getResourceType, getContentLegnth, getContentType, getLastModified, // general WebDAV properties
		getETag, quotaAvailableBytes, quotaUsedBytes, ocID, ocDownloadURL, ocDC) // properties needed by ownCloud

	// PropStat, only HTTP/1.1 200 is sent.
	propStatList := []propstatXML{}

	propStat := propstatXML{}
	propStat.Prop = propList
	propStat.Status = "HTTP/1.1 200 OK"
	propStatList = append(propStatList, propStat)

	response := responseXML{}

	response.Href = filepath.Join("/ocwebdav/remote.php/webdav", fileInfo.Path())
	if fileInfo.Folder() {
		response.Href = filepath.Join("/ocwebdav/remote.php/webdav", fileInfo.Path()) + "/"
	}

	response.Propstat = propStatList

	return &response, nil

}

type responseXML struct {
	XMLName             xml.Name      `xml:"d:response"`
	Href                string        `xml:"d:href"`
	Propstat            []propstatXML `xml:"d:propstat"`
	Status              string        `xml:"d:status,omitempty"`
	Error               *errorXML     `xml:"d:error"`
	ResponseDescription string        `xml:"d:responsedescription,omitempty"`
}

// http://www.ocwebdav.org/specs/rfc4918.html#ELEMENT_propstat
type propstatXML struct {
	// Prop requires DAV: to be the default namespace in the enclosing
	// XML. This is due to the standard encoding/xml package currently
	// not honoring namespace declarations inside a xmltag with a
	// parent element for anonymous slice elements.
	// Use of multistatusWriter takes care of this.
	Prop                []propertyXML `xml:"d:prop>_ignored_"`
	Status              string        `xml:"d:status"`
	Error               *errorXML     `xml:"d:error"`
	ResponseDescription string        `xml:"d:responsedescription,omitempty"`
}

// Property represents a single DAV resource property as defined in RFC 4918.
// http://www.ocwebdav.org/specs/rfc4918.html#data.model.for.resource.properties
type propertyXML struct {
	// XMLName is the fully qualified name that identifies this property.
	XMLName xml.Name

	// Lang is an optional xml:lang attribute.
	Lang string `xml:"xml:lang,attr,omitempty"`

	// InnerXML contains the XML representation of the property value.
	// See http://www.ocwebdav.org/specs/rfc4918.html#property_values
	//
	// Property values of complex type or mixed-content must have fully
	// expanded XML namespaces or be self-contained with according
	// XML namespace declarations. They must not rely on any XML
	// namespace declarations within the scope of the XML document,
	// even including the DAV: namespace.
	InnerXML []byte `xml:",innerxml"`
}

// http://www.ocwebdav.org/specs/rfc4918.html#ELEMENT_error
type errorXML struct {
	XMLName  xml.Name `xml:"d:error"`
	InnerXML []byte   `xml:",innerxml"`
}

type metaDataDriverNotSupportedError string

func (e metaDataDriverNotSupportedError) Error() string {
	return string(e)
}
func (e metaDataDriverNotSupportedError) Code() lib.Code {
	return lib.Code(lib.CodeInternal)
}
func (e metaDataDriverNotSupportedError) Message() string {
	return string(e)
}
