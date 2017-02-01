package ocfsmdatadriver

import (
	"os"
	"path/filepath"
	"strings"

	"context"
	"github.com/clawio/lib"
	"github.com/go-kit/kit/log/levels"
	"github.com/go-sql-Driver/mysql"
	"github.com/jinzhu/gorm"
	"github.com/satori/go.uuid"
	"time"
)

// extra is the extra information that goes inside the ObjectInfo's extra field.
type extra struct {
	ID   string `json:"id"`
	ETag string `json:"etag"`
}

// record represents the metadata stores on a SQL database.
type record struct {
	// ID is the unique identifier for a resource.
	// ownCloud uses this ID to track remote moves in the
	// sync clients in order to avoid a delete+download operation
	ID string

	// VirtualPath is the logical path for an user.
	// The difference between a path is that a path is always a relative
	// path to the user, meanwhile a virtualPath always contains the user in its path,
	// thus creating a global namespace for each user.
	// Ex: a path for user demo will be "photos/jamaica.png"
	// but its virtualPath will be "/d/demo/photos/jamaica.png"
	VirtualPath string `sql:"unique_index:idx_virtualpath" gorm:"column:virtualpath"`

	// Checksum is the checksum for the blob in type:sum format
	// Simple implementations of the metadata controller just compute
	// the checksum when the blob is uploaded to the server. On the other
	// hand this controllers stores the checksum in the database so
	// it is exposed when downloading the blob.
	Checksum string

	// ETag is most important and sensitive part of the ownCloud
	// synchronisation protocol. An ETag is an unique identifier that
	// is assigned to each resource every time a resource changes or its children.
	// ownCloud relies on ETags propagation to obtain a tree of changes on the
	// sync client.
	ETag string `gorm:"column:etag"`

	// ModTime is the modification time of the resource. It is also propagated like the
	// ETag as it is needed in some corner cases when the sync client losses its sync db and it
	// has to fallback on local mtimes. It also helps to perform the CAS operation on each node as it is easy
	// to check if a node has been updated comparing its modtime. ETags cannot be used in CAS because they do not
	// tell when the resource was modified, just that is has been modified.
	ModTime int64 `gorm:"column:modtime"`
}

// TableName returns the name of the SQL table.
func (r *record) TableName() string { return "records" }

// Driver implements the MetaDataDriver interface.
type Driver struct {
	logger                      levels.Levels
	sqlLogger                   mysql.Logger
	dataFolder                  string
	temporaryFolder             string
	maxSQLIdleConnections       int
	maxSQLConcurrentConnections int
	db                          *gorm.DB
}

// New returns an implementation of MetaDataDriver
func New(logger levels.Levels, sqlLogger mysql.Logger, maxSQLIdleConnections, maxSQLConcurrentConnections int, dataFolder, temporaryFolder, dsn string) (lib.MetaDataDriver, error) {
	if sqlLogger == nil {
		sqlLogger = &gorm.Logger{}
	}

	c := &Driver{
		logger:          logger,
		dataFolder:      dataFolder,
		temporaryFolder: temporaryFolder,
		sqlLogger:       sqlLogger,
	}

	if err := os.MkdirAll(dataFolder, 0755); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(temporaryFolder, 0755); err != nil {
		return nil, err
	}

	db, err := gorm.Open("mysql", dsn)
	if err != nil {
		logger.Error().Log("error", err)
		return nil, err
	}


	logger.Info().Log("maxidle", maxSQLIdleConnections, "maxopen", maxSQLConcurrentConnections)
	//db.SetLogger(sqlLogger)
	db.LogMode(false)
	db.DB().SetMaxIdleConns(maxSQLIdleConnections)
	db.DB().SetMaxOpenConns(maxSQLConcurrentConnections)

	err = db.AutoMigrate(&record{}).Error
	if err != nil {
		return nil, err
	}

	c.db = db
	return c, nil
}

// Init initializes the user home directory.
func (c *Driver) Init(ctx context.Context, user lib.User) error {
	localPath := c.getLocalPath(user, "/")
	if err := os.MkdirAll(localPath, 0755); err != nil {
		c.logger.Error().Log("error", err)
		return err
	}

	_, err := c.GetDBMetaData(c.GetVirtualPath(user, "/"), true, c.GetVirtualPath(user, "/"))
	if err != nil {
		return err
	}

	return nil
}

// CreateTree creates a new tree.
func (c *Driver) CreateFolder(ctx context.Context, user lib.User, path string) error {
	localPath := c.getLocalPath(user, path)
	if err := os.Mkdir(localPath, 0755); err != nil {
		c.logger.Error().Log("error", err)
		return err
	}
	return c.SetDBMetaData(c.GetVirtualPath(user, path), "", c.GetVirtualPath(user, "/"))
}

// ExamineObject returns the metadata associated with the object.
func (c *Driver) Examine(ctx context.Context, user lib.User, path string) (lib.FileInfo, error) {
	localPath := c.getLocalPath(user, path)
	osFileInfo, err := os.Stat(localPath)
	if err != nil {
		c.logger.Error().Log("error", err)
		if os.IsNotExist(err) {
			return nil, notFoundError(err.Error())
		}
		return nil, err
	}

	rec, err := c.GetDBMetaData(c.GetVirtualPath(user, path), true, c.GetVirtualPath(user, "/"))
	if err != nil {
		return nil, err
	}

	fileInfo := c.getObjectInfo(path, osFileInfo, rec)
	return fileInfo, nil
}

func (c *Driver) ListFolder(ctx context.Context, user lib.User, path string) ([]lib.FileInfo, error) {
	localPath := c.getLocalPath(user, path)
	osFileInfo, err := os.Stat(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, notFoundError(err.Error())
		}
		return nil, err
	}
	if !osFileInfo.IsDir() {
		return nil, isFolderError("file is not a folder")
	}
	fd, err := os.Open(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, notFoundError(err.Error())
		}
		return nil, err
	}
	defer fd.Close()
	osFileInfos, err := fd.Readdir(-1) // read all files inside the directory.
	if err != nil {
		return nil, err
	}
	var fileInfos []lib.FileInfo
	for _, fi := range osFileInfos {
		p := filepath.Join(path, filepath.Base(fi.Name()))
		rec, err := c.GetDBMetaData(c.GetVirtualPath(user, p), true, c.GetVirtualPath(user, "/"))
		if err != nil {
			return nil, err
		}
		fileInfos = append(fileInfos, c.getObjectInfo(p, fi, rec))
	}
	return fileInfos, nil
}

// DeleteObject deletes an object.
func (c *Driver) Delete(ctx context.Context, user lib.User, path string) error {
	localPath := c.getLocalPath(user, path)
	err := os.RemoveAll(localPath)
	if err != nil {
		return err
	}

	return c.removeInDB(c.GetVirtualPath(user, path), c.GetVirtualPath(user, "/"))
}

// Move moves an object from source to target.
func (c *Driver) Move(ctx context.Context, user lib.User, sourcePath, targetPath string) error {
	sourceLocalPath := c.getLocalPath(user, sourcePath)
	targetLocalPath := c.getLocalPath(user, targetPath)
	err := os.Rename(sourceLocalPath, targetLocalPath)
	if err != nil {
		if os.IsNotExist(err) {
			return notFoundError(err.Error())
		} else if _, ok := err.(*os.LinkError); ok {
			return renameError(err.Error())
		}
		return err
	}

	sourceVirtualPath := c.GetVirtualPath(user, sourcePath)
	targetVirtualPath := c.GetVirtualPath(user, targetPath)
	return c.MoveDBMetaData(sourceVirtualPath, targetVirtualPath, c.GetVirtualPath(user, "/"))
}

func (c *Driver) getLocalPath(user lib.User, path string) string {
	homeDir := secureJoin("/", user.Username())
	userPath := secureJoin(homeDir, path)
	return secureJoin(c.dataFolder, userPath)
}

// GetVirtualPath returns the virtual path inside the database for this user and path.
func (c *Driver) GetVirtualPath(user lib.User, path string) string {
	homeDir := secureJoin("/", string(user.Username()[0]), user.Username())
	return secureJoin(homeDir, path)
}
func (c *Driver) getObjectInfo(path string, osFileInfo os.FileInfo, rec *record) lib.FileInfo {
	return &fileInfo{path: path, osFileInfo: osFileInfo, checksum: rec.Checksum, etag: rec.ETag, id: rec.ID, mtime: rec.ModTime}
}

// secureJoin avoids path traversal attacks when joinning paths.
func secureJoin(args ...string) string {
	if len(args) > 1 {
		s := []string{"/"}
		s = append(s, args[1:]...)
		jailedPath := filepath.Join(s...)
		return filepath.Join(args[0], jailedPath)
	}
	return filepath.Join(args...)
}

func (c *Driver) getByVirtualPath(virtualPath string) (*record, error) {
	r := &record{}
	err := c.db.Where("virtualpath=?", virtualPath).First(r).Error
	return r, err
}

// GetDBMetaData returns the metadata kept in the database for this virtualPath.
func (c *Driver) GetDBMetaData(virtualPath string, forceCreateOnMiss bool, ancestorVirtualPath string) (*record, error) {
	r, err := c.getByVirtualPath(virtualPath)
	if err != nil {
		if err != gorm.ErrRecordNotFound {
			return nil, err
		}
		if !forceCreateOnMiss {
			return nil, err
		}
		err = c.SetDBMetaData(virtualPath, "", ancestorVirtualPath)
		if err != nil {
			return nil, err
		}
		// try after creation has been successful.
		// It can fail if a concurrent request resolves before, but it is safe.
		r, err = c.getByVirtualPath(virtualPath)
		if err != nil {
			return nil, err
		}
	}

	// at this point a valid record has been obtained, either by a hit on the db or by put-and-get
	return r, nil
}

func (c *Driver) PropagateChanges(user lib.User, from, to, checksum string) error {
	vp := c.GetVirtualPath(user, from)
	ancestor := c.GetVirtualPath(user, to)
	return c.SetDBMetaData(vp, checksum, ancestor)
}

// SetDBMetaData sets the metatadata for this virtualPath.
func (c *Driver) SetDBMetaData(virtualPath, checksum string, ancestorVirtualPath string) error {
	etag := uuid.NewV4().String()
	modTime := time.Now().UnixNano()
	id := etag

	// if the record already exists, we need to use its ID instead
	// creating a new one
	r, err := c.getByVirtualPath(virtualPath)
	if err == nil {
		c.logger.Debug().Log("record", *r, "msg", "id set to record.ID")
		id = r.ID
	}

	err = c.insertOrUpdateIntoDB(id, virtualPath, checksum, etag, modTime)
	if err != nil {
		c.logger.Error().Log("error", err, "error inserting record")
		return err
	}

	err = c.propagateChangesInDB(virtualPath, etag, modTime, ancestorVirtualPath)
	if err != nil {
		c.logger.Error().Log("error", err, "msg", "error propagating changes")
		// we do not return an error here as it is quite
		// common to abort the propagation of changes
		// when other concurrent request has already
		// updated them
	} else {
		c.logger.Debug().Log("child", virtualPath, "ancestor", ancestorVirtualPath, "msg", "changes propagated from child to ancestor")
	}

	return nil
}

// MoveDBMetaData moves metadata from one virtualPath to another.
func (c *Driver) MoveDBMetaData(sourceVirtualPath, targetVirtualPath, ancestorVirtualPath string) error {
	records, err := c.getChildrenrecords(sourceVirtualPath)
	if err != nil {
		c.logger.Error().Log("error", err, "msg", "error getting children for move")
		return err
	}

	tx := c.db.Begin()
	for _, rec := range records {
		newVirtualPath := secureJoin(targetVirtualPath, strings.TrimPrefix(rec.VirtualPath, sourceVirtualPath))
		c.logger.Debug().Log("sourcevirtualpath", rec.VirtualPath, "targetvirtualpath", newVirtualPath, "msg", "record to be moved")
		if err := c.db.Model(&record{}).Where("id=?", rec.ID).Updates(&record{VirtualPath: newVirtualPath}).Error; err != nil {
			c.logger.Error().Log("error", err, "msg", "error updating virtualpath")
			if err := tx.Rollback().Error; err != nil {
				c.logger.Crit().Log("error", err, "msg", "error rollbacking operation")
				return err
			}
			return err
		}
	}
	tx.Commit()

	etag := uuid.NewV4().String()
	modTime := time.Now().UnixNano()

	err = c.propagateChangesInDB(targetVirtualPath, etag, modTime, ancestorVirtualPath)
	if err != nil {
		c.logger.Error().Log("error", err, "error propagating changes")
		// we do not return an error here as it is quite
		// common to abort the propagation of changes
		// when other concurrent request has already
		// updated them
	} else {
		c.logger.Debug().Log("child", targetVirtualPath, "ancestor", ancestorVirtualPath, "msg", "changes propagated")
	}
	return nil
}

func (c *Driver) getChildrenrecords(virtualPath string) ([]record, error) {
	var records []record

	err := c.db.Where("virtualpath LIKE ? or virtualpath=?", virtualPath+"/%", virtualPath).Find(&records).Error
	return records, err
}

// propagateChangesInDB propagates mtime and etag values until
// ancestor (included). This propagation is needed for the ownCloud/nextCloud sync client
// to discover changes.
// Ex: given the successful upload of the file /d/demo/photos/1.png
// the etag and mtime values will be updated also at:
// 1st) /d/demo/photos
// 2nd) /d/demo
func (c *Driver) propagateChangesInDB(virtualPath, etag string, modTime int64, ancestor string) error {
	c.logger.Debug().Log("virtualpath", virtualPath, "etag", etag, "mtime", modTime, "record that triggered propagation")
	// virtualPathsToUpdate are sorted from largest to shortest virtual paths.
	// Ex: "/d/demo/photos" comes before "/d/demo/"

	virtualPathsToUpdate := c.getVirtualPathsUntilAncestor(virtualPath, ancestor)
	c.logger.Debug().Log("virtualpaths2update", virtualPathsToUpdate, "msg", "virtual paths to update")

	for _, vp := range virtualPathsToUpdate {
		affectedRows := c.updateInDB(vp, etag, modTime)
		if affectedRows == 0 {
			// when affectedRows == 0 it can mean two things:
			// 1st) the record will not be updated because it does not satisfy the mtime < x condition
			// 2nd) the record does not exist
			// To handle the 2nd scenario we insert the record manually in the db.
			parentID := uuid.NewV4().String()
			err := c.insertIntoDB(parentID, vp, "", etag, modTime)
			if err == nil {
				// record has been inserted to match child etag and mtime
				// so we can continue to propagate more ancestors
				continue
			}

			// the record may have been created in the mean time
			// and it could have failed because of a duplicate primary key error
			// or we may be in the 1st scenario,
			// either way, we need to abort the propagation.
			// we stop to process upper virtual paths because if the current virtual path has been already updated
			// implies that its ancestor has been also updated.
			// This is an optimisation on the ownCloud sync protocol, we use atomic CAS (compare-and-swap) on each
			// node and we only move upper in the three if the current node has not been already updated.
			c.logger.Error().Log("error", err, "msg", "virtual path already updated or failed to be inserted")
			return err
		}
	}
	return nil
}

func (c *Driver) getVirtualPathsUntilAncestor(virtualPath, ancestor string) []string {
	// virtualPaths is sorted from shortest to largest for easier implementation
	// this slice is sorted at the end viceverse
	var virtualPaths []string
	if !strings.HasPrefix(virtualPath, ancestor) {
		// if ancestor is not part of virtualPath
		// it does not make sense to update
		return virtualPaths
	}

	// remove ancestor from virtualPath
	virtualPath = virtualPath[len(ancestor):]
	tokens := strings.Split(virtualPath, "/")

	previous := ancestor
	virtualPaths = append(virtualPaths, previous) // add ancestor to update

	for _, token := range tokens {
		if token != "" {
			previous = secureJoin(previous, token)
			virtualPaths = append(virtualPaths, previous)
		}
	}

	// the last path is the one that triggered the propagation, so
	// we remove to avoid updating it after being inserted/updated
	if len(virtualPaths) >= 1 {
		virtualPaths = virtualPaths[:len(virtualPaths)-1]
	}

	// sort from largest to shortest
	for i := len(virtualPaths)/2 - 1; i >= 0; i-- {
		opp := len(virtualPaths) - 1 - i
		virtualPaths[i], virtualPaths[opp] = virtualPaths[opp], virtualPaths[i]
	}

	return virtualPaths
}

func (c *Driver) insertOrUpdateIntoDB(id, virtualPath, checksum, etag string, modTime int64) error {
	c.logger.Debug().Log("msg", "record to be inserted", "id", id, "virtualpath", virtualPath, "etag", etag, "mtime", modTime, "checksum", checksum)
	// this query only works on MySQL/MariaDB databases as it uses ON DUPLICATE KEY UPDATE feature
	// to implement an atomic operation, either an insert or an update.
	err := c.db.Exec(`INSERT INTO records (id,virtualpath,checksum, etag, modtime) VALUES (?,?,?,?,?)
	ON DUPLICATE KEY UPDATE checksum=VALUES(checksum), etag=VALUES(etag), modtime=VALUES(modtime)`,
		id, virtualPath, checksum, etag, modTime).Error
	return err
}

func (c *Driver) updateInDB(virtualPath, etag string, modTime int64) int64 {
	c.logger.Debug().Log("msg", "record to be updated", "virtualpath", virtualPath, "etag", etag, "mtime", modTime)
	return c.db.Model(&record{}).Where("virtualpath=? AND modtime < ?", virtualPath, modTime).Updates(&record{ETag: etag, ModTime: modTime}).RowsAffected
}

func (c *Driver) insertIntoDB(id, virtualPath, checksum, etag string, modTime int64) error {
	c.logger.Debug().Log("msg", "record to be inserted", "virtualpath", virtualPath, "etag", etag, "mtime", modTime)
	err := c.db.Exec(`INSERT INTO records (id,virtualpath,checksum, etag, modtime) VALUES (?,?,?,?,?)`,
		id, virtualPath, checksum, etag, modTime).Error
	return err
}

func (c *Driver) removeInDB(virtualPath, ancestorVirtualPath string) error {
	c.logger.Debug().Log("msg", "record to be removed", "virtualpath", virtualPath)
	removeBeforeTS := time.Now().UnixNano()
	err := c.db.Where("(virtualpath LIKE ? OR virtualpath=? ) AND modtime < ?", virtualPath+"/%", virtualPath, removeBeforeTS).Delete(&record{}).Error
	if err != nil {
		return err
	}

	// after deleting a resource we need to propagate changes up in the tree
	etag := uuid.NewV4().String()
	err = c.propagateChangesInDB(virtualPath, etag, removeBeforeTS, ancestorVirtualPath)
	if err != nil {
		c.logger.Error().Log("error", err, "msg", "error propagating changes")
		// we do not return an error here as it is quite
		// common to abort the propagation of changes
		// when other concurrent request has already
		// updated them
	} else {
		c.logger.Debug().Log("msg", "changes propagated", "virtualpath", virtualPath, "ancestor", ancestorVirtualPath)
	}
	return nil
}

type fileInfo struct {
	path       string
	osFileInfo os.FileInfo
	checksum   string
	etag       string
	id         string
	mtime      int64
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
	return f.mtime
}

func (f *fileInfo) Checksum() string {
	return f.checksum
}

func (f *fileInfo) ExtraAttributes() map[string]interface{} {
	return map[string]interface{}{
		"id":   f.id,
		"etag": f.etag,
	}
}

type checksumError string

func (e checksumError) Error() string {
	return string(e)
}

func (e checksumError) Code() lib.Code {
	return lib.Code(lib.CodeBadChecksum)
}
func (e checksumError) Message() string {
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

type isFolderError string

func (e isFolderError) Error() string {
	return string(e)
}
func (e isFolderError) Code() lib.Code {
	return lib.Code(lib.CodeBadInputData)
}
func (e isFolderError) Message() string {
	return string(e)
}

type renameError string

func (e renameError) Error() string {
	return string(e)
}
func (e renameError) Code() lib.Code {
	return lib.Code(lib.CodeBadInputData)
}
func (e renameError) Message() string {
	return string(e)
}
