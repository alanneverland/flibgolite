CREATE TABLE IF NOT EXISTS stats_main (
    language TEXT PRIMARY KEY,
    books_count INTEGER DEFAULT 0,
    authors_count INTEGER DEFAULT 0,
    series_count INTEGER DEFAULT 0,
	genres_count INTEGER DEFAULT 0,
    language_count INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS stats_genres (
    language TEXT,
    bunch TEXT,
    count INTEGER DEFAULT 0,
    PRIMARY KEY (language, bunch)
);

CREATE TABLE IF NOT EXISTS stats_subgenres (
    language TEXT NOT NULL,
    genre_code TEXT NOT NULL,
    count INTEGER DEFAULT 0,
    PRIMARY KEY (language, genre_code)
);

DROP TABLE IF EXISTS folders;
CREATE TABLE folders (
    id INTEGER PRIMARY KEY,
    parent_id INTEGER,
    name TEXT,
	sort TEXT
);
CREATE INDEX folders_parent_sort_idx ON folders (parent_id, sort);

DROP TABLE IF EXISTS authors;
CREATE TABLE authors (
    id INTEGER PRIMARY KEY,
    name TEXT,
    sort TEXT
);
CREATE UNIQUE INDEX authors_name_idx ON authors (name);
CREATE INDEX authors_sort_idx ON authors (sort);
CREATE INDEX authors_first_letter_idx ON authors(SUBSTR(sort, 1, 1));

DROP TABLE IF EXISTS authors_fts;
CREATE VIRTUAL TABLE authors_fts USING fts5(sort, content='', tokenize='unicode61 remove_diacritics 2');

DROP TABLE IF EXISTS books;
CREATE TABLE books (
    id INTEGER PRIMARY KEY,
	seen INTEGER DEFAULT 1,
	folder_id INTEGER,
    file TEXT,
    archive TEXT,
    size INTEGER,
    format TEXT,
    title TEXT,
    sort TEXT,
    year TEXT,
    language TEXT,   
    plot TEXT,
    cover TEXT,
    keywords TEXT,   
    updated INTEGER	
);
CREATE INDEX books_file_idx ON books (file);
CREATE INDEX books_archive_idx ON books (archive);
CREATE INDEX books_title_idx ON books (title);
CREATE INDEX books_sort_idx ON books (sort);
CREATE INDEX books_language_idx ON books (language); 
CREATE INDEX books_language_updated_idx ON books (language, updated); 
CREATE INDEX books_language_active_sort_idx ON books (language, sort) WHERE updated > 0;
CREATE INDEX books_updated_idx ON books (updated);
CREATE INDEX books_folder_active_sort_idx ON books (folder_id, sort) WHERE updated > 0;

DROP TABLE IF EXISTS books_fts;
CREATE VIRTUAL TABLE books_fts USING fts5(title, keywords, content='', tokenize='unicode61 remove_diacritics 2');

DROP TABLE IF EXISTS series;
CREATE TABLE series (
    id INTEGER PRIMARY KEY,
    name TEXT,
    sort TEXT
);
CREATE UNIQUE INDEX series_name_idx ON series (name);
CREATE INDEX series_sort_idx ON series (sort);
CREATE INDEX series_first_letter_idx ON series(SUBSTR(sort, 1, 1));

DROP TABLE IF EXISTS series_fts;
CREATE VIRTUAL TABLE series_fts USING fts5(sort, content='', tokenize='unicode61 remove_diacritics 2');

DROP TABLE IF EXISTS books_series;
CREATE TABLE books_series (
    id INTEGER PRIMARY KEY,
    book_id INTEGER,
    serie_id INTEGER,
    serie_num INTEGER,
    language TEXT
);
CREATE INDEX books_series_book_num_serie_idx ON books_series (book_id, serie_num, serie_id);
CREATE INDEX books_series_lang_serie_idx ON books_series (language, serie_id);
CREATE INDEX books_series_serie_lang_num_book_idx ON books_series (serie_id, language, serie_num, book_id);

DROP TABLE IF EXISTS books_authors;
CREATE TABLE books_authors (
    id INTEGER PRIMARY KEY,
    book_id INTEGER,
    author_id INTEGER,
    language TEXT
);
CREATE INDEX books_authors_book_author_idx ON books_authors(book_id, author_id);
CREATE INDEX books_authors_lang_auth_idx ON books_authors(language, author_id);
CREATE INDEX books_authors_auth_lang_idx ON books_authors(author_id, language, book_id);

DROP TABLE IF EXISTS books_genres;
CREATE TABLE books_genres (
    id INTEGER PRIMARY KEY,
    book_id INTEGER,
    genre_code TEXT
);
CREATE INDEX books_genres_book_genre_idx ON books_genres (book_id, genre_code);
CREATE INDEX books_genres_genre_book_idx ON books_genres (genre_code, book_id);

CREATE TRIGGER IF NOT EXISTS books_after_delete AFTER DELETE ON books 
BEGIN
    INSERT INTO books_fts(books_fts, rowid, title, keywords) 
    VALUES ('delete', old.id, old.title, old.keywords);
END;

CREATE TRIGGER IF NOT EXISTS authors_after_delete AFTER DELETE ON authors 
BEGIN
    INSERT INTO authors_fts(authors_fts, rowid, sort) 
    VALUES ('delete', old.id, old.sort);
END;

CREATE TRIGGER IF NOT EXISTS series_after_delete AFTER DELETE ON series 
BEGIN
    INSERT INTO series_fts(series_fts, rowid, sort) 
    VALUES ('delete', old.id, old.sort);
END;

