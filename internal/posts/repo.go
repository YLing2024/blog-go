package posts

import (
	"context"
	"database/sql"
	"strings"

	"blog-go/internal/snowflake"
	"blog-go/internal/store"
)

// pageSize 是公开列表每页条数（旧实现 PAGE_SIZE）。
const pageSize = 10

// 查询列：与旧实现 POST_COLS / POST_COLS_NO_CONTENT 逐字对齐（顺序决定 JSON 字段顺序）。
const (
	postCols = "p.id, p.title, p.subtitle, p.slug, p.public_id, p.content, p.excerpt, p.tags, p.published, " +
		"p.created_at, p.updated_at, c.id AS collection_id, c.name AS collection_name, c.slug AS collection_slug, c.public_id AS collection_public_id"
	postColsNoContent = "p.id, p.title, p.subtitle, p.slug, p.public_id, p.excerpt, p.tags, p.published, " +
		"p.created_at, p.updated_at, c.id AS collection_id, c.name AS collection_name, c.slug AS collection_slug, c.public_id AS collection_public_id"
	postJoin = " FROM posts p LEFT JOIN collections c ON c.id = p.collection_id"
)

// Repo 是文章仓储。
type Repo struct {
	DB  *store.DB
	Gen *snowflake.Generator
}

// New 构造文章仓储。
func New(db *store.DB, gen *snowflake.Generator) *Repo {
	return &Repo{DB: db, Gen: gen}
}

// CreateInput 是新建文章的入参（字段缺失语义已由 httpapi 归一）。
type CreateInput struct {
	Title      string
	Slug       string
	Content    string
	Excerpt    string
	Subtitle   string
	Tags       TagsInput
	Published  bool
	Collection CollectionInput
}

// UpdateInput 是更新文章的入参；指针为 nil 表示请求体未提供该字段（保持原值）。
type UpdateInput struct {
	Title      *string
	Slug       *string
	Content    *string
	Excerpt    *string
	Subtitle   *string
	Tags       TagsInput
	Published  *bool
	Collection CollectionInput
}

// ListPublic 公开列表：仅已发布，支持 ?page= 与 ?tag=，按创建时间倒序。
func (r *Repo) ListPublic(ctx context.Context, page int, tag string) (ListResult, error) {
	conditions := []string{"p.published = 1"}
	args := []any{}
	if tag != "" {
		conditions = append(conditions, store.TagLikeClause("p.tags"))
		args = append(args, "%,"+store.EscapeLike(tag)+",%")
	}
	where := strings.Join(conditions, " AND ")

	var total int64
	if err := r.DB.SQL.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM posts p WHERE "+where, args...).Scan(&total); err != nil {
		return ListResult{}, err
	}
	totalPages := int((total + pageSize - 1) / pageSize)
	if totalPages < 1 {
		totalPages = 1
	}
	safePage := page
	if safePage > totalPages {
		safePage = totalPages
	}
	if safePage < 1 {
		safePage = 1
	}

	query := "SELECT " + postColsNoContent + postJoin + " WHERE " + where +
		" ORDER BY p.created_at DESC, p.id DESC LIMIT ? OFFSET ?"
	queryArgs := append(append([]any{}, args...), pageSize, (safePage-1)*pageSize)
	rows, err := r.DB.SQL.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()
	list := make([]Summary, 0)
	for rows.Next() {
		item, ok, err := scanSummary(rows)
		if err != nil {
			return ListResult{}, err
		}
		if ok {
			list = append(list, item)
		}
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}
	return ListResult{List: list, Page: safePage, PageSize: pageSize, Total: total, TotalPages: totalPages}, nil
}

// FindByKey 按 public_id（现行 URL）优先、slug（历史链接）其次查一篇文章；无则返回 nil。
// 草稿与否由调用方判断（公开读看发布状态，管理端照返回）。
func (r *Repo) FindByKey(ctx context.Context, key string) (*Detail, error) {
	if key == "" {
		return nil, nil
	}
	byPublicID := "SELECT " + postCols + postJoin + " WHERE p.public_id = ?"
	if detail, ok, err := scanDetail(r.DB.SQL.QueryRowContext(ctx, byPublicID, key)); err != nil {
		return nil, err
	} else if ok {
		return &detail, nil
	}
	bySlug := "SELECT " + postCols + postJoin + " WHERE p.slug = ?"
	if detail, ok, err := scanDetail(r.DB.SQL.QueryRowContext(ctx, bySlug, key)); err != nil {
		return nil, err
	} else if ok {
		return &detail, nil
	}
	return nil, nil
}

// ListAdmin 管理端列表：含草稿，按更新时间倒序。
func (r *Repo) ListAdmin(ctx context.Context) ([]Detail, error) {
	rows, err := r.DB.SQL.QueryContext(ctx,
		"SELECT "+postCols+postJoin+" ORDER BY p.updated_at DESC, p.id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]Detail, 0)
	for rows.Next() {
		item, ok, err := scanDetail(rows)
		if err != nil {
			return nil, err
		}
		if ok {
			list = append(list, item)
		}
	}
	return list, rows.Err()
}

// ListByCollection 取某合集下的已发布文章（合集详情页用），按创建时间倒序。
func (r *Repo) ListByCollection(ctx context.Context, collectionID int64) ([]Summary, error) {
	rows, err := r.DB.SQL.QueryContext(ctx,
		"SELECT "+postColsNoContent+postJoin+
			" WHERE p.collection_id = ? AND p.published = 1 ORDER BY p.created_at DESC, p.id DESC",
		collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]Summary, 0)
	for rows.Next() {
		item, ok, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		if ok {
			list = append(list, item)
		}
	}
	return list, rows.Err()
}

// PreviewInfo 是 preview-link 需要的最小字段。
type PreviewInfo struct {
	Slug      string
	PublicID  *string
	Published bool
}

// PreviewInfo 按数据库数字 id 取文章的预览信息；不存在返回 nil。
func (r *Repo) PreviewInfoByID(ctx context.Context, id int64) (*PreviewInfo, error) {
	var (
		slug      string
		publicID  sql.NullString
		published int64
	)
	err := r.DB.SQL.QueryRowContext(ctx,
		"SELECT slug, public_id, published FROM posts WHERE id = ?", id).Scan(&slug, &publicID, &published)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &PreviewInfo{Slug: slug, PublicID: nullString(publicID), Published: published != 0}, nil
}

// Create 新建文章：slug 冲突自动加序号、public_id 现签、标签重建在事务外由调用方负责。
func (r *Repo) Create(ctx context.Context, in CreateInput) (*Detail, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, ErrTitleEmpty
	}
	var created *Detail
	err := r.DB.Tx(ctx, func(q store.Querier) error {
		colID, err := resolveCollectionCreate(ctx, q, in.Collection)
		if err != nil {
			return err
		}
		slugValue, err := store.UniqueSlug(ctx, q, "posts",
			store.Slugify(firstNonEmpty(in.Slug, in.Title), "post"), 0)
		if err != nil {
			return err
		}
		now := store.NowText()
		id, err := store.InsertReturningID(ctx, q,
			`INSERT INTO posts (title, slug, public_id, content, excerpt, subtitle, tags, published, collection_id, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			strings.TrimSpace(in.Title),
			slugValue,
			r.Gen.Next(),
			in.Content,
			strings.TrimSpace(in.Excerpt),
			NormalizeSubtitle(in.Subtitle),
			strings.Join(in.Tags.tagList(""), ","),
			boolToInt(in.Published),
			colID,
			now,
			now,
		)
		if err != nil {
			return err
		}
		detail, err := findByID(ctx, q, id)
		if err != nil {
			return err
		}
		created = detail
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// Update 更新文章：仅在请求体提供字段的范围内改动；slug 缺省时保持不变。
func (r *Repo) Update(ctx context.Context, id int64, in UpdateInput) (*Detail, error) {
	var updated *Detail
	err := r.DB.Tx(ctx, func(q store.Querier) error {
		current, err := findByID(ctx, q, id)
		if err != nil {
			return err
		}
		if current == nil {
			return ErrPostNotFound
		}

		title := current.Title
		if in.Title != nil {
			title = *in.Title
		}
		content := current.Content
		if in.Content != nil {
			content = *in.Content
		}
		excerpt := current.Excerpt
		if in.Excerpt != nil {
			excerpt = *in.Excerpt
		}
		subtitle := current.Subtitle
		if in.Subtitle != nil {
			subtitle = NormalizeSubtitle(*in.Subtitle)
		}
		published := current.Published
		if in.Published != nil {
			published = *in.Published
		}

		base := current.Slug
		if in.Slug != nil && strings.TrimSpace(*in.Slug) != "" {
			base = store.Slugify(*in.Slug, "post")
		} else if base == "" {
			base = store.Slugify(firstNonEmpty(title, current.Title), "post")
		}
		slugValue, err := store.UniqueSlug(ctx, q, "posts", base, id)
		if err != nil {
			return err
		}

		collectionID, err := resolveCollectionUpdate(ctx, q, in.Collection, current)
		if err != nil {
			return err
		}

		if _, err := q.ExecContext(ctx,
			`UPDATE posts
			 SET title = ?, slug = ?, content = ?, excerpt = ?, subtitle = ?, tags = ?, published = ?,
			     collection_id = ?, updated_at = ?
			 WHERE id = ?`,
			strings.TrimSpace(title),
			slugValue,
			content,
			strings.TrimSpace(excerpt),
			subtitle,
			strings.Join(in.Tags.tagList(currentTags(current)), ","),
			boolToInt(published),
			collectionID,
			store.NowText(),
			id,
		); err != nil {
			return err
		}

		detail, err := findByID(ctx, q, id)
		if err != nil {
			return err
		}
		updated = detail
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// Delete 删除文章；返回是否命中（未命中由调用方转 404）。
func (r *Repo) Delete(ctx context.Context, id int64) (bool, error) {
	res, err := r.DB.SQL.ExecContext(ctx, "DELETE FROM posts WHERE id = ?", id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// findByID 在给定查询器上按 id 取文章详情；不存在返回 nil。
func findByID(ctx context.Context, q store.Querier, id int64) (*Detail, error) {
	row := q.QueryRowContext(ctx, "SELECT "+postCols+postJoin+" WHERE p.id = ?", id)
	detail, ok, err := scanDetail(row)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return &detail, nil
}

// collectionExists 判断合集是否存在。
func collectionExists(ctx context.Context, q store.Querier, id int64) (bool, error) {
	var probe int64
	err := q.QueryRowContext(ctx, "SELECT id FROM collections WHERE id = ?", id).Scan(&probe)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// resolveCollectionCreate 解析新建时的 collection_id（未提供/清空 → NULL）。
func resolveCollectionCreate(ctx context.Context, q store.Querier, in CollectionInput) (any, error) {
	if !in.Present || in.Clear {
		return nil, nil
	}
	return resolveExistingCollection(ctx, q, in.Value)
}

// resolveCollectionUpdate 解析更新时的 collection_id：未提供保持原值、清空 → NULL、否则校验存在性。
func resolveCollectionUpdate(ctx context.Context, q store.Querier, in CollectionInput, current *Detail) (any, error) {
	if !in.Present {
		if current.Collection == nil {
			return nil, nil
		}
		return current.Collection.ID, nil
	}
	if in.Clear {
		return nil, nil
	}
	return resolveExistingCollection(ctx, q, in.Value)
}

// resolveExistingCollection 校验候选合集 id 是否有效（正数且存在），否则报「合集不存在」。
func resolveExistingCollection(ctx context.Context, q store.Querier, id int64) (any, error) {
	if id <= 0 {
		return nil, ErrCollectionNotFound
	}
	ok, err := collectionExists(ctx, q, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrCollectionNotFound
	}
	return id, nil
}

// currentTags 把现有文章标签拼回逗号串（供更新时 tags 缺省复用）。
func currentTags(d *Detail) string {
	return strings.Join(d.Tags, ",")
}

// boolToInt 把布尔转成旧实现的 0/1 存储形态。
func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
