package stock

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"errors"
	"sync/atomic"
	"time"

	"github.com/vinser/flibgolite/pkg/config"
	"github.com/vinser/flibgolite/pkg/database"
	"github.com/vinser/flibgolite/pkg/epub"
	"github.com/vinser/flibgolite/pkg/fb2"
	"github.com/vinser/flibgolite/pkg/fb3"
	"github.com/vinser/flibgolite/pkg/genres"
	"github.com/vinser/flibgolite/pkg/hash"
	"github.com/vinser/flibgolite/pkg/mobi"
	"github.com/vinser/flibgolite/pkg/model"
	"github.com/vinser/flibgolite/pkg/parser"
	"github.com/vinser/flibgolite/pkg/pdf"
	"github.com/vinser/flibgolite/pkg/rlog"
)

type Handler struct {
	CFG       *config.Config
	Hashes    *hash.BookHashes
	DB        *database.DB
	GT        *genres.GenresTree
	LOG       *rlog.Log
	ScanSema  chan struct{}
	BookQueue chan model.Book
	StopScan  chan struct{}
	StopDB    chan struct{}
	SyncDB    chan chan struct{}
	NeedOpt   int32
}

type File struct {
	Reader  io.ReadCloser
	Name    string
	Archive string
	Size    int64
}

// InitStockFolders()
func (h *Handler) InitStockFolders() {
	if err := os.MkdirAll(h.CFG.Library.STOCK_DIR, 0776); err != nil {
		log.Fatalf("failed to create Library STOCK_DIR directory %s: %s", h.CFG.Library.STOCK_DIR, err)
	}	
}

func (h *Handler) isFileReady(dir string, ent fs.DirEntry) (path string, ext string, err error) {
	info, err := ent.Info()
	if err != nil {
		return "", "", err
	}
	
	if !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("not a regular file")
	}
	
	path = filepath.Join(dir, info.Name())
	ext = strings.ToLower(filepath.Ext(info.Name()))
		
	if info.Size() == 0 {
		return "", "", fmt.Errorf("file %s is empty", path)
	}
	
	time.Sleep(time.Millisecond)

	newInfo, err := os.Stat(path)
	if err != nil {
		return "", "", err
	}
	
	if newInfo.Size() != info.Size() {
		return "", "", fmt.Errorf("file %s is still being written", path)
	}
		
	switch ext {
	case ".zip", ".epub", ".fb3":
		r, err := zip.OpenReader(path)
		if err != nil {
			if errors.Is(err, zip.ErrFormat) || errors.Is(err, io.ErrUnexpectedEOF) {
				return "", "", fmt.Errorf("archive %s is corrupted", path)
			}

			return "", "", fmt.Errorf("archive %s is busy", path)
		}
		r.Close()
	default:
		f, err := os.Open(path)
		if err != nil {
			return "", "", fmt.Errorf("file %s is busy", path)
		}
		f.Close()
	}

	return path, ext, nil	
}

func (h *Handler) ScanDir(dir string) error {
	var globalWG sync.WaitGroup 
	
	h.LOG.I.Printf("scanning folder %s for new books...\n", dir)
	
	h.LOG.I.Println("Loading RAM cache from database...")
	h.Hashes.Reload(h.DB.DB)
	
	if err := h.DB.ResetSeenFlag(); err != nil {
		h.LOG.W.Printf("Failed to reset seen flags: %v", err)
	}

	err := h.realScanDir(dir, &globalWG) 

	globalWG.Wait() 

	for len(h.BookQueue) > 0 {
		time.Sleep(100 * time.Millisecond)
	}

	syncDone := make(chan struct{})
	h.SyncDB <- syncDone
	<-syncDone
	
	deleted, err := h.DB.CleanUpDeletedBooks()
	if err != nil {
		h.LOG.W.Printf("Failed to clean up deleted books: %v", err)
	} else if deleted > 0 {
		h.LOG.S.Printf("Successfully removed %d old or deleted book records (and related data)", deleted)
		atomic.StoreInt32(&h.NeedOpt, 1) 
	}

	if atomic.CompareAndSwapInt32(&h.NeedOpt, 1, 0) {
		h.LOG.I.Println("Start database optimization...")
		
		bunchesMap := make(map[string][]string)		
		
		for _, genre := range h.GT.ListGenres() {
			subgenres := h.GT.ListSubGenres(genre.Value)
			codes := make([]string, len(subgenres))
			for i, sg := range subgenres {
				codes[i] = sg.Value
			}
			bunchesMap[genre.Value] = codes
		}
		
		h.DB.UpdateAllStats(bunchesMap, true)
		
		if err := h.DB.EndAndOptimize(); err != nil {
			h.LOG.S.Printf("Database optimization error: %v\n", err)
		} else {
			h.LOG.S.Println("Database optimization finished successfully")
		}
	}
	
	h.LOG.I.Println("Clearing RAM cache to free memory...")
	h.Hashes.Clear()
	
	h.LOG.I.Printf("end of folder scanning")

	return err
}

func (h *Handler) realScanDir(dir string, wg *sync.WaitGroup) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	
	entries, err := d.ReadDir(-1)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			newDir := filepath.Join(dir, entry.Name())
			h.realScanDir(newDir, wg)
			continue
		}

		ext := strings.ToLower(filepath.Ext(entry.Name()))
		switch ext {
		case ".fb2", ".epub", ".fb3", ".pdf", ".mobi", ".azw", ".azw3", ".prc", ".zip":
		default:
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.Size() == 0 || strings.HasPrefix(entry.Name(), "._") {
			continue 
		}
		
		path := filepath.Join(dir, entry.Name())
		relPath, err := filepath.Rel(h.CFG.Library.STOCK_DIR, path)
		if err != nil {
			relPath = entry.Name()
		}

		if h.Hashes.IsUnchanged(relPath, "", info.Size()) {
			if ext == ".zip" {
				h.addFileToBookQueue(relPath, "", info.Size(), hash.UnchangedArchive, "")
			} else {
				h.addFileToBookQueue(relPath, "", info.Size(), hash.UnchangedFile, "")
			}
			continue
		}

		_, _, err = h.isFileReady(dir, entry)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}

			errStr := strings.ToLower(err.Error())

			if strings.Contains(errStr, "not a regular file") ||
				strings.Contains(errStr, "is empty") {
				continue 
			}

			h.LOG.I.Println(err.Error())

			
			if strings.Contains(errStr, "busy") ||
				strings.Contains(errStr, "still being written") {
				continue 
			}
			
			if strings.Contains(errStr, "corrupted") {
				h.addFileToBookQueue(relPath, "", info.Size(), hash.BadArchive, err.Error())
				continue
			}

			continue
		}

		if ext == ".zip" {
			start := time.Now()
			h.LOG.I.Println("zip: ", relPath)
			err = h.indexFB2Zip(path)
			if err != nil {
				h.LOG.W.Println(err)
			}
			h.LOG.S.Printf("%v elapsed for parsing %s ", time.Since(start), relPath)
			continue
		}

		h.ScanSema <- struct{}{}
		wg.Add(1) 

		go func(p, e string) {
			defer func() {
				<-h.ScanSema 
				wg.Done() 
			}()

			relFile, _ := filepath.Rel(h.CFG.Library.STOCK_DIR, p)
			h.LOG.I.Println("file: ", relFile)

			var parseErr error
			switch e {
			case ".fb2":
				parseErr = h.indexFB2File(p)
			case ".epub":
				parseErr = h.indexEPUBFile(p)
			case ".fb3":
				parseErr = h.indexFB3File(p)
			case ".pdf":
				parseErr = h.indexPDFFile(p)
			case ".mobi", ".azw", ".azw3", ".prc":
				parseErr = h.indexMOBIFile(p)
			}

			if parseErr != nil {
				h.LOG.W.Println(parseErr)
			}
		}(path, ext)
	}

	return nil
}

func (h *Handler) addFileToBookQueue(file, archive string, size int64, state hash.BookState, errText string) {
	h.BookQueue <- model.Book{
		File:    file,
		Archive: archive,
		Size:     size,
		Updated: int64(state), 
		Keywords: errText,
	}
}

func (h *Handler) indexFB2File(FB2Path string) error {
	fInfo, _ := os.Stat(FB2Path)

	relFile, err := filepath.Rel(h.CFG.Library.STOCK_DIR, FB2Path)
	if err != nil {
		relFile = fInfo.Name()
	}

	f, err := os.Open(FB2Path)
	if err != nil {
		h.addFileToBookQueue(relFile, "", fInfo.Size(), hash.FileOpenFailed, err.Error())
		return fmt.Errorf("failed to open file %s: %s", relFile, err)
	}
	defer f.Close()

	var p parser.Parser
	p, err = fb2.ParseFB2(f)
	if err != nil {
		h.addFileToBookQueue(relFile, "", fInfo.Size(), hash.FileHasErrors, err.Error())
		return fmt.Errorf("file %s has errors: %s", relFile, err)
	}
	h.LOG.D.Println(p)

	language := p.GetLanguage()
	if !h.acceptLanguage(language.Code) {
		msg := fmt.Sprintf("publication language \"%s\" is configured as not accepted, file %s has been skipped", language.Code, relFile)
		h.addFileToBookQueue(relFile, "", fInfo.Size(), hash.LanguageNotAccepted, msg)
		return fmt.Errorf("publication language \"%s\" is configured as not accepted, file %s has been skipped", language.Code, relFile)
	}

	book := &model.Book{
		File:     relFile, 
		Archive:  "",
		Size:     fInfo.Size(),
		Format:   p.GetFormat(),
		Title:    p.GetTitle(),
		Sort:     p.GetSort(),
		Year:     p.GetYear(),
		Plot:     p.GetPlot(),
		Cover:    p.GetCover(),
		Language: language,
		Authors:  p.GetAuthors(),
		Genres:   p.GetGenres(),
		Keywords: p.GetKeywords(),
		Sequences: p.GetSequences(),
		Updated: time.Now().UnixNano(),
	}
	
	h.GT.Refine(book)
	
	if !h.GT.IsAccepted(book.Genres) {
		msg := fmt.Sprintf("book genres are configured as not accepted, file %s has been skipped", book.File)
		h.addFileToBookQueue(book.File, book.Archive, book.Size, hash.GenreNotAccepted, msg)
		return fmt.Errorf(msg)
	}
	
	h.BookQueue <- *book
	return nil
}

func (h *Handler) indexFB3File(FB3Path string) error {
	fInfo, _ := os.Stat(FB3Path)

	relFile, err := filepath.Rel(h.CFG.Library.STOCK_DIR, FB3Path)
	if err != nil {
		relFile = fInfo.Name()
	}

	zr, err := zip.OpenReader(FB3Path)
	if err != nil {
		h.addFileToBookQueue(relFile, "", fInfo.Size(), hash.BadArchive, err.Error())
		return fmt.Errorf("incorrect zip archive %s: %v", relFile, err)
	}
	defer zr.Close()

	var p parser.Parser
	p, err = fb3.NewFB3(zr)
	if err != nil {
		h.addFileToBookQueue(relFile, "", fInfo.Size(), hash.FileHasErrors, err.Error())
		return fmt.Errorf("file %s has errors: %v", relFile, err)
	}

	language := p.GetLanguage()
	if !h.acceptLanguage(language.Code) {
		msg := fmt.Sprintf("publication language \"%s\" is configured as not accepted, file %s has been skipped", language.Code, relFile)
		h.addFileToBookQueue(relFile, "", fInfo.Size(), hash.LanguageNotAccepted, msg)
		return fmt.Errorf("publication language \"%s\" is configured as not accepted, file %s has been skipped", language.Code, relFile)
	}
	h.LOG.D.Println(p)

	book := &model.Book{
		File:     relFile,
		Archive:  "",
		Size:     fInfo.Size(),
		Format:   p.GetFormat(),
		Title:    p.GetTitle(),
		Sort:     p.GetSort(),
		Year:     p.GetYear(),
		Plot:     p.GetPlot(),
		Cover:    p.GetCover(),
		Language: language,
		Authors:  p.GetAuthors(),
		Genres:   p.GetGenres(),
		Keywords: p.GetKeywords(),
		Sequences: p.GetSequences(),
		Updated:  time.Now().UnixNano(),
	}
	
	
	h.GT.Refine(book)
	
	if !h.GT.IsAccepted(book.Genres) {
		msg := fmt.Sprintf("book genres are configured as not accepted, file %s has been skipped", book.File)
		h.addFileToBookQueue(book.File, book.Archive, book.Size, hash.GenreNotAccepted, msg)
		return fmt.Errorf(msg)
	}
	
	h.BookQueue <- *book
	return nil
}

func (h *Handler) indexEPUBFile(EPUBPath string) error {
	fInfo, _ := os.Stat(EPUBPath)

	relFile, err := filepath.Rel(h.CFG.Library.STOCK_DIR, EPUBPath)
	if err != nil {
		relFile = fInfo.Name()
	}

	zr, err := zip.OpenReader(EPUBPath)
	if err != nil {
		h.addFileToBookQueue(relFile, "", fInfo.Size(), hash.BadArchive, err.Error())
		return fmt.Errorf("incorrect zip archive %s: %v", relFile, err)
	}
	defer zr.Close()

	var p parser.Parser
	zPath, err := epub.GetOPFPath(zr)
	if err != nil {
		h.addFileToBookQueue(relFile, "", fInfo.Size(), hash.FileHasErrors, err.Error())
		return fmt.Errorf("file %s has errors: %v", relFile, err)
	}
	p, err = epub.NewOPF(zr, zPath)
	if err != nil {
		h.addFileToBookQueue(relFile, "", fInfo.Size(), hash.FileHasErrors, err.Error())
		return fmt.Errorf("file %s has errors: %v", relFile, err)
	}
	language := p.GetLanguage()
	if !h.acceptLanguage(language.Code) {
		msg := fmt.Sprintf("publication language \"%s\" is configured as not accepted, file %s has been skipped", language.Code, relFile)
		h.addFileToBookQueue(relFile, "", fInfo.Size(), hash.LanguageNotAccepted, msg)
		return fmt.Errorf("publication language \"%s\" is configured as not accepted, file %s has been skipped", language.Code, relFile)
	}
	h.LOG.D.Println(p)

	book := &model.Book{
		File:     relFile,
		Archive:  "",
		Size:     fInfo.Size(),
		Format:   p.GetFormat(),
		Title:    p.GetTitle(),
		Sort:     p.GetSort(),
		Year:     p.GetYear(),
		Plot:     p.GetPlot(),
		Cover:    p.GetCover(),
		Language: language,
		Authors:  p.GetAuthors(),
		Genres:   p.GetGenres(),
		Keywords: p.GetKeywords(),
		Sequences: p.GetSequences(),
		Updated: time.Now().UnixNano(),		
	}
		
	h.GT.Refine(book)
	
	if !h.GT.IsAccepted(book.Genres) {
		msg := fmt.Sprintf("book genres are configured as not accepted, file %s has been skipped", book.File)
		h.addFileToBookQueue(book.File, book.Archive, book.Size, hash.GenreNotAccepted, msg)
		return fmt.Errorf(msg)
	}
	
	h.BookQueue <- *book
	return nil
}

func (h *Handler) indexMOBIFile(MOBIPath string) error {
	fInfo, _ := os.Stat(MOBIPath)

	relFile, err := filepath.Rel(h.CFG.Library.STOCK_DIR, MOBIPath)
	if err != nil {
		relFile = fInfo.Name()
	}

	file := fInfo.Name()

	f, err := os.Open(MOBIPath)
	if err != nil {
		h.addFileToBookQueue(relFile, "", fInfo.Size(), hash.FileOpenFailed, err.Error())
		return fmt.Errorf("failed to open file %s: %s", MOBIPath, err)
	}
	defer f.Close()

	var p parser.Parser
	p, err = mobi.NewMOBI(f, file)
	if err != nil {
		h.addFileToBookQueue(relFile, "", fInfo.Size(), hash.FileHasErrors, err.Error())
		return fmt.Errorf("file %s has errors: %v", file, err)
	}

	language := p.GetLanguage()
	if !h.acceptLanguage(language.Code) {
		msg := fmt.Sprintf("language %s not accepted for %s", language.Code, file)
		h.addFileToBookQueue(relFile, "", fInfo.Size(), hash.LanguageNotAccepted, msg)
		return fmt.Errorf("language %s not accepted for %s", language.Code, file)
	}

	book := &model.Book{
		File:     relFile,
		Archive:  "",
		Size:     fInfo.Size(),
		Format:   p.GetFormat(),
		Title:    p.GetTitle(),
		Sort:     p.GetSort(),
		Year:     p.GetYear(),
		Plot:     p.GetPlot(),
		Cover:    p.GetCover(),
		Language: language,
		Authors:  p.GetAuthors(),
		Genres:   p.GetGenres(),
		Keywords: p.GetKeywords(),
		Sequences: p.GetSequences(),
		Updated: time.Now().UnixNano(),
	}
		
	h.GT.Refine(book)
	
	if !h.GT.IsAccepted(book.Genres) {
		msg := fmt.Sprintf("book genres are configured as not accepted, file %s has been skipped", book.File)
		h.addFileToBookQueue(book.File, book.Archive, book.Size, hash.GenreNotAccepted, msg)
		return fmt.Errorf(msg)
	}
	
	h.BookQueue <- *book
	return nil
}

func (h *Handler) indexPDFFile(PDFPath string) error {
	fInfo, _ := os.Stat(PDFPath)

	relFile, err := filepath.Rel(h.CFG.Library.STOCK_DIR, PDFPath)
	if err != nil {
		relFile = fInfo.Name()
	}

	p, err := pdf.NewPDF(PDFPath)
	h.LOG.D.Printf("file %s parsing finished", relFile)
	if err != nil {
		h.addFileToBookQueue(relFile, "", fInfo.Size(), hash.FileHasErrors, err.Error())
		return fmt.Errorf("file %s has errors: %v", relFile, err)
	}

	language := p.GetLanguage()
	if !h.acceptLanguage(language.Code) {
		msg := fmt.Sprintf("language %s not accepted for %s", language.Code, relFile)
		h.addFileToBookQueue(relFile, "", fInfo.Size(), hash.LanguageNotAccepted, msg)
		return fmt.Errorf("language %s not accepted for %s", language.Code, relFile)
	}

	book := &model.Book{
		File:     relFile, 
		Archive:  "",
		Size:     fInfo.Size(),
		Format:   p.GetFormat(),
		Title:    p.GetTitle(),
		Sort:     p.GetSort(),
		Year:     p.GetYear(),
		Plot:     p.GetPlot(),
		Cover:    "",
		Language: language,
		Authors:  p.GetAuthors(),
		Genres:   p.GetGenres(),
		Keywords: p.GetKeywords(),
		Sequences: p.GetSequences(),
		Updated: time.Now().UnixNano(),
	}
	
	h.GT.Refine(book)
	
	if !h.GT.IsAccepted(book.Genres) {
		msg := fmt.Sprintf("book genres are configured as not accepted, file %s has been skipped", book.File)
		h.addFileToBookQueue(book.File, book.Archive, book.Size, hash.GenreNotAccepted, msg)
		return fmt.Errorf(msg)
	}

	select {
	case h.BookQueue <- *book:
	case <-time.After(time.Second * 15):
		return fmt.Errorf("database timeout while indexing %s (DB thread might be stuck)", relFile) 
	}
	return nil
}

func (h *Handler) indexFB2Zip(zipPath string) error {

	relArchive, err := filepath.Rel(h.CFG.Library.STOCK_DIR, zipPath)
	if err != nil {
		relArchive = filepath.Base(zipPath)
	}

	h.LOG.D.Printf("archive %s indexing has been started\n", relArchive)

	fInfo, _ := os.Stat(zipPath)

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		h.addFileToBookQueue(relArchive, "", fInfo.Size(), hash.BadArchive, err.Error())
		return fmt.Errorf("incorrect zip archive %s: %s", relArchive, err)
	}

	h.BookQueue <- model.Book{
		File:    relArchive, 
		Archive: "",         
		Size:    fInfo.Size(),
		Updated: int64(hash.ArchiveContainer), 
	}
	
	fb2Count := 0
	for _, file := range zr.File {
		if file.UncompressedSize64 > 0 && strings.ToLower(filepath.Ext(file.Name)) == ".fb2" {
			fb2Count++
		}
	}
	isMulti := fb2Count > 1 
	
	defer func() {
		zr.Close()
		h.LOG.D.Printf("archive %s indexing has been finished\n", relArchive)
	}()

	var zipWG sync.WaitGroup 

	for _, file := range zr.File {
		if file.UncompressedSize64 == 0 {		
			continue
		}
				
		ext := strings.ToLower(filepath.Ext(file.Name))
		if ext != ".fb2" {
			h.LOG.D.Printf("file %s from %s is not fb2", file.Name, relArchive)
			continue
		}

		fileName := filepath.ToSlash(file.Name)

		if h.Hashes.IsUnchanged(fileName, relArchive, int64(file.UncompressedSize64)) {
			h.addFileToBookQueue(fileName, relArchive, int64(file.UncompressedSize64), hash.UnchangedFile, "")
			continue
		}

		h.ScanSema <- struct{}{}
		zipWG.Add(1)

		go func(f *zip.File, fName string) {
			defer func() {
				<-h.ScanSema 
				zipWG.Done() 
			}()

			rc, err := f.Open()
			if err != nil {
				h.addFileToBookQueue(fName, relArchive, int64(f.UncompressedSize64), hash.FileOpenFailed, err.Error())
				return
			}
			defer rc.Close()

			p, err := fb2.ParseFB2(rc)
			if err != nil {
				h.addFileToBookQueue(fName, relArchive, int64(f.UncompressedSize64), hash.FileHasErrors, err.Error())
				h.LOG.D.Printf("file %s from %s has error: <%s> and has been skipped\n", fName, relArchive, err.Error())
				return
			}

			language := p.GetLanguage()
			if !h.acceptLanguage(language.Code) {
				msg := fmt.Sprintf("publication language \"%s\" is not accepted, file %s from %s has been skipped\n", language.Code, fName, relArchive)
				h.addFileToBookQueue(fName, relArchive, int64(f.UncompressedSize64), hash.LanguageNotAccepted, msg)
				h.LOG.D.Printf("publication language \"%s\" is not accepted, file %s from %s has been skipped\n", language.Code, fName, relArchive)
				return
			}

			book := &model.Book{
				File:      fName,
				Archive:   relArchive,
				Size:      int64(f.UncompressedSize64),
				Format:    p.GetFormat(),
				Title:     p.GetTitle(),
				Sort:      p.GetSort(),
				Year:      p.GetYear(),
				Plot:      p.GetPlot(),
				Cover:     p.GetCover(),
				Language:  language,
				Authors:   p.GetAuthors(),
				Genres:    p.GetGenres(),
				Keywords:  p.GetKeywords(),
				Sequences: p.GetSequences(),
				Updated:   time.Now().UnixNano(),
				IsMultiArchive: isMulti,
			}

			h.GT.Refine(book)
			
			if !h.GT.IsAccepted(book.Genres) {
				msg := fmt.Sprintf("book genres are configured as not accepted, file %s has been skipped", book.File)
				h.addFileToBookQueue(book.File, book.Archive, book.Size, hash.GenreNotAccepted, msg)
				return 
			}
			
			h.BookQueue <- *book
		}(file, fileName)
	}

	zipWG.Wait()
	
	return nil
}



func (h *Handler) AddBooksToIndex() {
	tx := &database.TX{}
	defer func() {
		if tx.Tx != nil { 
			tx.TxEnd()
		}		
		h.StopDB <- struct{}{}
	}()
	
	timer := time.NewTimer(time.Second)
	if !timer.Stop() {
		<-timer.C
	}
	
	bookInTX := 0
	lastStatsUpdate := time.Now()
	
	for {
	
		//var timeout <-chan time.Time 
		if tx.Tx != nil {
			timer.Reset(time.Second)
		}
	
		select {
		case book := <-h.BookQueue:
			atomic.StoreInt32(&h.NeedOpt, 1)

			if bookInTX == 0 {
				tx = h.DB.TxBegin()
			}
			
			isUpdateOnly := false
			
			switch hash.BookState(book.Updated) {
			case hash.UnchangedFile:
				tx.Exec(`UPDATE books SET seen = ? WHERE file = ? AND archive = ? AND size = ?`, h.DB.ScanID, book.File, book.Archive, book.Size)
				isUpdateOnly = true
				
			case hash.UnchangedArchive:
				tx.Exec(`UPDATE books SET seen = ? WHERE archive = '' AND file = ? AND size = ?`, h.DB.ScanID, book.File, book.Size)
				isUpdateOnly = true
				
			case hash.ArchiveContainer:
				tx.Exec(`UPDATE books SET seen = 0 WHERE archive = ?`, book.File)
			}
			
			if !isUpdateOnly {
				h.Hashes.Add(book.File, book.Archive, book.Size)			
				tx.NewBook(&book, h.DB.ScanID)	

				if book.Archive == "" {
					h.LOG.I.Printf("single file %s has been added\n", book.File)
				} else {
					h.LOG.I.Printf("file %s from %s has been added\n", book.File, book.Archive)
				}
			}
			
			bookInTX++			
			
			if bookInTX >= h.CFG.Database.MAX_BOOKS_IN_TX {
				tx.TxEnd()
				tx.Tx = nil
				bookInTX = 0		

				if time.Since(lastStatsUpdate) >= 3 * 60 * time.Second {
					bunchesMap := make(map[string][]string)					
					
					for _, genre := range h.GT.ListGenres() {
						subgenres := h.GT.ListSubGenres(genre.Value)
						codes := make([]string, len(subgenres))
						for i, sg := range subgenres {
							codes[i] = sg.Value
						}
						bunchesMap[genre.Value] = codes
					}
					
					h.DB.UpdateAllStats(bunchesMap, false)
					
					lastStatsUpdate = time.Now()
				}
			}
			
		case <-timer.C:
			h.LOG.D.Printf("Book queue timeout")
			if tx.Tx != nil {
				tx.TxEnd()
				tx.Tx = nil
			}
			bookInTX = 0
			
		case replyChan := <-h.SyncDB:
			if tx.Tx != nil {
				tx.TxEnd() 
				tx.Tx = nil
			}
			bookInTX = 0
			h.DB.FolderCache = make(map[string]int64)
			replyChan <- struct{}{}	
			
		case <-h.StopDB:
			return
		}
	}
}

func (h *Handler) acceptLanguage(lang string) bool {
	if strings.Contains(h.CFG.ACCEPTED, "any") {
		return true
	}

	return h.CFG.Accepted[lang]
}

// ===============================
func ZipEntryInfo(e *zip.File) string {
	return "\n===========================================\n" +
		fmt.Sprintln("File               : ", e.Name) +
		fmt.Sprintln("NonUTF8            : ", e.NonUTF8) +
		fmt.Sprintln("Modified           : ", e.Modified) +
		fmt.Sprintln("CRC32              : ", e.CRC32) +
		fmt.Sprintln("UncompressedSize64 : ", e.UncompressedSize64) +
		"===========================================\n"
}