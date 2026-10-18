-- 000001_init_schema.up.sql
-- 初始 23 张表：22 张普通表 + 1 张 FTS5 虚拟表（posts_fts），以及 FTS 同步触发器。
-- 全局约定（agent.md §4）：时间为 Unix 秒（UTC）；布尔为 INTEGER 0/1；JSON 以 TEXT 保存合法 JSON 字符串。

CREATE TABLE admins (
    id             INTEGER PRIMARY KEY,
    username       TEXT NOT NULL,
    password_hash  TEXT NOT NULL,
    email          TEXT NOT NULL DEFAULT '',
    last_login_at  INTEGER NOT NULL DEFAULT 0,
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_admins_username ON admins (username);

CREATE TABLE sessions (
    token_hash   TEXT PRIMARY KEY,
    admin_id     INTEGER NOT NULL,
    csrf_hash    TEXT NOT NULL DEFAULT '',
    ip_hash      TEXT NOT NULL DEFAULT '',
    ua           TEXT NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL
);
CREATE INDEX idx_sessions_admin_id ON sessions (admin_id);
CREATE INDEX idx_sessions_expires_at ON sessions (expires_at);

CREATE TABLE login_attempts (
    id           INTEGER PRIMARY KEY,
    key          TEXT NOT NULL,
    kind         TEXT NOT NULL,
    count        INTEGER NOT NULL DEFAULT 0,
    window_start INTEGER NOT NULL DEFAULT 0,
    locked_until INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX idx_login_attempts_key_kind ON login_attempts (key, kind);

CREATE TABLE posts (
    id               INTEGER PRIMARY KEY,
    slug             TEXT NOT NULL,
    title            TEXT NOT NULL,
    excerpt          TEXT NOT NULL DEFAULT '',
    content_md       TEXT NOT NULL DEFAULT '',
    content_html     TEXT NOT NULL DEFAULT '',
    content_text     TEXT NOT NULL DEFAULT '',
    cover_media_id   INTEGER,
    status           TEXT NOT NULL DEFAULT 'draft',
    published_at     INTEGER,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL,
    deleted_at       INTEGER,
    word_count       INTEGER NOT NULL DEFAULT 0,
    reading_time     INTEGER NOT NULL DEFAULT 0,
    is_pinned        INTEGER NOT NULL DEFAULT 0,
    allow_comments   INTEGER NOT NULL DEFAULT 1,
    series_id        INTEGER,
    series_order     INTEGER NOT NULL DEFAULT 0,
    seo_title        TEXT NOT NULL DEFAULT '',
    seo_description  TEXT NOT NULL DEFAULT '',
    canonical_url    TEXT NOT NULL DEFAULT '',
    og_media_id      INTEGER,
    view_count       INTEGER NOT NULL DEFAULT 0,
    like_count       INTEGER NOT NULL DEFAULT 0,
    comment_count    INTEGER NOT NULL DEFAULT 0,
    revision         INTEGER NOT NULL DEFAULT 1,
    lang             TEXT NOT NULL DEFAULT 'zh-CN'
);
CREATE UNIQUE INDEX idx_posts_slug ON posts (slug);
CREATE INDEX idx_posts_status_published_at ON posts (status, published_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_posts_series ON posts (series_id, series_order);

CREATE TABLE post_revisions (
    id          INTEGER PRIMARY KEY,
    post_id     INTEGER NOT NULL,
    revision_no INTEGER NOT NULL,
    title       TEXT NOT NULL,
    content_md  TEXT NOT NULL DEFAULT '',
    content_html TEXT NOT NULL DEFAULT '',
    note        TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_post_revisions_post_no ON post_revisions (post_id, revision_no);

CREATE TABLE tags (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_tags_name ON tags (name);
CREATE UNIQUE INDEX idx_tags_slug ON tags (slug);

CREATE TABLE post_tags (
    post_id INTEGER NOT NULL,
    tag_id  INTEGER NOT NULL,
    PRIMARY KEY (post_id, tag_id)
);
CREATE INDEX idx_post_tags_tag_id ON post_tags (tag_id);

CREATE TABLE series (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_series_slug ON series (slug);

CREATE TABLE preview_tokens (
    token_hash TEXT PRIMARY KEY,
    post_id    INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    used_at    INTEGER,
    created_at INTEGER NOT NULL
);

CREATE TABLE redirects (
    id         INTEGER PRIMARY KEY,
    from_path  TEXT NOT NULL,
    to_path    TEXT NOT NULL,
    status     INTEGER NOT NULL DEFAULT 301 CHECK (status IN (301, 302)),
    hits       INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_redirects_from_path ON redirects (from_path);

CREATE TABLE media (
    id               INTEGER PRIMARY KEY,
    sha256           TEXT NOT NULL,
    storage_driver   TEXT NOT NULL DEFAULT 'local',
    storage_key      TEXT NOT NULL,
    original_name    TEXT NOT NULL DEFAULT '',
    mime             TEXT NOT NULL DEFAULT '',
    ext              TEXT NOT NULL DEFAULT '',
    bytes            INTEGER NOT NULL DEFAULT 0,
    width            INTEGER NOT NULL DEFAULT 0,
    height           INTEGER NOT NULL DEFAULT 0,
    pixels           INTEGER NOT NULL DEFAULT 0,
    variants_json    TEXT NOT NULL DEFAULT '{}',
    blurhash         TEXT NOT NULL DEFAULT '',
    alt              TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'processing',
    error            TEXT NOT NULL DEFAULT '',
    orig_archived    INTEGER NOT NULL DEFAULT 0,
    orig_archived_at INTEGER,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_media_sha256 ON media (sha256);
CREATE INDEX idx_media_status ON media (status);

CREATE TABLE post_attachments (
    id         INTEGER PRIMARY KEY,
    post_id    INTEGER NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    share_url  TEXT NOT NULL DEFAULT '',
    file_name  TEXT NOT NULL DEFAULT '',
    bytes      INTEGER NOT NULL DEFAULT 0,
    sha256     TEXT NOT NULL DEFAULT '',
    version    TEXT NOT NULL DEFAULT '',
    sort       INTEGER NOT NULL DEFAULT 0,
    clicks     INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);
CREATE INDEX idx_post_attachments_post_id ON post_attachments (post_id);

CREATE TABLE comments (
    id           INTEGER PRIMARY KEY,
    post_id      INTEGER NOT NULL,
    parent_id    INTEGER,
    root_id      INTEGER,
    author_name  TEXT NOT NULL,
    author_email TEXT NOT NULL DEFAULT '',
    email_hash   TEXT NOT NULL DEFAULT '',
    author_url   TEXT NOT NULL DEFAULT '',
    content_md   TEXT NOT NULL DEFAULT '',
    content_html TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'pending',
    ip_hash      TEXT NOT NULL DEFAULT '',
    ua           TEXT NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE INDEX idx_comments_post_status_created ON comments (post_id, status, created_at);
CREATE INDEX idx_comments_status_created ON comments (status, created_at);

CREATE TABLE likes (
    post_id      INTEGER NOT NULL,
    visitor_hash TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    PRIMARY KEY (post_id, visitor_hash)
);

CREATE TABLE page_views (
    id            INTEGER PRIMARY KEY,
    post_id       INTEGER,
    path          TEXT NOT NULL,
    visitor_hash  TEXT NOT NULL DEFAULT '',
    referrer_host TEXT NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL
);
CREATE INDEX idx_page_views_post_created ON page_views (post_id, created_at);

-- date 为 Asia/Shanghai 的 YYYY-MM-DD；post_id = 0 表示全站聚合行（PK 不允许 NULL）。
CREATE TABLE stats_daily (
    date    TEXT NOT NULL,
    post_id INTEGER NOT NULL DEFAULT 0,
    pv      INTEGER NOT NULL DEFAULT 0,
    uv      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (date, post_id)
);

CREATE TABLE jobs (
    id           INTEGER PRIMARY KEY,
    type         TEXT NOT NULL,
    payload      TEXT NOT NULL DEFAULT '{}',
    run_at       INTEGER NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending',
    attempts     INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    last_error   TEXT NOT NULL DEFAULT '',
    locked_at    INTEGER,
    created_at   INTEGER NOT NULL
);
CREATE INDEX idx_jobs_status_run_at ON jobs (status, run_at);

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value_json TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE links (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL,
    url         TEXT NOT NULL,
    avatar      TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    sort        INTEGER NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'active',
    created_at  INTEGER NOT NULL
);

CREATE TABLE audit_logs (
    id          INTEGER PRIMARY KEY,
    admin_id    INTEGER NOT NULL DEFAULT 0,
    action      TEXT NOT NULL,
    target_type TEXT NOT NULL DEFAULT '',
    target_id   TEXT NOT NULL DEFAULT '',
    meta_json   TEXT NOT NULL DEFAULT '{}',
    ip_hash     TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL
);
CREATE INDEX idx_audit_logs_created_at ON audit_logs (created_at);
CREATE INDEX idx_audit_logs_target ON audit_logs (target_type, target_id);

CREATE TABLE pages (
    id              INTEGER PRIMARY KEY,
    slug            TEXT NOT NULL,
    title           TEXT NOT NULL,
    content_md      TEXT NOT NULL DEFAULT '',
    content_html     TEXT NOT NULL DEFAULT '',
    content_text    TEXT NOT NULL DEFAULT '',
    seo_title       TEXT NOT NULL DEFAULT '',
    seo_description TEXT NOT NULL DEFAULT '',
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    deleted_at      INTEGER
);
CREATE UNIQUE INDEX idx_pages_slug ON pages (slug);

CREATE TABLE link_checks (
    id          INTEGER PRIMARY KEY,
    post_id     INTEGER NOT NULL,
    url         TEXT NOT NULL,
    kind        TEXT NOT NULL DEFAULT 'link',
    status_code INTEGER NOT NULL DEFAULT 0,
    ok          INTEGER NOT NULL DEFAULT 0,
    error       TEXT NOT NULL DEFAULT '',
    checked_at  INTEGER NOT NULL
);
CREATE INDEX idx_link_checks_post_id ON link_checks (post_id);
CREATE INDEX idx_link_checks_checked_at ON link_checks (checked_at);

-- FTS5：外部内容表 posts_fts 与 posts 同步；trigram 分词用于中文（≥3 字），
-- 1–2 字关键词按 §7.1 V1 走 content_text LIKE 兜底（D6 实现）。
CREATE VIRTUAL TABLE posts_fts USING fts5(
    title,
    excerpt,
    content_text,
    content='posts',
    content_rowid='id',
    tokenize='trigram'
);

CREATE TRIGGER posts_fts_ai AFTER INSERT ON posts BEGIN
    INSERT INTO posts_fts (rowid, title, excerpt, content_text)
    VALUES (new.id, new.title, new.excerpt, new.content_text);
END;

CREATE TRIGGER posts_fts_ad AFTER DELETE ON posts BEGIN
    INSERT INTO posts_fts (posts_fts, rowid, title, excerpt, content_text)
    VALUES ('delete', old.id, old.title, old.excerpt, old.content_text);
END;

CREATE TRIGGER posts_fts_au AFTER UPDATE ON posts BEGIN
    INSERT INTO posts_fts (posts_fts, rowid, title, excerpt, content_text)
    VALUES ('delete', old.id, old.title, old.excerpt, old.content_text);
    INSERT INTO posts_fts (rowid, title, excerpt, content_text)
    VALUES (new.id, new.title, new.excerpt, new.content_text);
END;
