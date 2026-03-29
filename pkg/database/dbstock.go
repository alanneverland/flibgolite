package database

import (
	"database/sql"
	"log"
	"fmt"
	"time"
	"strings"
	"path/filepath"
	"github.com/vinser/flibgolite/pkg/hash"
	"github.com/vinser/flibgolite/pkg/model"
)

func (tx *TX) PrepareStatements() {
	tx.Stmt["selectIdFromFolders"] = tx.mustPrepare(`SELECT id FROM folders WHERE parent_id=? AND name=?`)
	tx.Stmt["insertIntoFolders"] = tx.mustPrepare(`INSERT INTO folders (parent_id, name, sort) VALUES (?, ?, ?)`)

	tx.Stmt["insertIntoBooks"] = tx.mustPrepare(`INSERT INTO books (folder_id, file, archive, size, format, title, sort, year, language, plot, cover, keywords, updated, seen) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	
	tx.Stmt["selectIdFromAuthors"] = tx.mustPrepare(`SELECT id FROM authors WHERE name=?`)
	tx.Stmt["insertIntoAuthors"] = tx.mustPrepare(`INSERT INTO authors (name, sort) VALUES (?, ?)`)
	tx.Stmt["insertIntoBooksAuthors"] = tx.mustPrepare(`INSERT INTO books_authors (book_id, author_id, language) VALUES (?, ?, ?)`)
	tx.Stmt["selectIdFromSeries"] = tx.mustPrepare(`SELECT id FROM series WHERE name=?`)
	tx.Stmt["insertIntoSeries"] = tx.mustPrepare(`INSERT INTO series (name, sort) VALUES (?, ?)`)
	tx.Stmt["insertIntoBooksSeries"] = tx.mustPrepare(`INSERT INTO books_series (book_id, serie_id, serie_num, language) VALUES (?, ?, ?, ?)`)
	tx.Stmt["insertIntoBooksGenres"] = tx.mustPrepare(`INSERT INTO books_genres (book_id, genre_code) VALUES (?, ?)`)	
}

func (tx *TX) GetOrCreateFolder(relPath string) int64 {
	if relPath == "" || relPath == "." {
		return 0
	}

	relPath = filepath.ToSlash(relPath)
	
	id, exists := tx.parentDB.FolderCache[relPath]
	if exists {
		return id
	}

	parts := strings.Split(relPath, "/")
	var parentId int64 = 0

	for _, part := range parts {
		if part == "" {
			continue
		}
		var currentId int64
		err := tx.Stmt["selectIdFromFolders"].QueryRow(parentId, part).Scan(&currentId)
		
		if err == sql.ErrNoRows {
			sortName := strings.ToUpper(part)
			res, err := tx.Stmt["insertIntoFolders"].Exec(parentId, part, sortName)
			if err != nil {
				log.Println("Insert Folder Error:", err)
				return 0
			}
			currentId, _ = res.LastInsertId()
		} else if err != nil {
			log.Println("Select Folder Error:", err)
			return 0
		}
		parentId = currentId
	}

	tx.parentDB.FolderCache[relPath] = parentId
	
	return parentId
}

func (db *DB) UpdateAllStats(bunchesMap map[string][]string, cleanup bool) {
	if len(bunchesMap) == 0 {
		return
	}

	tx, err := db.Begin()
	if err != nil {
		log.Println("UpdateAllStats begin error:", err)
		return
	}
	defer tx.Rollback()

	if cleanup {
		tx.Exec(`DELETE FROM stats_main WHERE language != '-' AND language NOT IN (SELECT DISTINCT language FROM books WHERE language != '')`)
		tx.Exec(`DELETE FROM stats_genres WHERE language != '-' AND language NOT IN (SELECT DISTINCT language FROM books WHERE language != '')`)
		tx.Exec(`DELETE FROM stats_subgenres WHERE language != '-' AND language NOT IN (SELECT DISTINCT language FROM books WHERE language != '')`)
	}

	tx.Exec(`
		REPLACE INTO stats_main (language, books_count, authors_count, series_count, genres_count, language_count)
		SELECT 
			'-',
			(SELECT COUNT(*) FROM books WHERE updated > 0),
			(SELECT COUNT(*) FROM authors WHERE sort NOT LIKE '[author not specified]'),
			(SELECT COUNT(*) FROM series),
			(SELECT COUNT(DISTINCT book_id) FROM books_genres),
			(SELECT COUNT(DISTINCT language) FROM books WHERE language != '')
	`)

	tx.Exec(`
		REPLACE INTO stats_main (language, books_count, authors_count, series_count, genres_count, language_count)
		SELECT 
			l.language,
			l.TotalBooks,
			(SELECT count(DISTINCT ba.author_id) FROM books b CROSS JOIN books_authors ba ON b.id = ba.book_id WHERE b.language = l.language AND b.updated > 0),
			(SELECT count(DISTINCT bs.serie_id) FROM books b CROSS JOIN books_series bs ON b.id = bs.book_id WHERE b.language = l.language AND b.updated > 0),
			(SELECT count(DISTINCT bg.book_id) FROM books b CROSS JOIN books_genres bg ON b.id = bg.book_id WHERE b.language = l.language AND b.updated > 0),
			1
		FROM (
			SELECT language, count(id) AS TotalBooks
			FROM books
			WHERE language != '' AND language IS NOT NULL AND updated > 0
			GROUP BY language
		) l;
	`)

	tx.Exec(`UPDATE stats_genres SET count = 0`)
	tx.Exec(`UPDATE stats_subgenres SET count = 0`)
	
	tx.Exec(`
		REPLACE INTO stats_subgenres (language, genre_code, count)
		SELECT 
			'-', 
			bg.genre_code, 
			COUNT(DISTINCT b.id)
		FROM books b
		JOIN books_genres bg ON b.id = bg.book_id
		WHERE b.updated > 0
		GROUP BY bg.genre_code
	`)

	tx.Exec(`
		REPLACE INTO stats_subgenres (language, genre_code, count)
		SELECT 
			b.language, 
			bg.genre_code, 
			COUNT(DISTINCT b.id)
		FROM books b
		JOIN books_genres bg ON b.id = bg.book_id
		WHERE b.language != '' AND b.updated > 0
		GROUP BY b.language, bg.genre_code
	`)

	tx.Exec(`CREATE TEMP TABLE IF NOT EXISTS temp_bunches (bunch TEXT, genre_code TEXT)`)
	tx.Exec(`DELETE FROM temp_bunches`)

	tx.Exec(`CREATE INDEX IF NOT EXISTS temp_bunches_genre_idx ON temp_bunches (genre_code)`)

	stmtTemp, _ := tx.Prepare(`INSERT INTO temp_bunches (bunch, genre_code) VALUES (?, ?)`)
	for bunch, codes := range bunchesMap {
		for _, code := range codes {
			stmtTemp.Exec(bunch, code)
		}
	}
	stmtTemp.Close()

	tx.Exec(`
		REPLACE INTO stats_genres (language, bunch, count)
		SELECT 
			'-', 
			tb.bunch, 
			COUNT(DISTINCT b.id)
		FROM books b
		JOIN books_genres bg ON b.id = bg.book_id
		JOIN temp_bunches tb ON bg.genre_code = tb.genre_code
		WHERE b.updated > 0
		GROUP BY tb.bunch
	`)

	tx.Exec(`
		REPLACE INTO stats_genres (language, bunch, count)
		SELECT 
			b.language, 
			tb.bunch, 
			COUNT(DISTINCT b.id)
		FROM books b
		JOIN books_genres bg ON b.id = bg.book_id
		JOIN temp_bunches tb ON bg.genre_code = tb.genre_code
		WHERE b.language != '' AND b.updated > 0
		GROUP BY b.language, tb.bunch
	`)

	tx.Exec(`DROP TABLE temp_bunches`)
	tx.Exec(`DELETE FROM stats_genres WHERE count = 0`)
	tx.Exec(`DELETE FROM stats_subgenres WHERE count = 0`)

	tx.Commit()
}

func (db *DB) ResetSeenFlag() error {
	db.ScanID = time.Now().Unix()
	return nil//err
}

func (db *DB) CleanUpDeletedBooks() (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %v", err)
	}
	defer tx.Rollback()
	
	var totalDeletedBooks int64 = 0
	var res sql.Result

	res, err = tx.Exec(`DELETE FROM books WHERE seen < ? AND archive = ''`, db.ScanID)
	if err != nil {
		log.Printf("Error deleting missing physical files: %v", err)
		return 0, err
	}
	deletedPhysical, _ := res.RowsAffected()
	totalDeletedBooks += deletedPhysical

	res, err = tx.Exec(`
		DELETE FROM books 
		WHERE archive != '' 
		  AND (seen = 0 OR archive NOT IN (SELECT file FROM books WHERE archive = ''))
	`)
	if err != nil {
		log.Printf("Error deleting archived books: %v", err)
		return 0, err
	}
	deletedArchived, _ := res.RowsAffected()
	totalDeletedBooks += deletedArchived
	
	if _, err := tx.Exec(`DELETE FROM books_authors WHERE book_id NOT IN (SELECT id FROM books)`); err != nil {
		log.Printf("Error cleaning books_authors: %v", err)
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM books_genres WHERE book_id NOT IN (SELECT id FROM books)`); err != nil {
		log.Printf("Error cleaning books_genres: %v", err)
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM books_series WHERE book_id NOT IN (SELECT id FROM books)`); err != nil {
		log.Printf("Error cleaning books_series: %v", err)
		return 0, err
	}	

	for {
		resFolders, err := tx.Exec(`
			DELETE FROM folders 
			WHERE id NOT IN (SELECT DISTINCT folder_id FROM books) 
			  AND id NOT IN (SELECT DISTINCT parent_id FROM folders)
		`)
		if err != nil {
			log.Printf("Error cleaning orphaned folders: %v", err)
			break
		}
		
		affected, _ := resFolders.RowsAffected()
		if affected == 0 {
			break
		}
	}

	if _, err := tx.Exec(`DELETE FROM authors WHERE id NOT IN (SELECT DISTINCT author_id FROM books_authors)`); err != nil {
		log.Printf("Error cleaning orphaned authors: %v", err)
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM series WHERE id NOT IN (SELECT DISTINCT serie_id FROM books_series)`); err != nil {
		log.Printf("Error cleaning orphaned sequences: %v", err)
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Error committing cleanup transaction: %v", err)
		return 0, err
	}

	return totalDeletedBooks, nil
}

func (db *DB) EndAndOptimize() error {	
	_, err := db.Exec("ANALYZE;")
	return err
}

// Books
func (tx *TX) NewBook(b *model.Book, scanID int64) {
	langCode := ""
	if b.Language != nil {
		langCode = b.Language.Code
	}
		
	b.FolderID = -1
	if b.Updated > 0 {
		dirPath := ""
		if b.Archive != "" {
			if b.IsMultiArchive {
				dirPath = b.Archive 
			} else {
				dirPath = filepath.Dir(b.Archive)
			}
		} else {
			dirPath = filepath.Dir(b.File)
		}
		
		b.FolderID = tx.GetOrCreateFolder(dirPath)
	}

	res, err := tx.Stmt["insertIntoBooks"].Exec(b.FolderID, b.File, b.Archive, b.Size, b.Format, b.Title, b.Sort, b.Year, langCode, b.Plot, b.Cover, b.Keywords, b.Updated, scanID)
	if err != nil {
		log.Panicln(err)
	}

	bookId, err := res.LastInsertId()
	if err != nil {
		log.Println(err)
		return
	}
	
	if b.Updated > 0 {
		
		q := `INSERT INTO books_fts (rowid, title, keywords) VALUES (?, ?, ?)`
		_, err = tx.Exec(q, bookId, b.Title, b.Keywords)
		if err != nil {
			log.Panicln(err)
		}

		for _, author := range b.Authors {
			authorId := tx.NewAuthor(author)
			_, err = tx.Stmt["insertIntoBooksAuthors"].Exec(bookId, authorId, langCode)
		}

		for _, genre := range b.Genres {
			_, err = tx.Stmt["insertIntoBooksGenres"].Exec(bookId, genre)
		}

		for _, seq := range b.Sequences {
			if seq != nil && seq.Name != "" {
				serieId := tx.NewSerie(&model.Serie{Name: seq.Name, Sort: seq.Sort})
				if serieId != 0 {
					_, err = tx.Stmt["insertIntoBooksSeries"].Exec(bookId, serieId, seq.Num, langCode)
					if err != nil {
						log.Println("BooksSeries Insert Error:", err)
					}
				} else {
					log.Printf("WARNING: lost serie '%s' for book ID %d", seq.Name, bookId)
				}
			}
		}
	}
}

func (tx *TX) RecordBookState(b *model.Book, s hash.BookState, scanID int64) {
	langCode := ""
	if b.Language != nil {
		langCode = b.Language.Code
	}
	
	b.FolderID = -1
	
	_, err := tx.Stmt["insertIntoBooks"].Exec(b.FolderID, b.File, b.Archive, b.Size, b.Format, b.Title, b.Sort, b.Year, langCode, b.Plot, b.Cover, b.Keywords, int64(s), scanID)
	if err != nil {
		log.Panicln(err)
	}
}

// Series
func (tx *TX) NewSerie(s *model.Serie) int64 {
	if s.Name == "" || s.Sort == "" {
		return 0
	}
	
	id := tx.FindSerie(s)
	if id != 0 {
		return id
	}	

	res, err := tx.Stmt["insertIntoSeries"].Exec(s.Name, s.Sort)
	if err != nil {
		log.Println("Insert Series Error:", err)
		return 0
	}
	id, _ = res.LastInsertId()

	q := `INSERT INTO series_fts (rowid, sort) VALUES (?, ?)`
	_, err = tx.Exec(q, id, s.Sort)
	if err != nil {
		log.Println("Insert Series FTS Error:", err)
	}
	
	return id
}

func (tx *TX) FindSerie(s *model.Serie) int64 {
	var id int64 = 0
	err := tx.Stmt["selectIdFromSeries"].QueryRow(s.Name).Scan(&id)
	if err == sql.ErrNoRows {
		return 0
	}
	return id
}

// Authors
func (tx *TX) NewAuthor(a *model.Author) int64 {
	if (a.Sort == "") {
		a.Name = "[author not specified]"
		a.Sort = "[AUTHOR NOT SPECIFIED]"
	}

	id := tx.FindAuthor(a)
	if id != 0 {
		return id
	}

	res, err := tx.Stmt["insertIntoAuthors"].Exec(a.Name, a.Sort)
	if err != nil {
		log.Printf("Name: %s, Sort: %s\n", a.Name, a.Sort)
		log.Panicln(err)
	}
	id, _ = res.LastInsertId()
	q := `INSERT INTO authors_fts (rowid, sort) VALUES (?, ?)`
	_, err = tx.Exec(q, id, a.Sort)
	if err != nil {
		log.Panicln(err)
	}
	return id
}

func (tx *TX) FindAuthor(a *model.Author) int64 {
	var id int64 = 0
	err := tx.Stmt["selectIdFromAuthors"].QueryRow(a.Name).Scan(&id)
	if err == sql.ErrNoRows {
		return 0
	}
	return id
}
