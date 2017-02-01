package fsmdatadriver

import (
	"os"
	"path/filepath"

	"context"
	"fmt"
	"github.com/clawio/clawiod/root"
	"github.com/go-kit/kit/log/levels"
	"strings"
)

type driver struct {
	logger          levels.Levels
	dataFolder      string
	temporaryFolder string
}

// New returns an implementation of MetaDataController.
func New(logger levels.Levels, dataFolder, temporaryFolder string) (root.MetaDataDriver, error) {
	logger = logger.With("pkg", "fdmdatadriver")
	c := &driver{
		logger:          logger,
		dataFolder:      dataFolder,
		temporaryFolder: temporaryFolder,
	}

	if err := os.MkdirAll(dataFolder, 0755); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(temporaryFolder, 0755); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *driver) Init(ctx context.Context, user root.User) error {
	localPath := c.getLocalPath(user, "/")
	if err := os.MkdirAll(localPath, 0755); err != nil {
		return err
	}
	return nil
}

func (c *driver) CreateFolder(ctx context.Context, user root.User, path string) error {
	localPath := c.getLocalPath(user, path)
	if err := os.Mkdir(localPath, 0755); err != nil {
		c.logger.Error().Log("error", err)
		if os.IsExist(err) {
			return alreadyExistError("folder already exist")
		}
		return err
	}
	c.logger.Info().Log("msg", "folder created", "folder", localPath)
	return nil
}

func (c *driver) Examine(ctx context.Context, user root.User, path string) (root.FileInfo, error) {
	localPath := c.getLocalPath(user, path)
	fsFileInfo, err := os.Stat(localPath)
	if err != nil {
		c.logger.Error().Log("error", err)
		if os.IsNotExist(err) {
			return nil, notFoundError(err.Error())
		}
		return nil, err
	}
	c.logger.Info().Log("msg", "file examined", "file", localPath)
	fileInfo := c.convert(path, fsFileInfo)
	return fileInfo, nil
}

func (c *driver) ListFolder(ctx context.Context, user root.User, path string) ([]root.FileInfo, error) {
	localPath := c.getLocalPath(user, path)
	fsFileInfo, err := os.Stat(localPath)
	if err != nil {
		c.logger.Error().Log("error", err)
		if os.IsNotExist(err) {
			return nil, notFoundError(err.Error())
		}
		return nil, err
	}

	c.logger.Info().Log("msg", "file examined", "file", localPath)
	if !fsFileInfo.IsDir() {
		return nil, isFolderError(fmt.Sprintf("%q is not a folder", localPath))
	}

	fd, err := os.Open(localPath)
	if err != nil {
		c.logger.Error().Log("error", err)
		if os.IsNotExist(err) {
			return nil, notFoundError(err.Error())
		}
		return nil, err
	}
	defer fd.Close()

	c.logger.Info().Log("msg", "folder opened", "folder", localPath)
	fsFileInfos, err := fd.Readdir(-1) // read all files inside the directory.
	if err != nil {
		c.logger.Error().Log("error", err)
		return nil, err
	}
	c.logger.Info().Log("msg", "folder readed", "numfiles", len(fsFileInfos))
	var fileInfos []root.FileInfo
	for _, fi := range fsFileInfos {
		nodePath := filepath.Join(path, filepath.Base(fi.Name()))
		fileInfos = append(fileInfos, c.convert(nodePath, fi))
	}
	return fileInfos, nil
}

func (c *driver) Delete(ctx context.Context, user root.User, path string) error {
	localPath := c.getLocalPath(user, path)
	err := os.RemoveAll(localPath)
	if err != nil {
		c.logger.Error().Log("error", err)
	}
	c.logger.Info().Log("msg", "file deleted", "file", localPath)
	return nil
}

func (c *driver) Move(ctx context.Context, user root.User, sourcePath, targetPath string) error {
	sourceLocalPath := c.getLocalPath(user, sourcePath)
	targetLocalPath := c.getLocalPath(user, targetPath)
	err := os.Rename(sourceLocalPath, targetLocalPath)
	if err != nil {
		c.logger.Error().Log("error", err)
		if os.IsNotExist(err) {
			return notFoundError(err.Error())
		} else if _, ok := err.(*os.LinkError); ok {
			return renameError(err.Error())
		}
		return err
	}
	c.logger.Info().Log("msg", "file renamed", "source", sourceLocalPath, "target", targetLocalPath)
	return nil
}
func (c *driver) getLocalPath(user root.User, path string) string {
	dataFolder := strings.Trim(c.dataFolder, "/")
	path = strings.Trim(path, "/")
	return fmt.Sprintf("/%s/%s/%s", dataFolder, user.Username(), filepath.Clean(path))
}

func (c *driver) convert(path string, fsFileInfo os.FileInfo) root.FileInfo {
	return &fileInfo{path: path, osFileInfo: fsFileInfo}
}

type fileInfo struct {
	path       string
	osFileInfo os.FileInfo
}

func (f *fileInfo) Path() string {
	return f.path
}

func (f *fileInfo) Folder() bool {
	return f.osFileInfo.IsDir()
}

func (f *fileInfo) Size() int64 {
	return int64(f.osFileInfo.Size())
}

func (f *fileInfo) Modified() int64 {
	return f.osFileInfo.ModTime().UnixNano()
}

func (f *fileInfo) Checksum() string {
	return ""
}

func (f *fileInfo) ExtraAttributes() map[string]interface{} {
	return nil
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

type alreadyExistError string

func (e alreadyExistError) Error() string {
	return string(e)
}
func (e alreadyExistError) Code() root.Code {
	return root.Code(root.CodeAlreadyExist)
}
func (e alreadyExistError) Message() string {
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

type renameError string

func (e renameError) Error() string {
	return string(e)
}
func (e renameError) Code() root.Code {
	return root.Code(root.CodeBadInputData)
}
func (e renameError) Message() string {
	return string(e)
}
