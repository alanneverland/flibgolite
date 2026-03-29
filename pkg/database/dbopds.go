package database

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
	"math/rand"
	"regexp"
	"unicode/utf8"

	"github.com/vinser/flibgolite/pkg/model"
	"github.com/vinser/flibgolite/pkg/user"
	"golang.org/x/crypto/bcrypt"
)

// Auth
var ErrorBadUserOrPassword = fmt.Errorf("Bad user name or password")

func (db *DB) GetUserByUsername(username string) (user.User, error) {
	// TODO Replace with real request
	users := map[string]user.User{
		"admin": {
			ID:       0,
			Username: "admin",
			Password: "admin",
			Email:    "admin@localhost",
		},
		"john": {
			ID:       1,
			Username: "john",
			Password: "p@ss",
			Email:    "john@localhost",
		},
	}

	if u, ok := users[username]; ok {
		hashed, err := bcrypt.GenerateFromPassword([]byte(u.Password), bcrypt.MinCost)
		if err != nil {
			return user.User{}, err
		}
		u.Password = string(hashed)
		return u, nil
	} else {
		return user.User{}, ErrorBadUserOrPassword
	}
}

func (db *DB) CalcBookLanguages() []model.LanguageStat {
	var langs []model.LanguageStat
	q := `SELECT language, count(id) as c FROM books WHERE language != '' AND updated > 0 GROUP BY language ORDER BY c DESC`
	rows, err := db.Query(q)
	if err != nil {
		log.Println("CalcBookLanguages error:", err)
		return langs
	}
	defer rows.Close()

	for rows.Next() {
		var l model.LanguageStat
		if err := rows.Scan(&l.Code, &l.Count); err == nil {
			langs = append(langs, l)
		}
	}
	return langs
}

func (db *DB) GetBookLanguages() []model.LanguageStat {
	var langs []model.LanguageStat
	q := `SELECT language, books_count FROM stats_main WHERE language != '-' ORDER BY books_count DESC`
	rows, err := db.Query(q)
	
	if err != nil {
		log.Println("GetBookLanguages error:", err)
		return db.CalcBookLanguages()
	}
	defer rows.Close()

	for rows.Next() {
		var l model.LanguageStat
		if err := rows.Scan(&l.Code, &l.Count); err == nil {
			langs = append(langs, l)
		}
	}

	if len(langs) == 0 {
		return db.CalcBookLanguages()
	}
	
	return langs
}

func (db *DB) FindBookById(id int64) *model.Book {
	b := &model.Book{}
	q := `SELECT file, archive, format, title, cover FROM books WHERE id=?`
	err := db.QueryRow(q, id).Scan(&b.File, &b.Archive, &b.Format, &b.Title, &b.Cover)
	if err == sql.ErrNoRows {
		return nil
	}
	return b
}

func (db *DB) CountLanguageBooks(languageCode string) int64 {
	var c int64 = 0
	q := `SELECT count(*) FROM books WHERE language=? AND updated > 0`
	err := db.QueryRow(q, languageCode).Scan(&c)
	if err == sql.ErrNoRows {
		return 0
	}
	return c
}

func (db *DB) sinceUnixNano(days int) int64 {
	now := time.Now()
	timeLimit := now.AddDate(0, 0, -days).UnixNano()

	var countLimit int64

	q := `SELECT updated FROM books WHERE updated > 0 ORDER BY updated DESC LIMIT 1 OFFSET 9999`
	err := db.QueryRow(q).Scan(&countLimit)
	if err != nil {
		return timeLimit
	}	

	if countLimit > timeLimit {
		return countLimit - 1
	}

	return timeLimit
}

func (db *DB) ListAuthors(prefix, abc string, days int, blang string) []*model.Author {
	prefixLen := utf8.RuneCountInString(prefix) + 1
	var (
		rows *sql.Rows
		err  error
		q    string
		args []interface{}
	)

	if days > 0 {
		var innerConds []string
		var outerConds []string

		innerConds = append(innerConds, fmt.Sprintf("b.updated > %d", db.sinceUnixNano(days)))
		
		if blang != "" {
			innerConds = append(innerConds, "b.language = ?")
			args = append(args, blang)
		}

		if prefixLen == 1 {
			if abc != "" {
				outerConds = append(outerConds, "SUBSTR(a.sort, 1, 1) IN("+abc+")")
			}
		} else {
			outerConds = append(outerConds, "a.sort LIKE ?")
			args = append(args, prefix+"%")
		}

		innerWhere := "WHERE " + strings.Join(innerConds, " AND ")
		outerWhere := ""
		if len(outerConds) > 0 {
			outerWhere = "WHERE " + strings.Join(outerConds, " AND ")
		}

		q = fmt.Sprintf(`
			SELECT MIN(a.id), MIN(a.name), SUBSTR(a.sort, 1, %d) AS p, COUNT(a.id)
			FROM authors a
			JOIN (
				SELECT DISTINCT ba.author_id
				FROM books b
				JOIN books_authors ba ON b.id = ba.book_id
				%s
			) t ON a.id = t.author_id
			%s
			GROUP BY p
		`, prefixLen, innerWhere, outerWhere)

		rows, err = db.Query(q, args...)

	} else if blang != "" {
		var conds []string
		
		if prefixLen == 1 {
			if abc != "" {
				conds = append(conds, "SUBSTR(a.sort, 1, 1) IN("+abc+")")
			}
		} else {
			conds = append(conds, "a.sort LIKE ?")
			args = append(args, prefix+"%")
		}

		conds = append(conds, `a.id IN (
			SELECT ba.author_id 
			FROM books_authors ba 
			WHERE ba.language = ?
		)`)
		args = append(args, blang)

		whereClause := ""
		if len(conds) > 0 {
			whereClause = "WHERE " + strings.Join(conds, " AND ")
		}

		q = fmt.Sprintf(`
			SELECT MIN(a.id), MIN(a.name), SUBSTR(a.sort, 1, %d) AS p, COUNT(a.id)
			FROM authors a
			%s
			GROUP BY p
		`, prefixLen, whereClause)

		rows, err = db.Query(q, args...)

	} else {

		if prefixLen == 1 {
			if abc != "" {
				q = `
				SELECT MIN(id), MIN(name), SUBSTR(sort, 1, 1) AS p, COUNT(id)
				FROM authors
				WHERE SUBSTR(sort, 1, 1) IN(` + abc + `)
				GROUP BY p
				`
			} else {
				q = `
				SELECT MIN(id), MIN(name), SUBSTR(sort, 1, 1) AS p, COUNT(id)
				FROM authors
				GROUP BY p
				`
			}
			rows, err = db.Query(q)
		} else {
			q = fmt.Sprintf(`
				SELECT MIN(id), MIN(name), SUBSTR(sort, 1, %d) AS p, COUNT(id)
				FROM authors
				WHERE sort LIKE ?
				GROUP BY p
				`, prefixLen)
			rows, err = db.Query(q, prefix+"%")
		}
	}

	if err != nil {
		log.Println("ListAuthors query error:", err)
		return []*model.Author{}
	}
	defer rows.Close()

	authors := []*model.Author{}
	for rows.Next() {
		a := &model.Author{}
		if err := rows.Scan(&a.ID, &a.Name, &a.Sort, &a.Count); err != nil {
			log.Println("ListAuthors scan error:", err)
			continue
		}
		authors = append(authors, a)
	}

	if len(authors) == 1 && authors[0].Count > 1 {
		if authors[0].Sort != prefix {
			return db.ListAuthors(authors[0].Sort, abc, days, blang)
		}
	}
	return authors
}

func (db *DB) AuthorNotSpecifiedId() int64 {
	q := `SELECT id FROM authors WHERE sort LIKE '[author not specified]'`
	var id int64
	err := db.QueryRow(q).Scan(&id)
	if err != nil {
		return 0
	}
	return id
}

func (db *DB) ListAuthorsByExactSort(sort string, days int, blang string) []*model.Author {
	authors := []*model.Author{}
	var (
		rows *sql.Rows
		err  error
		q    string
	)

	if days > 0 || blang != "" {
		var conds []string
		var args []interface{}

		if days > 0 {
			conds = append(conds, fmt.Sprintf("b.updated > %d", db.sinceUnixNano(days)))
		}
		if blang != "" {
			conds = append(conds, "b.language = ?")
			args = append(args, blang)
		}
		
		conds = append(conds, "a.sort = ?")
		args = append(args, sort)

		q = fmt.Sprintf(`
			SELECT a.id, a.name, a.sort, COUNT(DISTINCT b.id) AS count
			FROM books b
			JOIN books_authors ba ON b.id = ba.book_id
			JOIN authors a ON ba.author_id = a.id
			WHERE %s
			GROUP BY a.id, a.name, a.sort
			ORDER BY a.name
		`, strings.Join(conds, " AND "))
		rows, err = db.Query(q, args...)
	} else {
		q = `
			SELECT id, name, sort, (SELECT COUNT(*) FROM books_authors WHERE author_id = authors.id) AS count
			FROM authors
			WHERE sort = ?
			ORDER BY name
		`
		rows, err = db.Query(q, sort)
	}

	if err != nil {
		log.Println("ListAuthorsByExactSort error:", err)
		return authors
	}
	defer rows.Close()

	for rows.Next() {
		var a *model.Author = &model.Author{}
		if err := rows.Scan(&a.ID, &a.Name, &a.Sort, &a.Count); err != nil {
			log.Println(err)
			continue
		}
		if a.Count > 0 {
			authors = append(authors, a)
		}
	}
	return authors
}

func (db *DB) ListAuthorWithTotals(prefix string, days int, blang string) []*model.Author {
	var (
		rows *sql.Rows
		err  error
		q    string
		args []interface{}
	)

	if days > 0 {
		var conds []string
		
		conds = append(conds, fmt.Sprintf("b.updated > %d", db.sinceUnixNano(days)))
		
		if blang != "" {
			conds = append(conds, "ba.language = ?") 
			args = append(args, blang)
		}
		
		whereClause := " AND " + strings.Join(conds, " AND ")
		args = append(args, prefix+"%")

		q = fmt.Sprintf(`
			SELECT a.id, a.name, a.sort, COUNT(DISTINCT b.id)
			FROM authors a
			JOIN books_authors ba ON a.id = ba.author_id
			JOIN books b ON ba.book_id = b.id %s
			WHERE a.sort LIKE ?
			GROUP BY a.id, a.name, a.sort
		`, whereClause)

		rows, err = db.Query(q, args...)

	} else if blang != "" {
		args = append(args, blang, prefix+"%")

		q = `
			SELECT a.id, a.name, a.sort, COUNT(DISTINCT ba.book_id)
			FROM authors a
			JOIN books_authors ba ON a.id = ba.author_id
			WHERE ba.language = ? AND a.sort LIKE ?
			GROUP BY a.id, a.name, a.sort
		`
		rows, err = db.Query(q, args...)

	} else {
		q = `
			SELECT 
				a.id, 
				a.name, 
				a.sort, 
				(SELECT COUNT(DISTINCT book_id) FROM books_authors WHERE author_id = a.id) AS count
			FROM authors a
			WHERE a.sort LIKE ?
			ORDER BY a.sort
		`
		rows, err = db.Query(q, prefix+"%")
	}

	if err != nil {
		log.Println("ListAuthorWithTotals query error:", err)
		return []*model.Author{}
	}
	defer rows.Close()

	authors := []*model.Author{}
	for rows.Next() {
		a := &model.Author{}
		if err := rows.Scan(&a.ID, &a.Name, &a.Sort, &a.Count); err != nil {
			log.Println("ListAuthorWithTotals scan error:", err)
			continue
		}
		authors = append(authors, a)
	}
	return authors
}

func (db *DB) ListAuthorBooks(authorId, serieId int64, days int, limit, offset int, blang string) []*model.Book {
	var (
		q    string
		rows *sql.Rows
		err  error
	)

	var conds []string
	var args []interface{}

	conds = append(conds, "ba.author_id=?")
	args = append(args, authorId)

	if serieId != 0 {
		conds = append(conds, "bs.serie_id=?")
		args = append(args, serieId)
	}

	if blang != "" {
		conds = append(conds, "b.language=?")
		args = append(args, blang)
	}

	orderCond := "ORDER BY b.sort"
	if days > 0 {
		conds = append(conds, fmt.Sprintf("b.updated > %d", db.sinceUnixNano(days)))
		orderCond = "ORDER BY b.updated DESC"
	}

	whereClause := strings.Join(conds, " AND ")

	if serieId == 0 {
		q = fmt.Sprintf(`
		SELECT b.id, b.file, b.archive, b.size, b.format, b.title, b.year, b.plot, b.cover, b.language, b.keywords 
		FROM books as b 
		JOIN books_authors as ba ON b.id=ba.book_id 
		WHERE %s
		%s
		`, whereClause, orderCond)
	} else {
		q = fmt.Sprintf(`
		SELECT b.id, b.file, b.archive, b.size, b.format, b.title, b.year, b.plot, b.cover, b.language, b.keywords
		FROM books as b
		JOIN books_authors as ba ON b.id=ba.book_id
		JOIN books_series as bs ON b.id=bs.book_id
		WHERE %s
		ORDER BY bs.serie_num
		`, whereClause)
	}
	
	rows, err = db.pageQuery(q, limit, offset, args...)
	if err != nil {
		log.Println("DB page query error: ", err.Error())
	}
	defer rows.Close()
	books := []*model.Book{}

	for rows.Next() {
		b := &model.Book{
			Language: &model.Language{},
		}

		if err = rows.Scan(&b.ID, &b.File, &b.Archive, &b.Size, &b.Format, &b.Title, &b.Year, &b.Plot, &b.Cover, &b.Language.Code, &b.Keywords); err != nil {
			log.Fatal(err)
		}
		books = append(books, b)
	}
	return books
}

func (db *DB) AuthorBookSeries(authorId int64, blang string) []*model.Serie {
	series := []*model.Serie{}
	
	q := `
		SELECT 
			bs.serie_id, 
			s.name,
			COUNT(DISTINCT b.id)
		FROM books_authors as ba 
		JOIN books as b ON b.id=ba.book_id
		JOIN books_series as bs ON bs.book_id=b.id
		JOIN series as s ON s.id=bs.serie_id
		WHERE ba.author_id=? 
	`
	args := []interface{}{authorId}
	
	if blang != "" {
		q += " AND b.language=? "
		args = append(args, blang)
	}
	q += " GROUP BY bs.serie_id "
	
	rows, err := db.Query(q, args...)
	if err != nil {
		return series
	}
	defer rows.Close()

	for rows.Next() {
		s := &model.Serie{}
		if err := rows.Scan(&s.ID, &s.Name, &s.Count); err != nil {
			return series
		}
		series = append(series, s)
	}
	return series
}

func (db *DB) AuthorByID(authorId int64, blang string) *model.Author {
	author := &model.Author{}
	var err error
	
	if blang != "" {
		q := `
			SELECT 
				a.name, 
				a.sort,
				COUNT(DISTINCT ba.book_id) AS count
			FROM authors AS a 
			JOIN books_authors ba ON ba.author_id = a.id
			JOIN books b ON b.id = ba.book_id
			WHERE a.id=? AND b.language=?
			GROUP BY a.id, a.name, a.sort
		`
		err = db.QueryRow(q, authorId, blang).Scan(&author.Name, &author.Sort, &author.Count)
	} else {
		q := `
			SELECT 
				a.name, 
				a.sort,
				(SELECT COUNT(*) FROM books_authors WHERE author_id = a.id) AS count
			FROM authors AS a 
			WHERE a.id=?
		`
		err = db.QueryRow(q, authorId).Scan(&author.Name, &author.Sort, &author.Count)
	}
	
	if err == sql.ErrNoRows {
		return nil
	}
	return author
}

func (db *DB) AuthorsByBookId(bookId int64) []*model.Author {
	authors := []*model.Author{}
	q := `
		SELECT a.id, a.name, a.sort 
		FROM authors as a 
		JOIN books_authors as ba ON a.id=ba.author_id 
		WHERE ba.book_id=?
		ORDER BY a.sort
	`
	rows, err := db.Query(q, bookId)
	if err != nil {
		return authors
	}
	defer rows.Close()

	for rows.Next() {
		a := &model.Author{}
		if err := rows.Scan(&a.ID, &a.Name, &a.Sort); err != nil {
			return authors
		}
		authors = append(authors, a)
	}
	return authors
}

func (db *DB) GetBunchesStats(blang string) map[string]int64 {
	res := make(map[string]int64)
	langKey := blang
	if langKey == "" {
		langKey = "-"
	}
	
	rows, err := db.Query("SELECT bunch, count FROM stats_genres WHERE language = ?", langKey)
	if err != nil {
		log.Println("GetBunchesStats error:", err)
		return res
	}
	defer rows.Close()
	
	for rows.Next() {
		var b string
		var c int64
		if err := rows.Scan(&b, &c); err == nil {
			res[b] = c
		}
	}
	return res
}

func (db *DB) CountBunchesBooks(bunches map[string][]string, days int, blang string) map[string]int64 {
	res := make(map[string]int64)
	if len(bunches) == 0 {
		return res
	}

	var queryParts []string
	var args []interface{}

	if days <= 0 && blang == "" {

		for bunch := range bunches {
			var count int64
			err := db.QueryRow(`SELECT count FROM stats_genres WHERE language = '-' AND bunch = ?`, bunch).Scan(&count)
			if err == nil {
				res[bunch] = count
			} else if err != sql.ErrNoRows {
				log.Println("CountBunchesBooks cache read error:", err)
			}
		}
		return res

	} else if days <= 0 && blang != "" {
		for bunch := range bunches {
			var count int64
			err := db.QueryRow(`SELECT count FROM stats_genres WHERE language = ? AND bunch = ?`, blang, bunch).Scan(&count)
			if err == nil {
				res[bunch] = count
			} else if err != sql.ErrNoRows {
				log.Println("CountBunchesBooks cache read error:", err)
			}
		}
		return res

	} else if days > 0 {
		for bunch, codes := range bunches {
			if len(codes) == 0 {
				continue
			}
			var bookConds []string

			args = append(args, bunch)

			bookConds = append(bookConds, "b.updated > ?")
			args = append(args, db.sinceUnixNano(days))

			if blang != "" {
				bookConds = append(bookConds, "b.language = ?")
				args = append(args, blang)
			}

			placeholders := make([]string, len(codes))
			for i, code := range codes {
				placeholders[i] = "?"
				args = append(args, code)
			}

			q := fmt.Sprintf(`
				SELECT ? as bunch, COUNT(b.id) as c
				FROM books b
				WHERE %s
				  AND EXISTS (
					  SELECT 1 FROM books_genres bg 
					  WHERE bg.book_id = b.id AND bg.genre_code IN (%s)
				  )
			`, strings.Join(bookConds, " AND "), strings.Join(placeholders, ","))
			queryParts = append(queryParts, q)
		}

		if len(queryParts) == 0 {
			return res
		}

		finalQuery := strings.Join(queryParts, " UNION ALL ")

		rows, err := db.Query(finalQuery, args...)
		if err != nil {
			log.Println("CountBunchesBooks error:", err)
			return res
		}
		defer rows.Close()

		for rows.Next() {
			var b string
			var c int64
			if err := rows.Scan(&b, &c); err == nil {
				res[b] = c
			}
		}
		
		if err = rows.Err(); err != nil {
			log.Println("Rows iteration error:", err)
		}
	}

	return res
}

func (db *DB) PageGenreBooks(genreCode string, days int, limit, offset int, blang string) []*model.Book {
	var (
		q    string
		rows *sql.Rows
		err  error
	)

	if days > 0 {
		var conds []string
		var args []interface{}

		conds = append(conds, "b1.updated > ?")
		args = append(args, db.sinceUnixNano(days))

		if blang != "" {
			conds = append(conds, "b1.language = ?")
			args = append(args, blang)
		}

		conds = append(conds, "EXISTS (SELECT 1 FROM books_genres bg WHERE bg.book_id = b1.id AND bg.genre_code = ?)")
		args = append(args, genreCode)

		q = fmt.Sprintf(`
			SELECT b.id, b.file, b.archive, b.size, b.format, b.title, b.sort, b.year, b.plot, b.cover, b.language, b.keywords 
			FROM (
				SELECT b1.id 
				FROM books AS b1
				WHERE %s
				ORDER BY b1.updated DESC 
				LIMIT ? OFFSET ?
			) AS t
			JOIN books AS b ON t.id = b.id
			ORDER BY b.updated DESC
		`, strings.Join(conds, " AND "))

		args = append(args, limit, offset)
		rows, err = db.Query(q, args...)

	} else {

		var conds []string
		var args []interface{}

		conds = append(conds, "bg1.genre_code = ?")
		args = append(args, genreCode)

		if blang != "" {
			conds = append(conds, "b1.language = ?")
			args = append(args, blang)
		}
		conds = append(conds, "b1.updated > 0")

		q = fmt.Sprintf(`
			SELECT b.id, b.file, b.archive, b.size, b.format, b.title, b.sort, b.year, b.plot, b.cover, b.language, b.keywords 
			FROM (
				SELECT b1.id 
				FROM books_genres AS bg1
				CROSS JOIN books AS b1 ON bg1.book_id = b1.id
				WHERE %s
				ORDER BY b1.sort ASC 
				LIMIT ? OFFSET ?
			) AS t
			JOIN books AS b ON t.id = b.id
			ORDER BY b.sort ASC
		`, strings.Join(conds, " AND "))

		args = append(args, limit, offset)
		rows, err = db.Query(q, args...)
	}

	if err != nil {
		log.Println("DB page query error: ", err.Error())
		return []*model.Book{}
	}
	defer rows.Close()

	books := []*model.Book{}
	for rows.Next() {
		b := &model.Book{
			Language: &model.Language{},
		}
		if err = rows.Scan(&b.ID, &b.File, &b.Archive, &b.Size, &b.Format, &b.Title, &b.Sort, &b.Year, &b.Plot, &b.Cover, &b.Language.Code, &b.Keywords); err != nil {
			log.Println("Row scan error:", err) 
			continue
		}
		books = append(books, b)
	}

	if err = rows.Err(); err != nil {
		log.Println("Rows iteration error:", err)
	}

	return books
}

func (db *DB) GetGenreCountsInBunch(bunch string, subgenreCodes []string, days int, blang string) map[string]int64 {
	res := make(map[string]int64)
	if len(subgenreCodes) == 0 {
		return res
	}

	if days <= 0 && blang == "" {
		placeholders := make([]string, len(subgenreCodes))
		var args []interface{}
		args = append(args, "-") 
		for i, code := range subgenreCodes {
			placeholders[i] = "?"
			args = append(args, code)
		}
		
		q := fmt.Sprintf(`
			SELECT genre_code, count 
			FROM stats_subgenres 
			WHERE language = ? AND genre_code IN (%s)
		`, strings.Join(placeholders, ","))
		
		rows, err := db.Query(q, args...)
		if err != nil {
			log.Println("GetGenreCountsInBunch cache error:", err)
			return res
		}
		defer rows.Close()
		
		for rows.Next() {
			var code string
			var count int64
			if err := rows.Scan(&code, &count); err == nil {
				res[code] = count
			}
		}
		return res

	} else if days <= 0 && blang != "" {
		placeholders := make([]string, len(subgenreCodes))
		var args []interface{}
		args = append(args, blang)
		for i, code := range subgenreCodes {
			placeholders[i] = "?"
			args = append(args, code)
		}
		
		q := fmt.Sprintf(`
			SELECT genre_code, count 
			FROM stats_subgenres 
			WHERE language = ? AND genre_code IN (%s)
		`, strings.Join(placeholders, ","))
		
		rows, err := db.Query(q, args...)
		if err != nil {
			log.Println("GetGenreCountsInBunch cache error:", err)
			return res
		}
		defer rows.Close()
		
		for rows.Next() {
			var code string
			var count int64
			if err := rows.Scan(&code, &count); err == nil {
				res[code] = count
			}
		}
		return res
	} else if days > 0 {
		placeholders := make([]string, len(subgenreCodes))
		var codesArgs []interface{}
		for i, code := range subgenreCodes {
			placeholders[i] = "?"
			codesArgs = append(codesArgs, code)
		}
		inClause := strings.Join(placeholders, ",")

		var conds []string
		var args []interface{}

		conds = append(conds, "b.updated > ?")
		args = append(args, db.sinceUnixNano(days))

		if blang != "" {
			conds = append(conds, "b.language = ?")
			args = append(args, blang)
		}

		conds = append(conds, fmt.Sprintf("bg.genre_code IN (%s)", inClause))
		args = append(args, codesArgs...)

		q := fmt.Sprintf(`
			SELECT bg.genre_code, COUNT(b.id)
			FROM books b
			CROSS JOIN books_genres bg ON bg.book_id = b.id
			WHERE %s
			GROUP BY bg.genre_code
		`, strings.Join(conds, " AND "))

		rows, err := db.Query(q, args...)
		if err != nil {
			log.Println("GetGenreCountsInBunch error:", err)
			return res
		}
		defer rows.Close()

		for rows.Next() {
			var code string
			var count int64
			if err := rows.Scan(&code, &count); err == nil {
				res[code] = count
			}
		}
		return res
	}

	return res
}

func (db *DB) CountGenreBooks(genreCode string, days int, blang string) int64 {
	var c int64 = 0

	if days <= 0 {
		langKey := "-"
		if blang != "" {
			langKey = blang
		}
		q := `SELECT count FROM stats_subgenres WHERE language = ? AND genre_code = ?`
		err := db.QueryRow(q, langKey, genreCode).Scan(&c)
		if err != nil && err != sql.ErrNoRows {
			log.Println("CountGenreBooks cache error:", err)
		}
		return c
	}

	if days > 0 {
		var bookConds []string
		var args []interface{}

		bookConds = append(bookConds, "b.updated > ?")
		args = append(args, db.sinceUnixNano(days))

		if blang != "" {
			bookConds = append(bookConds, "b.language = ?")
			args = append(args, blang)
		}

		bookConds = append(bookConds, "EXISTS (SELECT 1 FROM books_genres bg WHERE bg.book_id = b.id AND bg.genre_code = ?)")
		args = append(args, genreCode)

		q := fmt.Sprintf(`SELECT count(b.id) FROM books b WHERE %s`, strings.Join(bookConds, " AND "))
		err := db.QueryRow(q, args...).Scan(&c)
		if err != nil && err != sql.ErrNoRows {
			log.Println("CountGenreBooks error:", err)
		}
		return c
	}

	return 0
}

func (db *DB) ListSerieBooks(id int64, days int, limit, offset int, blang string) []*model.Book {
	var conds []string
	var args []interface{}
	
	conds = append(conds, "bs.serie_id=?")
	args = append(args, id)
	
	if blang != "" {
		conds = append(conds, "b.language = ?")
		args = append(args, blang)
	}

	orderCond := "ORDER BY bs.serie_num"
	if days > 0 {
		conds = append(conds, fmt.Sprintf("b.updated > %d", db.sinceUnixNano(days)))
		orderCond = "ORDER BY b.updated DESC"
	}

	q := fmt.Sprintf(`
		SELECT b.id, b.file, b.archive, b.size, b.format, b.title, b.year, b.plot, b.cover, b.language, b.keywords 
		FROM books as b 
		JOIN books_series as bs ON b.id=bs.book_id
		WHERE %s
		%s
	`, strings.Join(conds, " AND "), orderCond)

	rows, err := db.pageQuery(q, limit, offset, args...)
	if err != nil {
		log.Println("DB page query error: ", err.Error())
	}
	defer rows.Close()
	books := []*model.Book{}

	for rows.Next() {
		b := &model.Book{
			Language: &model.Language{},
		}
		if err = rows.Scan(&b.ID, &b.File, &b.Archive, &b.Size, &b.Format, &b.Title, &b.Year, &b.Plot, &b.Cover, &b.Language.Code, &b.Keywords); err != nil {
			log.Fatal(err)
		}
		books = append(books, b)
	}
	return books
}

func (db *DB) ListSeries(prefix, abc string, days int, blang string) []*model.Serie {
	prefixLen := utf8.RuneCountInString(prefix) + 1
	var (
		rows *sql.Rows
		err  error
		q    string
		args []interface{}
	)

	if days > 0 {
		var innerConds []string
		var outerConds []string

		innerConds = append(innerConds, fmt.Sprintf("b.updated > %d", db.sinceUnixNano(days)))
		
		if blang != "" {
			innerConds = append(innerConds, "b.language = ?")
			args = append(args, blang)
		}

		if prefixLen == 1 {
			if abc != "" {
				outerConds = append(outerConds, "SUBSTR(s.sort, 1, 1) IN("+abc+")")
			}
		} else {
			outerConds = append(outerConds, "s.sort LIKE ?")
			args = append(args, prefix+"%")
		}

		innerWhere := "WHERE " + strings.Join(innerConds, " AND ")
		outerWhere := ""
		if len(outerConds) > 0 {
			outerWhere = "WHERE " + strings.Join(outerConds, " AND ")
		}

		q = fmt.Sprintf(`
			SELECT MIN(s.id), MIN(s.name), SUBSTR(s.sort, 1, %d) AS p, COUNT(s.id)
			FROM series s
			JOIN (
				SELECT DISTINCT bs.serie_id
				FROM books b
				JOIN books_series bs ON b.id = bs.book_id
				%s
			) t ON s.id = t.serie_id
			%s
			GROUP BY p
		`, prefixLen, innerWhere, outerWhere)

		rows, err = db.Query(q, args...)

	} else if blang != "" {
		var conds []string
		
		if prefixLen == 1 {
			if abc != "" {
				conds = append(conds, "SUBSTR(s.sort, 1, 1) IN("+abc+")")
			}
		} else {
			conds = append(conds, "s.sort LIKE ?")
			args = append(args, prefix+"%")
		}

		conds = append(conds, `s.id IN (
			SELECT bs.serie_id 
			FROM books_series bs 
			WHERE bs.language = ?
		)`)
		args = append(args, blang)

		whereClause := ""
		if len(conds) > 0 {
			whereClause = "WHERE " + strings.Join(conds, " AND ")
		}

		q = fmt.Sprintf(`
			SELECT MIN(s.id), MIN(s.name), SUBSTR(s.sort, 1, %d) AS p, COUNT(s.id)
			FROM series s
			%s
			GROUP BY p
		`, prefixLen, whereClause)

		rows, err = db.Query(q, args...)

	} else {
		if prefixLen == 1 {
			if abc != "" {
				q = `
				SELECT MIN(id), MIN(name), SUBSTR(sort, 1, 1) AS p, COUNT(id)
				FROM series
				WHERE SUBSTR(sort, 1, 1) IN(` + abc + `)
				GROUP BY p
				`
			} else {
				q = `
				SELECT MIN(id), MIN(name), SUBSTR(sort, 1, 1) AS p, COUNT(id)
				FROM series
				GROUP BY p
				`
			}
			rows, err = db.Query(q)
		} else {
			q = fmt.Sprintf(`
				SELECT MIN(id), MIN(name), SUBSTR(sort, 1, %d) AS p, COUNT(id)
				FROM series
				WHERE sort LIKE ?
				GROUP BY p
				`, prefixLen)
			rows, err = db.Query(q, prefix+"%")
		}
	}

	if err != nil {
		log.Println("ListSeries query error:", err)
		return []*model.Serie{}
	}
	defer rows.Close()

	series := []*model.Serie{}
	for rows.Next() {
		s := &model.Serie{}
		if err := rows.Scan(&s.ID, &s.Name, &s.Sort, &s.Count); err != nil {
			log.Println("ListSeries scan error:", err)
			continue
		}
		series = append(series, s)
	}

	if len(series) == 1 && series[0].Count > 1 {
		if series[0].Sort != prefix {
			return db.ListSeries(series[0].Sort, abc, days, blang)
		}
	}
	return series
}

func (db *DB) ListSeriesWithTotals(prefix string, days int, blang string) []*model.Serie {
	var (
		rows *sql.Rows
		err  error
		q    string
		args []interface{}
	)

	if days > 0 {
		var conds []string
		
		conds = append(conds, fmt.Sprintf("b.updated > %d", db.sinceUnixNano(days)))
		
		if blang != "" {
			conds = append(conds, "bs.language = ?")
			args = append(args, blang)
		}
		
		whereClause := " AND " + strings.Join(conds, " AND ")
		args = append(args, prefix+"%")

		q = fmt.Sprintf(`
			SELECT s.id, s.name, s.sort, COUNT(DISTINCT b.id)
			FROM series s
			JOIN books_series bs ON s.id = bs.serie_id
			JOIN books b ON bs.book_id = b.id %s
			WHERE s.sort LIKE ?
			GROUP BY s.id, s.name, s.sort
		`, whereClause)

		rows, err = db.Query(q, args...)

	} else if blang != "" {
		args = append(args, blang, prefix+"%")

		q = `
			SELECT s.id, s.name, s.sort, COUNT(DISTINCT bs.book_id)
			FROM series s
			JOIN books_series bs ON s.id = bs.serie_id
			WHERE bs.language = ? AND s.sort LIKE ?
			GROUP BY s.id, s.name, s.sort
		`
		rows, err = db.Query(q, args...)

	} else {
		q = `
			SELECT 
				s.id, 
				s.name, 
				s.sort, 
				(SELECT COUNT(DISTINCT book_id) FROM books_series WHERE serie_id = s.id) AS count
			FROM series s
			WHERE s.sort LIKE ?
			ORDER BY s.sort
		`
		rows, err = db.Query(q, prefix+"%")
	}

	if err != nil {
		log.Println("ListSeriesWithTotals query error:", err)
		return []*model.Serie{}
	}
	defer rows.Close()

	series := []*model.Serie{}
	for rows.Next() {
		s := &model.Serie{}
		if err := rows.Scan(&s.ID, &s.Name, &s.Sort, &s.Count); err != nil {
			log.Println("ListSeriesWithTotals scan error:", err)
			continue
		}
		series = append(series, s)
	}
	return series
}

func (db *DB) SerieByID(serieId int64, blang string) *model.Serie {
	serie := &model.Serie{}
	var err error
	
	if blang != "" {
		q := `
			SELECT 
				s.name, 
				s.sort,
				COUNT(DISTINCT bs.book_id) AS count
			FROM series AS s 
			JOIN books_series bs ON bs.serie_id = s.id
			JOIN books b ON b.id = bs.book_id
			WHERE s.id=? AND b.language=?
			GROUP BY s.id, s.name, s.sort
		`
		err = db.QueryRow(q, serieId, blang).Scan(&serie.Name, &serie.Sort, &serie.Count)
	} else {
		q := `
			SELECT 
				s.name, 
				s.sort,
				(SELECT COUNT(DISTINCT bs.book_id) FROM books_series bs WHERE bs.serie_id = s.id) AS count
			FROM series AS s 
			WHERE s.id=?
		`
		err = db.QueryRow(q, serieId).Scan(&serie.Name, &serie.Sort, &serie.Count)
	}

	if err == sql.ErrNoRows {
		return nil
	}
	return serie
}

func (db *DB) ListSeriesByExactSort(sort string, days int, blang string) []*model.Serie {
	series := []*model.Serie{}
	var (
		rows *sql.Rows
		err  error
		q    string
	)

	if days > 0 || blang != "" {
		var conds []string
		var args []interface{}

		if days > 0 {
			conds = append(conds, fmt.Sprintf("b.updated > %d", db.sinceUnixNano(days)))
		}
		if blang != "" {
			conds = append(conds, "b.language = ?")
			args = append(args, blang)
		}
		
		conds = append(conds, "s.sort = ?")
		args = append(args, sort)

		q = fmt.Sprintf(`
			SELECT s.id, s.name, s.sort, COUNT(DISTINCT b.id) AS count
			FROM books b
			JOIN books_series bs ON b.id = bs.book_id
			JOIN series s ON bs.serie_id = s.id
			WHERE %s
			GROUP BY s.id, s.name, s.sort
			ORDER BY s.name
		`, strings.Join(conds, " AND "))
		rows, err = db.Query(q, args...)
	} else {
		q = `
			SELECT id, name, sort, (SELECT COUNT(DISTINCT book_id) FROM books_series WHERE serie_id = series.id) AS count
			FROM series
			WHERE sort = ?
			ORDER BY name
		`
		rows, err = db.Query(q, sort)
	}

	if err != nil {
		log.Println("ListSeriesByExactSort error:", err)
		return series
	}
	defer rows.Close()

	for rows.Next() {
		s := &model.Serie{}
		if err := rows.Scan(&s.ID, &s.Name, &s.Sort, &s.Count); err != nil {
			log.Println(err)
			continue
		}
		if s.Count > 0 {
			series = append(series, s)
		}
	}
	return series
}

func (db *DB) SeriesByBookID(bookId int64) []*model.Sequence {
	seqs := []*model.Sequence{}
	q := `
		SELECT s.id, s.name, bs.serie_num 
		FROM books_series as bs 
		JOIN series as s ON s.id=bs.serie_id 
		WHERE bs.book_id=? 
		ORDER BY bs.serie_num
	`
	rows, err := db.Query(q, bookId)
	if err != nil {
		return seqs
	}
	defer rows.Close()

	for rows.Next() {
		seq := &model.Sequence{}
		if err := rows.Scan(&seq.ID, &seq.Name, &seq.Num); err == nil {
			seqs = append(seqs, seq)
		}
	}
	return seqs
}

func (db *DB) GetLatestBookLanguages(days int) []model.LanguageStat {
	var langs []model.LanguageStat
	q := `SELECT language, count(id) as c FROM books WHERE language != '' AND updated > ? GROUP BY language ORDER BY c DESC`
	
	rows, err := db.Query(q, db.sinceUnixNano(days))
	if err != nil {
		log.Println("GetLatestBookLanguages error:", err)
		return langs
	}
	defer rows.Close()

	for rows.Next() {
		var l model.LanguageStat
		if err := rows.Scan(&l.Code, &l.Count); err == nil {
			langs = append(langs, l)
		}
	}
	return langs
}

func (db *DB) GetLatestStats(days int, blang string) DBStats {
	s := DBStats{}

	if days <= 0 && blang == "" {
		return db.GetGlobalStats()
	}

	if days <= 0 && blang != "" {
		q := `SELECT books_count, authors_count, series_count, genres_count FROM stats_main WHERE language = ?`
		err := db.QueryRow(q, blang).Scan(&s.TotalBooks, &s.TotalAuthors, &s.TotalSeries, &s.TotalGenres)
		if err == nil {
			s.TotalLanguages = 1
			return s
		}
		log.Println("Stats cache read error:", err)
	}

	var conds []string
	var args []interface{}

	if blang != "" {
		conds = append(conds, "b.language = ?")
		args = append(args, blang)
	}

	if days > 0 {
		conds = append(conds, "b.updated > ?")
		args = append(args, db.sinceUnixNano(days))
	}

	whereClause := "WHERE " + strings.Join(conds, " AND ")

	qBooks := fmt.Sprintf(`SELECT count(*) FROM books b %s`, whereClause)
	db.QueryRow(qBooks, args...).Scan(&s.TotalBooks)

	qAuthors := fmt.Sprintf(`SELECT count(DISTINCT ba.author_id) FROM books b CROSS JOIN books_authors ba ON b.id = ba.book_id %s`, whereClause)
	db.QueryRow(qAuthors, args...).Scan(&s.TotalAuthors)

	qSeries := fmt.Sprintf(`SELECT count(DISTINCT bs.serie_id) FROM books b CROSS JOIN books_series bs ON b.id = bs.book_id %s`, whereClause)
	db.QueryRow(qSeries, args...).Scan(&s.TotalSeries)

	qGenres := fmt.Sprintf(`SELECT count(DISTINCT bg.book_id) FROM books b CROSS JOIN books_genres bg ON b.id = bg.book_id %s`, whereClause)
	db.QueryRow(qGenres, args...).Scan(&s.TotalGenres)

	if blang == "" {
		langWhereClause := whereClause + " AND b.language != ''"
		qLanguages := fmt.Sprintf(`SELECT count(DISTINCT b.language) FROM books b %s`, langWhereClause)
		db.QueryRow(qLanguages, args...).Scan(&s.TotalLanguages)
	} else {
		s.TotalLanguages = 1
	}
	
	return s
}

func (db *DB) GetGlobalStats() DBStats {
	s := DBStats{}
	q := `
		SELECT books_count, authors_count, series_count, genres_count, 
		(SELECT COUNT(*) FROM stats_main WHERE language != '-') 
		FROM stats_main WHERE language = '-'
	`
	db.QueryRow(q).Scan(&s.TotalBooks, &s.TotalAuthors, &s.TotalSeries, &s.TotalGenres, &s.TotalLanguages)
	return s
}

func (db *DB) GetLatestBooksCount(days int, blang string) int64 {	
	var c int64
	q := "SELECT count(*) FROM books WHERE updated > ?"
	args := []interface{}{db.sinceUnixNano(days)}
	if blang != "" {
		q += " AND language = ?"
		args = append(args, blang)
	}
	db.QueryRow(q, args...).Scan(&c)
	return c
}

func (db *DB) PageLatestBooks(days, limit, offset int, blang string) []*model.Book {
	q := `
	SELECT b.id, b.file, b.archive, b.size, b.format, b.title, b.year, b.plot, b.cover, b.language, b.keywords 
	FROM books as b
	WHERE b.updated > ? 
	`
	args := []interface{}{db.sinceUnixNano(days)}
	if blang != "" {
		q += " AND b.language = ? "
		args = append(args, blang)
	}
	q += " ORDER BY b.updated DESC"
	
	rows, err := db.pageQuery(q, limit, offset, args...)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	books := []*model.Book{}
	for rows.Next() {
		b := &model.Book{
			Language: &model.Language{},
		}
		if err := rows.Scan(&b.ID, &b.File, &b.Archive, &b.Size, &b.Format, &b.Title, &b.Year, &b.Plot, &b.Cover, &b.Language.Code, &b.Keywords); err != nil {
			log.Fatal(err)
		}
		books = append(books, b)
	}
	return books
}

const (
	SearchBookByTitleMode   = "title"
	SearchBookByKeywordMode = "keywords"
)

func (db *DB) SearchBooksCountByTitle(pattern string) int64 {
	return db.searchBooksCount(SearchBookByTitleMode, pattern)
}

func (db *DB) SearchBooksCountByKeyword(pattern string) int64 {
	return db.searchBooksCount(SearchBookByKeywordMode, pattern)
}

func (db *DB) searchBooksCount(mode, pattern string) int64 {
	ftsQuery := prepareFTSQuery(pattern)
	if ftsQuery == "" {
		return 0 
	}
	
	var c int64 = 0
	q := `SELECT count(*) as c FROM books_fts WHERE ` + mode + ` MATCH ?`
	err := db.QueryRow(q, ftsQuery).Scan(&c)
	if err == sql.ErrNoRows {
		return 0
	}
	return c
}

func (db *DB) PageFoundBooksByTitle(pattern string, limit, offset int) []*model.Book {
	return db.pageFoundBooks(SearchBookByTitleMode, pattern, limit, offset)
}

func (db *DB) PageFoundBooksByKeywords(pattern string, limit, offset int) []*model.Book {
	return db.pageFoundBooks(SearchBookByKeywordMode, pattern, limit, offset)
}

func (db *DB) pageFoundBooks(mode, pattern string, limit, offset int) []*model.Book {
	ftsQuery := prepareFTSQuery(pattern)
	if ftsQuery == "" {
		return []*model.Book{} 
	}

	foundIDs := func(mode, pattern string, limit, offset int) []string {
		q := `SELECT rowid 
			FROM books_fts 
			WHERE ` + mode + ` MATCH ? 
			ORDER BY rank 
			`
		rows, err := db.pageQuery(q, limit, offset, ftsQuery)
		if err != nil {
			log.Fatal(err)
		}
		defer rows.Close()
		foundIDs := []string{}
		for rows.Next() {
			var id int
			if err := rows.Scan(&id); err != nil {
				log.Fatal(err)
			}
			foundIDs = append(foundIDs, strconv.Itoa(id))
		}
		return foundIDs
	}(mode, ftsQuery, limit, offset)
	
	if len(foundIDs) == 0 {
		return []*model.Book{}
	}
	
	q := `
	SELECT b.id, b.file, b.archive, b.size, b.format, b.title, b.year, b.plot, b.cover, b.language, b.keywords 
	FROM books as b
	WHERE b.id IN (` + strings.Join(foundIDs, ",") + `) 
	`
	rows, err := db.Query(q)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	booksIdx := map[int64]*model.Book{}
	for rows.Next() {
		b := &model.Book{
			Language: &model.Language{},
		}
		if err := rows.Scan(&b.ID, &b.File, &b.Archive, &b.Size, &b.Format, &b.Title, &b.Year, &b.Plot, &b.Cover, &b.Language.Code, &b.Keywords); err != nil {
			log.Fatal(err)
		}
		booksIdx[b.ID] = b
	}

	books := []*model.Book{}
	for _, b := range foundIDs {
		i, _ := strconv.ParseInt(b, 10, 64)
		if book, ok := booksIdx[i]; ok {
			books = append(books, book)
		}
	}
	return books
}

var searchTokenizer = regexp.MustCompile(`\^?"[^"]+"|\S+`)

func prepareFTSQuery(pattern string) string {
	pattern = strings.TrimSpace(pattern)
	if len(pattern) == 0 {
		return ""
	}

	tokens := searchTokenizer.FindAllString(pattern, -1)
	var queryParts []string

	cleaner := strings.NewReplacer("'", "", "*", "", "(", "", ")", "", "-", " ", ":", " ")

	for _, token := range tokens {
		isAnchor := strings.HasPrefix(token, "^")
		if isAnchor {
			token = token[1:] 
		}

		isQuoted := strings.HasPrefix(token, `"`) && strings.HasSuffix(token, `"`)
		if isQuoted && len(token) >= 2 {
			token = token[1 : len(token)-1] 
		}

		cleanToken := strings.TrimSpace(cleaner.Replace(token))
		if cleanToken == "" {
			continue
		}

		part := fmt.Sprintf(`"%s"`, cleanToken)

		if !isQuoted {
			part += "*"
		}

		if isAnchor {
			part = "^" + part
		}

		queryParts = append(queryParts, part)
	}

	return strings.Join(queryParts, " ")
}

func (db *DB) SearchAuthorsCount(pattern string) int64 {
	ftsQuery := prepareFTSQuery(pattern)
	if ftsQuery == "" {
		return 0
	}
	
	var c int64 = 0
	q := `SELECT count(*) as c FROM authors_fts WHERE sort MATCH ?`
	err := db.QueryRow(q, ftsQuery).Scan(&c)
	if err == sql.ErrNoRows {
		return 0
	}
	return c
}

func (db *DB) PageFoundAuthors(pattern string, limit, offset int) []*model.Author {
	ftsQuery := prepareFTSQuery(pattern)
	if ftsQuery == "" {
		return []*model.Author{}
	}
	
	q := `
	SELECT a.id, a.name, a.sort, 
	       (SELECT COUNT(DISTINCT ba.book_id) FROM books_authors AS ba WHERE ba.author_id = a.id) AS count
	FROM authors AS a
	WHERE a.id IN (SELECT rowid FROM authors_fts WHERE sort MATCH ?)
	ORDER BY a.sort 
	`
	rows, err := db.pageQuery(q, limit, offset, ftsQuery)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	authors := []*model.Author{}

	for rows.Next() {
		a := &model.Author{}
		if err := rows.Scan(&a.ID, &a.Name, &a.Sort, &a.Count); err != nil {
			log.Fatal(err)
		}
		authors = append(authors, a)
	}
	return authors
}

func (db *DB) SearchSeriesCount(pattern string) int64 {
	ftsQuery := prepareFTSQuery(pattern)
	if ftsQuery == "" {
		return 0
	}
	
	var c int64 = 0
	q := `SELECT count(*) as c FROM series_fts WHERE sort MATCH ?`
	err := db.QueryRow(q, ftsQuery).Scan(&c)
	if err == sql.ErrNoRows {
		return 0
	}
	return c
}

func (db *DB) PageFoundSeries(pattern string, limit, offset int) []*model.Serie {
	ftsQuery := prepareFTSQuery(pattern)
	if ftsQuery == "" {
		return []*model.Serie{}
	}
	
	q := `
	SELECT s.id, s.name, 
		(SELECT COUNT(DISTINCT bs.book_id) FROM books_series AS bs WHERE bs.serie_id = s.id) AS count
	FROM series AS s
	WHERE s.id in (SELECT rowid FROM series_fts WHERE sort MATCH ?)
	ORDER BY s.sort
	`
	rows, err := db.pageQuery(q, limit, offset, ftsQuery)
	if err != nil {
		log.Println("PageFoundSeries error:", err)
		return []*model.Serie{}
	}
	defer rows.Close()
	series := []*model.Serie{}

	for rows.Next() {
		s := &model.Serie{}
		if err := rows.Scan(&s.ID, &s.Name, &s.Count); err != nil {
			log.Println(err)
		}
		series = append(series, s)
	}
	return series
}

func (db *DB) pageQuery(query string, limit, offset int, args ...interface{}) (*sql.Rows, error) {
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	if offset > 0 {
		query += " OFFSET ?"
		args = append(args, offset)
	}
	rows, err := db.Query(query, args...)
	return rows, err
}

func (db *DB) GetFolder(folderId int64) *model.Folder {
	if folderId == 0 {
		return &model.Folder{ID: 0, ParentID: 0, Name: "Root"} 
	}
	f := &model.Folder{}
	q := `SELECT id, parent_id, name FROM folders WHERE id=?`
	err := db.QueryRow(q, folderId).Scan(&f.ID, &f.ParentID, &f.Name)
	if err == sql.ErrNoRows {
		return nil
	}
	return f
}

func (db *DB) CountFolderItems(folderId int64) (int, int) {
	var fCount, bCount int
	db.QueryRow(`SELECT count(*) FROM folders WHERE parent_id=?`, folderId).Scan(&fCount)
	db.QueryRow(`SELECT count(*) FROM books WHERE folder_id=? AND updated > 0`, folderId).Scan(&bCount)
	return fCount, bCount
}

func (db *DB) PageFolders(folderId int64, limit, offset int) []*model.Folder {

	q := `
	SELECT f.id, f.parent_id, f.name, 
	       (SELECT COUNT(*) FROM books WHERE folder_id = f.id AND updated > 0) as book_count
	FROM folders f
	WHERE f.parent_id = ?
	ORDER BY f.sort ASC
	`
	rows, err := db.pageQuery(q, limit, offset, folderId)
	if err != nil {
		log.Println("PageFolders error:", err)
		return []*model.Folder{}
	}
	defer rows.Close()
	
	folders := []*model.Folder{}
	for rows.Next() {
		f := &model.Folder{}
		if err := rows.Scan(&f.ID, &f.ParentID, &f.Name, &f.Count); err == nil {
			folders = append(folders, f)
		}
	}
	return folders
}

func (db *DB) PageFolderBooks(folderId int64, limit, offset int) []*model.Book {
	q := `
	SELECT b.id, b.file, b.archive, b.size, b.format, b.title, b.year, b.plot, b.cover, b.language, b.keywords 
	FROM books as b
	WHERE b.folder_id=? AND b.updated > 0
	ORDER BY b.sort ASC
	`
	rows, err := db.pageQuery(q, limit, offset, folderId)
	if err != nil {
		log.Println("PageFolderBooks error:", err)
		return []*model.Book{}
	}
	defer rows.Close()

	books := []*model.Book{}
	for rows.Next() {
		b := &model.Book{Language: &model.Language{}}
		if err := rows.Scan(&b.ID, &b.File, &b.Archive, &b.Size, &b.Format, &b.Title, &b.Year, &b.Plot, &b.Cover, &b.Language.Code, &b.Keywords); err == nil {
			books = append(books, b)
		}
	}
	return books
}

func (db *DB) GetRandomLatestBook(days int, blang string) *model.Book {
	var count int
	var err error

	var conds []string
	var args []interface{}

	conds = append(conds, "b.updated > ?")
	args = append(args, db.sinceUnixNano(days))

	if blang != "" {
		conds = append(conds, "b.language = ?")
		args = append(args, blang)
	}

	whereClause := strings.Join(conds, " AND ")

	countQuery := fmt.Sprintf(`SELECT COUNT(b.id) FROM books b WHERE %s`, whereClause)
	err = db.QueryRow(countQuery, args...).Scan(&count)
	if err != nil || count <= 0 {
		return nil
	}

	offset := rand.Intn(count)

	qArgs := append(append([]interface{}{}, args...), offset)
	q := fmt.Sprintf(`
		SELECT b.id, b.file, b.archive, b.size, b.format, b.title, b.year, b.plot, b.cover, b.language, b.keywords 
		FROM books as b 
		WHERE %s 
		LIMIT 1 OFFSET ?
	`, whereClause)
	
	b := &model.Book{Language: &model.Language{}}
	err = db.QueryRow(q, qArgs...).Scan(
		&b.ID, &b.File, &b.Archive, &b.Size, &b.Format, &b.Title, &b.Year, &b.Plot, &b.Cover, &b.Language.Code, &b.Keywords,
	)
	
	if err != nil && err != sql.ErrNoRows {
		log.Println("GetRandomLatestBook error:", err)
		return nil
	}
	return b
}

func (db *DB) GetRandomBookByBunch(bunch string, codes []string, days int, blang string) *model.Book {
	if len(codes) == 0 {
		return nil
	}

	placeholders := make([]string, len(codes))
	var codesArgs []interface{}
	for i, code := range codes {
		placeholders[i] = "?"
		codesArgs = append(codesArgs, code)
	}
	inClause := strings.Join(placeholders, ",")

	var count int
	var err error

	if days <= 0 {
		langKey := "-"
		if blang != "" {
			langKey = blang
		}

		err = db.QueryRow(`SELECT count FROM stats_genres WHERE language = ? AND bunch = ?`, langKey, bunch).Scan(&count)
		
		if err != nil || count <= 0 {
			return nil
		}

		offset := rand.Intn(count)
		var q string
		var qArgs []interface{}

		if blang == "" {
			q = fmt.Sprintf(`
				SELECT b.id, b.file, b.archive, b.size, b.format, b.title, b.year, b.plot, b.cover, b.language, b.keywords 
				FROM (
					SELECT DISTINCT book_id 
					FROM books_genres 
					WHERE genre_code IN (%s) 
					LIMIT 1 OFFSET ?
				) AS t
				JOIN books b ON b.id = t.book_id
			`, inClause)
			qArgs = append(append([]interface{}{}, codesArgs...), offset)
		} else {
			q = fmt.Sprintf(`
				SELECT b.id, b.file, b.archive, b.size, b.format, b.title, b.year, b.plot, b.cover, b.language, b.keywords 
				FROM (
					SELECT DISTINCT book_id 
					FROM books_genres 
					WHERE genre_code IN (%s)
				) bg
				JOIN books b ON b.id = bg.book_id
				WHERE b.language = ? AND b.updated > 0
				LIMIT 1 OFFSET ?
			`, inClause)
			qArgs = append(append([]interface{}{}, codesArgs...), blang, offset)
		}

		b := &model.Book{Language: &model.Language{}}
		err = db.QueryRow(q, qArgs...).Scan(
			&b.ID, &b.File, &b.Archive, &b.Size, &b.Format, &b.Title, &b.Year, &b.Plot, &b.Cover, &b.Language.Code, &b.Keywords,
		)
		if err != nil && err != sql.ErrNoRows {
			log.Println("GetRandomBookByBunch error:", err)
		}
		return b
	}

	var bookConds []string
	var args []interface{}

	bookConds = append(bookConds, "b.updated > ?")
	args = append(args, db.sinceUnixNano(days))

	if blang != "" {
		bookConds = append(bookConds, "b.language = ?")
		args = append(args, blang)
	}

	bookConds = append(bookConds, fmt.Sprintf("EXISTS (SELECT 1 FROM books_genres bg WHERE bg.book_id = b.id AND bg.genre_code IN (%s))", inClause))

	whereClause := strings.Join(bookConds, " AND ")

	countArgs := append(append([]interface{}{}, args...), codesArgs...)
	countQuery := fmt.Sprintf(`SELECT COUNT(b.id) FROM books b WHERE %s`, whereClause)

	err = db.QueryRow(countQuery, countArgs...).Scan(&count)
	if err != nil || count <= 0 {
		return nil
	}

	offset := rand.Intn(count)

	qArgs := append(append([]interface{}{}, args...), codesArgs...)
	qArgs = append(qArgs, offset)

	q := fmt.Sprintf(`
		SELECT b.id, b.file, b.archive, b.size, b.format, b.title, b.year, b.plot, b.cover, b.language, b.keywords 
		FROM books b
		WHERE %s
		LIMIT 1 OFFSET ?
	`, whereClause)

	b := &model.Book{Language: &model.Language{}}
	err = db.QueryRow(q, qArgs...).Scan(
		&b.ID, &b.File, &b.Archive, &b.Size, &b.Format, &b.Title, &b.Year, &b.Plot, &b.Cover, &b.Language.Code, &b.Keywords,
	)
	if err != nil && err != sql.ErrNoRows {
		log.Println("GetRandomBookByBunch error:", err)
	}
	return b
}

func (db *DB) GetRandomBookByGenre(genreCode string, days int, blang string) *model.Book {
	var count int
	var err error

	if days <= 0 {
		langKey := "-"
		if blang != "" {
			langKey = blang
		}

		err = db.QueryRow(`SELECT count FROM stats_subgenres WHERE language = ? AND genre_code = ?`, langKey, genreCode).Scan(&count)
		if err != nil || count <= 0 {
			return nil
		}

		offset := rand.Intn(count)
		var q string
		var qArgs []interface{}

		if blang == "" {

			q = `
				SELECT b.id, b.file, b.archive, b.size, b.format, b.title, b.year, b.plot, b.cover, b.language, b.keywords 
				FROM (
					SELECT book_id 
					FROM books_genres 
					WHERE genre_code = ? 
					LIMIT 1 OFFSET ?
				) AS t
				JOIN books b ON b.id = t.book_id
			`
			qArgs = []interface{}{genreCode, offset}
		} else {

			q = `
				SELECT b.id, b.file, b.archive, b.size, b.format, b.title, b.year, b.plot, b.cover, b.language, b.keywords 
				FROM books_genres bg
				JOIN books b ON b.id = bg.book_id
				WHERE bg.genre_code = ? AND b.language = ? AND b.updated > 0
				LIMIT 1 OFFSET ?
			`
			qArgs = []interface{}{genreCode, blang, offset}
		}

		b := &model.Book{Language: &model.Language{}}
		err = db.QueryRow(q, qArgs...).Scan(
			&b.ID, &b.File, &b.Archive, &b.Size, &b.Format, &b.Title, &b.Year, &b.Plot, &b.Cover, &b.Language.Code, &b.Keywords,
		)
		if err != nil && err != sql.ErrNoRows {
			log.Println("GetRandomBookByGenre error:", err)
		}
		return b
	}

	var bookConds []string
	var args []interface{}

	bookConds = append(bookConds, "b.updated > ?")
	args = append(args, db.sinceUnixNano(days))

	if blang != "" {
		bookConds = append(bookConds, "b.language = ?")
		args = append(args, blang)
	}

	bookConds = append(bookConds, "EXISTS (SELECT 1 FROM books_genres bg WHERE bg.book_id = b.id AND bg.genre_code = ?)")
	whereClause := strings.Join(bookConds, " AND ")

	countArgs := append(append([]interface{}{}, args...), genreCode)
	countQuery := fmt.Sprintf(`SELECT COUNT(b.id) FROM books b WHERE %s`, whereClause)

	err = db.QueryRow(countQuery, countArgs...).Scan(&count)
	if err != nil || count <= 0 {
		return nil
	}

	offset := rand.Intn(count)

	qArgs := append(append([]interface{}{}, args...), genreCode)
	qArgs = append(qArgs, offset)

	q := fmt.Sprintf(`
		SELECT b.id, b.file, b.archive, b.size, b.format, b.title, b.year, b.plot, b.cover, b.language, b.keywords 
		FROM books b
		WHERE %s
		LIMIT 1 OFFSET ?
	`, whereClause)

	b := &model.Book{Language: &model.Language{}}
	err = db.QueryRow(q, qArgs...).Scan(
		&b.ID, &b.File, &b.Archive, &b.Size, &b.Format, &b.Title, &b.Year, &b.Plot, &b.Cover, &b.Language.Code, &b.Keywords,
	)
	if err != nil && err != sql.ErrNoRows {
		log.Println("GetRandomBookByGenre error:", err)
	}
	return b
}

func (db *DB) PageLanguageBooks(blang string, limit, offset int) []*model.Book {
	q := `
	SELECT b.id, b.file, b.archive, b.size, b.format, b.title, b.year, b.plot, b.cover, b.language, b.keywords 
	FROM books as b
	WHERE b.language = ? AND b.updated > 0
	ORDER BY b.sort ASC
	`
	rows, err := db.pageQuery(q, limit, offset, blang)
	if err != nil {
		log.Println("PageLanguageBooks error:", err)
		return []*model.Book{}
	}
	defer rows.Close()

	books := []*model.Book{}
	for rows.Next() {
		b := &model.Book{Language: &model.Language{}}
		if err := rows.Scan(&b.ID, &b.File, &b.Archive, &b.Size, &b.Format, &b.Title, &b.Year, &b.Plot, &b.Cover, &b.Language.Code, &b.Keywords); err == nil {
			books = append(books, b)
		}
	}
	return books
}