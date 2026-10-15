// Package posts 是文章领域：公开读、管理写、草稿预览与发布约定。
//
// JSON 字段顺序与旧实现逐字对齐（前端会按字段而非顺序消费，但契约要求形状一致）：
// 列表 id,title,subtitle,slug,public_id,excerpt,tags,published,created_at,updated_at,collection；
// 详情在 public_id 之后多一个 content。public_id 必须是字符串（雪花 ID 超 JS 安全整数）。
package posts

import (
	"database/sql"

	"blog-go/internal/tags"
)

// CollectionRef 是文章附带的合集引用（合集为 NULL 时该字段为 null）。
type CollectionRef struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	PublicID string `json:"public_id"`
}

// Summary 是列表用的文章结构（不含正文）。
type Summary struct {
	ID         int64          `json:"id"`
	Title      string         `json:"title"`
	Subtitle   string         `json:"subtitle"`
	Slug       string         `json:"slug"`
	PublicID   *string        `json:"public_id"`
	Excerpt    string         `json:"excerpt"`
	Tags       []string       `json:"tags"`
	Published  bool           `json:"published"`
	CreatedAt  string         `json:"created_at"`
	UpdatedAt  string         `json:"updated_at"`
	Collection *CollectionRef `json:"collection"`
}

// Detail 是文章详情 / 管理端结构（含正文，public_id 之后紧跟 content）。
type Detail struct {
	ID         int64          `json:"id"`
	Title      string         `json:"title"`
	Subtitle   string         `json:"subtitle"`
	Slug       string         `json:"slug"`
	PublicID   *string        `json:"public_id"`
	Content    string         `json:"content"`
	Excerpt    string         `json:"excerpt"`
	Tags       []string       `json:"tags"`
	Published  bool           `json:"published"`
	CreatedAt  string         `json:"created_at"`
	UpdatedAt  string         `json:"updated_at"`
	Collection *CollectionRef `json:"collection"`
}

// ListResult 是公开文章列表的分页响应体（字段顺序与旧实现一致）。
type ListResult struct {
	List       []Summary `json:"list"`
	Page       int       `json:"page"`
	PageSize   int       `json:"pageSize"`
	Total      int64     `json:"total"`
	TotalPages int       `json:"totalPages"`
}

// CollectionInput 表达请求体里 collection_id 的三种意图：未提供 / 显式清空 / 指定候选值。
type CollectionInput struct {
	Present bool
	Clear   bool
	Value   int64
}

// TagsInput 表达请求体里 tags 的形态：未提供 / JSON 数组 / 标量。
// 数组元素与标量都按旧实现的 String() 语义先转成字符串再处理。
type TagsInput struct {
	Present bool
	IsArray bool
	Array   []string
	Scalar  string
}

// tagList 还原旧实现 `Array.isArray(tags) ? tags : parseTags(tags)`。
func (t TagsInput) tagList(fallback string) []string {
	if t.Present && t.IsArray {
		return t.Array
	}
	if t.Present {
		return tags.Parse(t.Scalar)
	}
	return tags.Parse(fallback)
}

// scanner 是 *sql.Row / *sql.Rows 的公共子集。
type scanner interface{ Scan(dest ...any) error }

// scanSummary 扫描「不含正文」的查询行。
func scanSummary(s scanner) (Summary, bool, error) {
	var (
		id                      int64
		title, subtitle, slug   string
		publicID                sql.NullString
		excerpt, tagStr         string
		published               int64
		createdAt, updatedAt    string
		cID                     sql.NullInt64
		cName, cSlug, cPublicID sql.NullString
	)
	err := s.Scan(&id, &title, &subtitle, &slug, &publicID, &excerpt, &tagStr,
		&published, &createdAt, &updatedAt, &cID, &cName, &cSlug, &cPublicID)
	if err == sql.ErrNoRows {
		return Summary{}, false, nil
	}
	if err != nil {
		return Summary{}, false, err
	}
	return Summary{
		ID:         id,
		Title:      title,
		Subtitle:   subtitle,
		Slug:       slug,
		PublicID:   nullString(publicID),
		Excerpt:    excerpt,
		Tags:       tags.Parse(tagStr),
		Published:  published != 0,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
		Collection: collectionRef(cID, cName, cSlug, cPublicID),
	}, true, nil
}

// scanDetail 扫描「含正文」的查询行。
func scanDetail(s scanner) (Detail, bool, error) {
	var (
		id                       int64
		title, subtitle, slug    string
		publicID                 sql.NullString
		content, excerpt, tagStr string
		published                int64
		createdAt, updatedAt     string
		cID                      sql.NullInt64
		cName, cSlug, cPublicID  sql.NullString
	)
	err := s.Scan(&id, &title, &subtitle, &slug, &publicID, &content, &excerpt, &tagStr,
		&published, &createdAt, &updatedAt, &cID, &cName, &cSlug, &cPublicID)
	if err == sql.ErrNoRows {
		return Detail{}, false, nil
	}
	if err != nil {
		return Detail{}, false, err
	}
	return Detail{
		ID:         id,
		Title:      title,
		Subtitle:   subtitle,
		Slug:       slug,
		PublicID:   nullString(publicID),
		Content:    content,
		Excerpt:    excerpt,
		Tags:       tags.Parse(tagStr),
		Published:  published != 0,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
		Collection: collectionRef(cID, cName, cSlug, cPublicID),
	}, true, nil
}

// nullString 把可空文本列转成 *string（NULL → nil，保持 JSON 里的 null）。
func nullString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

// collectionRef 把 JOIN 出的合集列拼成引用对象；无合集时为 nil。
func collectionRef(id sql.NullInt64, name, slug, publicID sql.NullString) *CollectionRef {
	if !id.Valid {
		return nil
	}
	return &CollectionRef{
		ID:       id.Int64,
		Name:     name.String,
		Slug:     slug.String,
		PublicID: publicID.String,
	}
}
