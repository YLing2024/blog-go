// Package collections 是合集领域：公开列表/详情与管理端增删改查。
//
// 合集有独立雪花 public_id（URL 用它），slug 退化为内部别名；删除合集时显式把文章
// collection_id 置空（外键 ON DELETE SET NULL 兜底，双保险）。
package collections

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"blog-go/internal/posts"
	"blog-go/internal/snowflake"
	"blog-go/internal/store"
)

// 领域错误：文案即对外响应体里的 message。
var (
	// ErrNameEmpty 新建/更新合集名称为空。
	ErrNameEmpty = errors.New("合集名称不能为空")
	// ErrCollectionNotFound 合集不存在。
	ErrCollectionNotFound = errors.New("合集不存在")
)

// Collection 是合集基本形态（管理端返回原始行）。
type Collection struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	PublicID    *string `json:"public_id"`
	Description string  `json:"description"`
	CreatedAt   string  `json:"created_at"`
}

// WithCount 是列表形态，多一个 post_count（字段顺序与旧实现一致）。
type WithCount struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	PublicID    *string `json:"public_id"`
	Description string  `json:"description"`
	CreatedAt   string  `json:"created_at"`
	PostCount   int64   `json:"post_count"`
}

// Detail 是合集详情形态（description 后追加 posts）。
type Detail struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	Slug        string          `json:"slug"`
	PublicID    *string         `json:"public_id"`
	Description string          `json:"description"`
	CreatedAt   string          `json:"created_at"`
	Posts       []posts.Summary `json:"posts"`
}

// Repo 是合集仓储；Posts 用于取合集下的文章。
type Repo struct {
	DB    *store.DB
	Gen   *snowflake.Generator
	Posts *posts.Repo
}

// New 构造合集仓储。
func New(db *store.DB, gen *snowflake.Generator, postsRepo *posts.Repo) *Repo {
	return &Repo{DB: db, Gen: gen, Posts: postsRepo}
}

// Input 是新建/更新合集的入参；Update 用指针区分「未提供」。
type Input struct {
	Name        *string
	Slug        *string
	Description *string
}

// ListPublic 公开合集列表（含已发布文章数），按创建时间升序。
func (r *Repo) ListPublic(ctx context.Context) ([]WithCount, error) {
	return r.list(ctx, true)
}

// ListAdmin 管理端合集列表（含全部文章数）。
func (r *Repo) ListAdmin(ctx context.Context) ([]WithCount, error) {
	return r.list(ctx, false)
}

func (r *Repo) list(ctx context.Context, onlyPublished bool) ([]WithCount, error) {
	join := "LEFT JOIN posts p ON p.collection_id = c.id"
	if onlyPublished {
		join += " AND p.published = 1"
	}
	query := "SELECT c.id, c.name, c.slug, c.public_id, c.description, c.created_at, COUNT(p.id) AS post_count " +
		"FROM collections c " + join + " GROUP BY c.id, c.name, c.slug, c.public_id, c.description, c.created_at " +
		"ORDER BY c.created_at ASC, c.id ASC"
	rows, err := r.DB.SQL.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]WithCount, 0)
	for rows.Next() {
		var (
			c        WithCount
			publicID sql.NullString
		)
		if err := rows.Scan(&c.ID, &c.Name, &c.Slug, &publicID, &c.Description, &c.CreatedAt, &c.PostCount); err != nil {
			return nil, err
		}
		c.PublicID = nullString(publicID)
		list = append(list, c)
	}
	return list, rows.Err()
}

// GetPublic 公开合集详情：:key 可为 public_id 或历史 slug；并附带已发布文章列表。
func (r *Repo) GetPublic(ctx context.Context, key string) (*Detail, error) {
	collection, err := r.findByKey(ctx, key)
	if err != nil || collection == nil {
		return nil, err
	}
	list, err := r.Posts.ListByCollection(ctx, collection.ID)
	if err != nil {
		return nil, err
	}
	return &Detail{
		ID:          collection.ID,
		Name:        collection.Name,
		Slug:        collection.Slug,
		PublicID:    collection.PublicID,
		Description: collection.Description,
		CreatedAt:   collection.CreatedAt,
		Posts:       list,
	}, nil
}

// Create 新建合集：名称必填，slug 冲突自动加序号，public_id 现签。
func (r *Repo) Create(ctx context.Context, in Input) (*Collection, error) {
	name := ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if name == "" {
		return nil, ErrNameEmpty
	}
	var created *Collection
	err := r.DB.Tx(ctx, func(q store.Querier) error {
		rawSlug := ""
		if in.Slug != nil {
			rawSlug = *in.Slug
		}
		slugValue, err := store.UniqueSlug(ctx, q, "collections",
			store.Slugify(firstNonEmpty(rawSlug, name), "collection"), 0)
		if err != nil {
			return err
		}
		description := ""
		if in.Description != nil {
			description = strings.TrimSpace(*in.Description)
		}
		id, err := store.InsertReturningID(ctx, q,
			"INSERT INTO collections (name, slug, public_id, description, created_at) VALUES (?, ?, ?, ?, ?)",
			name, slugValue, r.Gen.Next(), description, store.NowText())
		if err != nil {
			return err
		}
		row, err := findRow(ctx, q, id)
		if err != nil {
			return err
		}
		created = row
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// Update 更新合集：仅在请求体提供字段的范围内改动。
func (r *Repo) Update(ctx context.Context, id int64, in Input) (*Collection, error) {
	var updated *Collection
	err := r.DB.Tx(ctx, func(q store.Querier) error {
		current, err := findRow(ctx, q, id)
		if err != nil {
			return err
		}
		if current == nil {
			return ErrCollectionNotFound
		}
		name := current.Name
		if in.Name != nil {
			name = strings.TrimSpace(*in.Name)
		}
		description := current.Description
		if in.Description != nil {
			description = strings.TrimSpace(*in.Description)
		}
		base := current.Slug
		if in.Slug != nil && strings.TrimSpace(*in.Slug) != "" {
			base = store.Slugify(*in.Slug, "collection")
		} else if base == "" {
			base = store.Slugify(firstNonEmpty(name, current.Name), "collection")
		}
		slugValue, err := store.UniqueSlug(ctx, q, "collections", base, id)
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx,
			"UPDATE collections SET name = ?, slug = ?, description = ? WHERE id = ?",
			name, slugValue, description, id); err != nil {
			return err
		}
		row, err := findRow(ctx, q, id)
		if err != nil {
			return err
		}
		updated = row
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// Delete 删除合集：先显式解除文章关联，再删行；返回是否命中。
func (r *Repo) Delete(ctx context.Context, id int64) (bool, error) {
	deleted := false
	err := r.DB.Tx(ctx, func(q store.Querier) error {
		exists, err := findRow(ctx, q, id)
		if err != nil {
			return err
		}
		if exists == nil {
			return nil
		}
		if _, err := q.ExecContext(ctx, "UPDATE posts SET collection_id = NULL WHERE collection_id = ?", id); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, "DELETE FROM collections WHERE id = ?", id); err != nil {
			return err
		}
		deleted = true
		return nil
	})
	return deleted, err
}

// findByKey 按 public_id 优先后 slug 查合集；无则 nil。
func (r *Repo) findByKey(ctx context.Context, key string) (*Collection, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, nil
	}
	row := "SELECT id, name, slug, public_id, description, created_at FROM collections "
	byPublicID, ok, err := scanRow(r.DB.SQL.QueryRowContext(ctx, row+"WHERE public_id = ?", key))
	if err != nil {
		return nil, err
	}
	if ok {
		return &byPublicID, nil
	}
	bySlug, ok, err := scanRow(r.DB.SQL.QueryRowContext(ctx, row+"WHERE slug = ?", key))
	if err != nil {
		return nil, err
	}
	if ok {
		return &bySlug, nil
	}
	return nil, nil
}

// findRow 按数字 id 取合集；无则 nil。
func findRow(ctx context.Context, q store.Querier, id int64) (*Collection, error) {
	row := "SELECT id, name, slug, public_id, description, created_at FROM collections WHERE id = ?"
	c, ok, err := scanRow(q.QueryRowContext(ctx, row, id))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return &c, nil
}

// scanRow 扫描单行合集；sql.ErrNoRows 视为「未命中」。
func scanRow(s interface {
	Scan(dest ...any) error
}) (Collection, bool, error) {
	var (
		c        Collection
		publicID sql.NullString
	)
	err := s.Scan(&c.ID, &c.Name, &c.Slug, &publicID, &c.Description, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return Collection{}, false, nil
	}
	if err != nil {
		return Collection{}, false, err
	}
	c.PublicID = nullString(publicID)
	return c, true, nil
}

// nullString 把可空文本列转成 *string。
func nullString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
