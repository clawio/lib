package ocfsdatadriver

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"hash/adler32"
	"io"
	"io/ioutil"
	"os"
	"strings"

	"github.com/clawio/clawiod/root"
	"github.com/clawio/clawiod/root/ocfsmdatadriver"
	"github.com/go-kit/kit/log/levels"
	"path/filepath"
	"regexp"
	"strconv"
)

type driver struct {
	logger                 levels.Levels
	dataFolder             string
	temporaryFolder        string
	chunksFolder           string
	checksum               string
	verifyClientChecksum   bool
	metaDataDriver         root.MetaDataDriver
	ownCloudMetaDataDriver *ocfsmdatadriver.Driver
}

// New returns an implementation of DataDriver.
func New(logger levels.Levels, dataFolder, temporaryFolder, chunksFolder, checksum string, verifyClientChecksum bool, metaDataDriver root.MetaDataDriver) (root.DataDriver, error) {
	if err := os.MkdirAll(dataFolder, 755); err != nil {
		return nil, err
	}
	if temporaryFolder == "" {
		temporaryFolder = os.TempDir()
	}

	if chunksFolder == "" {
		chunksFolder = filepath.Join(temporaryFolder, "/chunks")
	}

	logger.Info().Log("msg", "folders are the following", "datafolder", dataFolder, "temporaryfolder", temporaryFolder, "chunksfolder", chunksFolder)
	if err := os.MkdirAll(temporaryFolder, 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(chunksFolder, 0755); err != nil {
		return nil, err
	}

	// check that metadata driver is compatible with owncloud.
	// This is an ugly check but is the one needed to keep the metadata driver interface clean
	// We cast to all compatible implementations.
	ownCloudMetaDataDriver, ok := metaDataDriver.(*ocfsmdatadriver.Driver)
	if !ok {
		logger.Crit().Log("error", "metadata driver is not ocfsmdatadriver")
		return nil, errors.New("metadata driver is not ocfsmdatadriver")
	}

	return &driver{
		logger:                 logger,
		dataFolder:             strings.Trim(dataFolder, "/"),
		temporaryFolder:        strings.Trim(temporaryFolder, "/"),
		chunksFolder:           strings.Trim(chunksFolder, "/"),
		checksum:               checksum,
		verifyClientChecksum:   verifyClientChecksum,
		metaDataDriver:         metaDataDriver,
		ownCloudMetaDataDriver: ownCloudMetaDataDriver,
	}, nil
}

func (c *driver) Init(ctx context.Context, user root.User) error {
	return nil
}

// UploadFile saves a file to disk.
// This operation has 4 phases:
// 1) Write the file to a temporary folder.
// 2) Optional: calculate the checksum of the file if server-checksum is enabled.
// 3) Optional: if a client-checksum is provided, check if it matches with the server-checksum.
// 4) Move the file from the temporary folder to user folder.
func (c *driver) UploadFile(ctx context.Context, user root.User, path string, r io.ReadCloser, clientChecksum string) error {
	defer r.Close()
	// if the file is a chunk we handle it differently
	isChunked, err := c.isChunkedUpload(path)
	if err != nil {
		c.logger.Error().Log("error", err)
		return err
	}

	if isChunked {
		c.logger.Info().Log("msg", "upload is chunk upload")
		return c.uploadChunk(ctx, user, path, r, clientChecksum)
	}

	tempFileName, err := c.saveToTempFile(r)
	if err != nil {
		c.logger.Error().Log("error", err)
		return err
	}

	var computedChecksum string
	// 2) Optional: calculate the checksum of the file.
	if c.checksum != "" {
		computedChecksum, err = c.computeChecksum(tempFileName)
		if err != nil {
			c.logger.Error().Log("error", err)
			return err
		}

		c.logger.Info().Log("msg", "checksum computed", "checksum", computedChecksum, "file", tempFileName)

		// 3) Optional: verify if server-checksum matches client-checksum.
		if c.verifyClientChecksum {
			if computedChecksum != clientChecksum {
				msg := fmt.Sprintf("fsdatadriver: wrong checksum computed:%q received:%q",
					computedChecksum, clientChecksum)
				return checksumError(msg)
			}
		}
	}

	// 4) Move the file from the temporary folder to user folder.
	localPath := c.getLocalPath(user, path)
	if err := os.Rename(tempFileName, localPath); err != nil {
		c.logger.Error().Log("error", err)
		if os.IsNotExist(err) {
			return notFoundError(err.Error())
		}
		return err
	}
	c.logger.Info().Log("msg", "atomic rename completed", "source", tempFileName, "target", localPath)
	if err = c.ownCloudMetaDataDriver.PropagateChanges(user, path, "/", computedChecksum); err != nil {
		c.logger.Error().Log("error", err, "msg", "error propagating changes")
	}
	return nil
}

func (c *driver) DownloadFile(ctx context.Context, user root.User, path string) (io.ReadCloser, error) {
	localPath := c.getLocalPath(user, path)
	fd, err := os.Open(localPath)
	if err != nil {
		c.logger.Error().Log("error", err)
		if os.IsNotExist(err) {
			return nil, notFoundError(err.Error())
		}
		return nil, err
	}
	fileInfo, err := fd.Stat()
	if err != nil {
		c.logger.Error().Log("error", err)
		if os.IsNotExist(err) {
			return nil, notFoundError(err.Error())
		}
		return nil, err
	}

	if fileInfo.IsDir() {
		err := isFolderError("file is a folder")
		c.logger.Error().Log("error", err)
		return nil, err
	}

	c.logger.Info().Log("msg", "file opened for reading", "file", localPath)
	return fd, nil
}

func (c *driver) uploadChunk(ctx context.Context, user root.User, path string, r io.ReadCloser, clientChecksum string) error {
	chunkInfo, err := getChunkBLOBInfo(path)
	if err != nil {
		err := fmt.Errorf("error getting chunk info from path: %s", path)
		c.logger.Error().Log("error", err)
		return err
	}

	c.logger.Info().Log("chunknum", chunkInfo.currentChunk, "chunks", chunkInfo.totalChunks,
		"transferid", chunkInfo.transferID, "uploadid", chunkInfo.uploadID())

	chunkTempFilename, chunkTempFile, err := c.createChunkTempFile()
	if err != nil {
		c.logger.Error().Log("error", err)
		return err
	}
	defer chunkTempFile.Close()

	if _, err := io.Copy(chunkTempFile, r); err != nil {
		c.logger.Error().Log("error", err)
		return err
	}

	// force close of the file here because if it is the last chunk to
	// assemble the big file we must have all the chunks already closed.
	if err = chunkTempFile.Close(); err != nil {
		c.logger.Error().Log("error", err)
		return err
	}

	chunksFolderName, err := c.getChunkFolderName(chunkInfo)
	if err != nil {
		c.logger.Error().Log("error", err)
		return err
	}
	c.logger.Info().Log("chunkfolder", chunksFolderName)

	chunkTarget := chunksFolderName + "/" + fmt.Sprintf("%d", chunkInfo.currentChunk)
	if err = os.Rename(chunkTempFilename, chunkTarget); err != nil {
		c.logger.Error().Log("error", err)
		return err
	}

	c.logger.Info().Log("chunktarget", chunkTarget)

	// Check that all chunks are uploaded.
	// This is very inefficient, the server has to check that it has all the
	// chunks after each uploaded chunk.
	// A two-phase upload like DropBox is better, because the server will
	// assembly the chunks when the client asks for it.
	chunksFolder, err := os.Open(chunksFolderName)
	if err != nil {
		c.logger.Error().Log("error", err)
		return err
	}
	defer chunksFolder.Close()

	// read all the chunks inside the chunk folder; -1 == all
	chunks, err := chunksFolder.Readdir(-1)
	if err != nil {
		c.logger.Error().Log("error", err)
		return err
	}
	c.logger.Info().Log("msg", "chunkfolder readed", "nchunks", len(chunks))

	// there is still some chunks to be uploaded.
	// we return CodeUploadIsPartial to notify uper layers that the upload is still
	// not complete and requires more actions.
	// This code is needed to notify the owncloud webservice that the upload has not yet been
	// completed and needs to continue uploading chunks.
	if len(chunks) < int(chunkInfo.totalChunks) {
		return partialUploadError("current chunk does not complete the file")
	}

	assembledFileName, assembledFile, err := c.createChunkTempFile()
	if err != nil {
		c.logger.Error().Log("error", err)
		return err
	}
	defer assembledFile.Close()

	c.logger.Info().Log("assembledfile", assembledFileName)

	// walk all chunks and append to assembled file
	for i := range chunks {
		target := chunksFolderName + "/" + fmt.Sprintf("%d", i)

		chunk, err := os.Open(target)
		if err != nil {
			c.logger.Error().Log("error", err)
			return err
		}
		defer chunk.Close()

		if _, err = io.Copy(assembledFile, chunk); err != nil {
			c.logger.Error().Log("error", err)
			return err
		}
		c.logger.Debug().Log("msg", "chunk appended to assembledfile")

		// we close the chunk here because if the assembled file contains hundreds of chunks
		// we will end up with hundreds of open file descriptors
		if err = chunk.Close(); err != nil {
			c.logger.Error().Log("error", err)
			return err

		}
	}

	// at this point the assembled file is complete
	// so we free space removing the chunks folder
	defer func() {
		if err = os.RemoveAll(chunksFolderName); err != nil {
			c.logger.Crit().Log("error", err, "msg", "error deleting chunk folder")
		}
	}()

	// when writing to the assembled file the write pointer points to the end of the file
	// so we need to seek it to the beginning
	if _, err = assembledFile.Seek(0, 0); err != nil {
		c.logger.Error().Log("error", err)
		return err
	}

	path = chunkInfo.path
	tempFileName := assembledFileName

	// Start of Copy/Paste from UploadFile to avoid a copy of the file to temporary area twice
	var computedChecksum string
	// 2) Optional: calculate the checksum of the file.
	if c.checksum != "" {
		computedChecksum, err = c.computeChecksum(tempFileName)
		if err != nil {
			c.logger.Error().Log("error", err)
			return err
		}

		c.logger.Info().Log("msg", "checksum computed", "checksum", computedChecksum, "file", tempFileName)

		// 3) Optional: verify if server-checksum matches client-checksum.
		if c.verifyClientChecksum {
			if computedChecksum != clientChecksum {
				msg := fmt.Sprintf("fsdatadriver: wrong checksum computed:%q received:%q",
					computedChecksum, clientChecksum)
				return checksumError(msg)
			}
		}
	}

	// 4) Move the file from the temporary folder to user folder.
	localPath := c.getLocalPath(user, path)
	if err := os.Rename(tempFileName, localPath); err != nil {
		c.logger.Error().Log("error", err)
		if os.IsNotExist(err) {
			return notFoundError(err.Error())
		}
		return err
	}
	c.logger.Info().Log("msg", "atomic rename completed", "source", tempFileName, "target", localPath)
	if err = c.ownCloudMetaDataDriver.PropagateChanges(user, path, "/", computedChecksum); err != nil {
		c.logger.Error().Log("error", err, "msg", "error propagating changes")
	}
	return nil
}

func (c *driver) saveToTempFile(r io.Reader) (string, error) {
	temporaryFolder := fmt.Sprintf("/%s", c.temporaryFolder)
	fd, err := ioutil.TempFile(temporaryFolder, "")
	if err != nil {
		return "", err
	}
	defer fd.Close()

	written, err := io.Copy(fd, r)
	if err != nil {
		return "", err
	}

	c.logger.Error().Log("msg", "file written to temporary file", "wb", written, "file", fd.Name())
	return fd.Name(), nil
}

func (c *driver) computeChecksum(fn string) (string, error) {
	checksumType := strings.ToLower(c.checksum)
	var hash hash.Hash
	switch checksumType {
	case "md5":
		hash = md5.New()
	case "adler32":
		hash = adler32.New()
	case "sha1":
		hash = sha1.New()
	case "sha256":
		hash = sha256.New()
	default:
		return "", errors.New(fmt.Sprintf("fsdatadriver: provided checksum %q not implemented", c.checksum))
	}
	fd, err := os.Open(fn)
	if err != nil {
		return "", err
	}
	defer fd.Close()
	if _, err := io.Copy(hash, fd); err != nil {
		return "", err
	}
	checksum := fmt.Sprintf("%x", hash.Sum([]byte{}))
	return checksumType + ":" + checksum, nil
}

func (c *driver) getLocalPath(user root.User, path string) string {
	path = strings.Trim(path, "/")
	return fmt.Sprintf("/%s/%s/%s", c.dataFolder, user.Username(), path)
}

func (c *driver) isChunkedUpload(path string) (bool, error) {
	return regexp.MatchString(`-chunking-\w+-[0-9]+-[0-9]+$`, path)
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

func (c *driver) createChunkTempFile() (string, *os.File, error) {
	file, err := ioutil.TempFile(fmt.Sprintf("/%s", c.chunksFolder), "")
	if err != nil {
		return "", nil, err
	}

	return file.Name(), file, nil
}

func (c *driver) getChunkFolderName(i *chunkBLOBInfo) (string, error) {
	p := "/" + c.chunksFolder + filepath.Clean("/"+i.uploadID())
	if err := os.MkdirAll(p, 0755); err != nil {
		return "", err
	}
	return p, nil
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
