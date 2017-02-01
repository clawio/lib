package lib

import (
	"context"
	"github.com/go-kit/kit/log/levels"
	"io"
	"net/http"
)

const (
	// WARNING: ADD NEW CODES TO THE END TO NOT BREAK THE API

	// InvalidToken is returned when the auth token is invalid or has expired
	CodeInvalidToken Code = iota
	// Unauthenticated is returned when authentication is needed for execution.
	CodeUnauthorized
	// BadAuthenticationData is returned when the authentication fails.
	CodeBadAuthenticationData
	// BadInputData is returned when the input parameters are not valid.
	CodeBadInputData
	// NotFound is returned when something cannot be found.
	CodeNotFound
	// BadChecksum is returned when two checksum differs.
	CodeBadChecksum
	// TooBig is returned when something is too big to be processed.
	CodeTooBig
	// CodeUserNotFound
	CodeUserNotFound
	// CodeInternal
	CodeInternal
	// CodeAlreadyExist
	CodeAlreadyExist
	// CodeUploadIsPartial is the error to return when the upload of file
	// is in a partial state, like an owncloud chunk upload where the upload of a chunk
	// does not complete the upload.
	CodeUploadIsPartial
	// CodeForbidden is used when something is forbidden, like uploading to lib
	CodeForbidden
)

type (
	Code uint32

	Error interface {
		error
		Code() Code
		Message() string
	}

	User interface {
		Username() string
		Email() string
		DisplayName() string
		ExtraAttributes() map[string]interface{}
	}

	FileInfo interface {
		Path() string
		Folder() bool
		Size() int64
		Modified() int64
		Checksum() string
		ExtraAttributes() map[string]interface{}
	}

	DataDriver interface {
		UploadFile(ctx context.Context, user User, path string, r io.ReadCloser, clientChecksum string) error
		DownloadFile(ctx context.Context, user User, path string) (io.ReadCloser, error)
	}

	MetaDataDriver interface {
		Examine(ctx context.Context, user User, path string) (FileInfo, error)
		Move(ctx context.Context, user User, sourcePath, targetPath string) error
		Delete(ctx context.Context, user User, path string) error
		ListFolder(ctx context.Context, user User, path string) ([]FileInfo, error)
		CreateFolder(ctx context.Context, user User, path string) error
	}

	UserDriver interface {
		GetByCredentials(username, password string) (User, error)
	}

	TokenDriver interface {
		CreateToken(user User) (string, error)
		UserFromToken(token string) (User, error)
	}

	RegistryNode interface {
		ID() string
		Rol() string
		Version() string
		Host() string
		URL() string
	}

	RegistryDriver interface {
		Register(ctx context.Context, node RegistryNode) error
		//UnRegister(ctx context.Context, id string) error
		GetNodesForRol(ctx context.Context, rol string) ([]RegistryNode, error)
	}

	ContextManager interface {
		GetLog(ctx context.Context) (*levels.Levels, bool)
		MustGetLog(ctx context.Context) *levels.Levels
		SetLog(ctx context.Context, logger *levels.Levels) context.Context
		GetTraceID(ctx context.Context) (string, bool)
		MustGetTraceID(ctx context.Context) string
		SetTraceID(ctx context.Context, traceId string) context.Context
		GetUser(ctx context.Context) (User, bool)
		MustGetUser(ctx context.Context) User
		SetUser(ctx context.Context, user User) context.Context
		GetAccessToken(ctx context.Context) (string, bool)
		MustGetAccessToken(ctx context.Context) string
		SetAccessToken(ctx context.Context, token string) context.Context
	}

	AuthenticationMiddleware interface {
		HandlerFunc(handlerFunc http.HandlerFunc) http.HandlerFunc
	}

	LoggerMiddleware interface {
		HandlerFunc(handlerFunc http.HandlerFunc) http.HandlerFunc
	}

	CorsMiddleware interface {
		Handler(handler http.Handler) http.Handler
	}

	BasicAuthMiddleware interface {
		HandlerFunc(handlerFunc http.HandlerFunc) http.HandlerFunc
	}

	WebService interface {
		IsProxy() bool
		Endpoints() map[string]map[string]http.HandlerFunc
	}

	WebErrorConverter interface {
		ErrorToJSON(err error) ([]byte, error)
	}

	WebServer interface {
	}

	AuthenticationWebServiceClient interface {
		Token(ctx context.Context, username, password string) (string, error)
		Ping(ctx context.Context, token string) error

	}

	DataWebServiceClient interface {
		UploadFile(ctx context.Context, user User, path string, r io.ReadCloser, clientChecksum string) error
		DownloadFile(ctx context.Context, user User, path string) (io.ReadCloser, error)
	}

	MetaDataWebServiceClient interface {
		Examine(ctx context.Context, user User, path string) (FileInfo, error)
		Move(ctx context.Context, user User, sourcePath, targetPath string) error
		Delete(ctx context.Context, user User, path string) error
		ListFolder(ctx context.Context, user User, path string) ([]FileInfo, error)
		CreateFolder(ctx context.Context, user User, path string) error
	}

	MimeGuesser interface {
		FromString(fileName string) string
		FromFileInfo(fileInfo FileInfo) string
	}

	Configuration interface {
		GetPort() int
		GetCPU() string
		GetEnabledWebServices() string

		GetAppLoggerOut() string
		GetAppLoggerMaxSize() int
		GetAppLoggerMaxAge() int
		GetAppLoggerMaxBackups() int

		GetHTTPAccessLoggerOut() string
		GetHTTPAccessLoggerMaxSize() int
		GetHTTPAccessLoggerMaxAge() int
		GetHTTPAccessLoggerMaxBackups() int

		IsTLSEnabled() bool
		GetTLSCertificate() string
		GetTLSPrivateKey() string

		GetUserDriver() string
		GetMemUserDriverUsers() string
		GetLDAPUserDriverBindUsername() string
		GetLDAPUserDriverBindPassword() string
		GetLDAPUserDriverHostname() string
		GetLDAPUserDriverPort() int
		GetLDAPUserDriverBaseDN() string
		GetLDAPUserDriverFilter() string

		GetDataDriver() string
		GetFSDataDriverDataFolder() string
		GetFSDataDriverTemporaryFolder() string
		GetFSDataDriverChecksum() string
		GetFSDataDriverVerifyClientChecksum() bool
		GetOCFSDataDriverDataFolder() string
		GetOCFSDataDriverTemporaryFolder() string
		GetOCFSDataDriverChunksFolder() string
		GetOCFSDataDriverChecksum() string
		GetOCFSDataDriverVerifyClientChecksum() bool

		GetMetaDataDriver() string
		GetFSMDataDriverDataFolder() string
		GetFSMDataDriverTemporaryFolder() string
		GetOCFSMDataDriverDataFolder() string
		GetOCFSMDataDriverTemporaryFolder() string
		GetOCFSMDataDriverMaxSQLIddle() int
		GetOCFSMDataDriverMaxSQLConcurrent() int
		GetOCFSMDataDriverDSN() string

		GetTokenDriver() string
		GetJWTTokenDriverKey() string

		GetRegistryDriver() string
		GetETCDRegistryDriverUrls() string
		GetETCDRegistryDriverUsername() string
		GetETCDRegistryDriverPassword() string
		GetETCDRegistryDriverKey() string

		GetBasicAuthMiddleware() string
		GetBasicAuthMiddlewareCookieName() string

		IsCORSMiddlewareEnabled() bool
		GetCORSMiddlewareAccessControlAllowOrigin() string
		GetCORSMiddlewareAccessControlAllowMethods() string
		GetCORSMiddlewareAccessControlAllowHeaders() string

		GetAuthenticationWebService() string
		GetAuthenticationWebServiceMethodAgnostic() bool

		GetDataWebService() string
		GetDataWebServiceMaxUploadFileSize() int64

		GetMetaDataWebService() string

		GetOCWebService() string
		GetOCWebServiceMaxUploadFileSize() int64
		GetRemoteOCWebServiceMaxUploadFileSize() int64
	}

	ConfigurationSource interface {
		LoadConfiguration() (Configuration, error)
	}
)
