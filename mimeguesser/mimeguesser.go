package mimeguesser

import (
	"github.com/clawio/clawiod/root"
	"mime"
	"path/filepath"
)

const folderMime = "clawio/folder"

type guesser struct{}

func New() root.MimeGuesser {
	return &guesser{}
}

func (m *guesser) FromString(name string) string {
	return mime.TypeByExtension(filepath.Base(name))
}

func (m *guesser) FromFileInfo(fileInfo root.FileInfo) string {
	if fileInfo.Folder() {
		return folderMime
	}
	return m.FromString(fileInfo.Path())
}
