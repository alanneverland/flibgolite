package hash

import (
	"log"
	"sync"

	"github.com/jmoiron/sqlx"
)

type BookState int64

const (
	Unique BookState = -1 * iota
	FileIsEmpty
	FileHasErrors
	LanguageNotAccepted
	GenreNotAccepted
	BadArchive
	UnsupportedFormat
	FileOpenFailed
	ArchiveContainer
	UnchangedFile
	UnchangedArchive
)

type BookHashes struct {
	Archives  map[string]map[string]int64
	Files     map[string]int64
	mx        sync.RWMutex
}

func InitHashes(db *sqlx.DB) *BookHashes {
	bh := &BookHashes{}
	bh.Reload(db)
	return bh
}

func (bh *BookHashes) Reload(db *sqlx.DB) {
	bh.mx.Lock()
	defer bh.mx.Unlock()

	bh.Archives = make(map[string]map[string]int64)
	bh.Files = make(map[string]int64)

	rows, err := db.Query(`SELECT file, archive, size FROM books`)
	if err != nil {
		log.Println("Reload cache error:", err)
		return
	}
	defer rows.Close()

	var file, archive string
	var size int64
	
	for rows.Next() {
		err := rows.Scan(&file, &archive, &size)
		if err != nil {
			continue
		}

		if archive == "" {
			bh.Files[file] = size
		} else {
			archMap, ok := bh.Archives[archive]
			if !ok {
				archMap = make(map[string]int64)
				bh.Archives[archive] = archMap
			}
			if file != "" {
				archMap[file] = size
			}
		}
	}
}

func (bh *BookHashes) Clear() {
	bh.mx.Lock()
	defer bh.mx.Unlock()

	bh.Archives = make(map[string]map[string]int64)
	bh.Files = make(map[string]int64)
}

func (bh *BookHashes) Add(file, archive string, size int64) {
	bh.mx.Lock()
	defer bh.mx.Unlock()
		
	if archive == "" {
		bh.Files[file] = size
	} else {
		if _, ok := bh.Archives[archive]; !ok {
			bh.Archives[archive] = make(map[string]int64)
		}
		if file != "" {
			bh.Archives[archive][file] = size
		}
	}
}

func (bh *BookHashes) IsUnchanged(file, archive string, size int64) bool {
	bh.mx.RLock()
	defer bh.mx.RUnlock()
	
	if archive == "" {
		if cachedSize, ok := bh.Files[file]; ok {
			return cachedSize == size
		}
		return false
	}
	
	if _, ok := bh.Archives[archive]; ok {
		if cachedSize, ok := bh.Archives[archive][file]; ok {
			return cachedSize == size
		}
	}
	
	return false
}


