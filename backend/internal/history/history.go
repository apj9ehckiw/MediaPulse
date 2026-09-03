// Package history 下载记录：每次成功/失败的下载流水，持久化 JSONL。
package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Record 一条下载记录。
type Record struct {
	TopicID    int64     `json:"topicId"`
	AuthorUID  int64     `json:"authorUid"`
	Title      string    `json:"title"`
	CreateTime string    `json:"createTime"` // 帖子发布时间
	File       string    `json:"file"`       // 文件名（相对输出目录）
	SizeMB     float64   `json:"sizeMB"`
	Status     string    `json:"status"` // done | failed | skipped
	Error      string    `json:"error,omitempty"`
	At         time.Time `json:"at"`
}

// Store 记录存储：内存环形 + JSONL 追加落盘。
type Store struct {
	mu     sync.RWMutex
	path   string
	maxMem int
	rows   []Record
}

// Open 打开记录存储并回放历史文件。
func Open(path string) (*Store, error) {
	s := &Store{path: path, maxMem: 1000}
	data, err := os.ReadFile(path)
	if err == nil {
		var rows []Record
		if err := json.Unmarshal(data, &rows); err == nil {
			if len(rows) > s.maxMem {
				rows = rows[len(rows)-s.maxMem:]
			}
			s.rows = rows
		}
	} else {
		// 首次：确保目录存在
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Add 追加一条记录并落盘。
func (s *Store) Add(r Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, r)
	if len(s.rows) > s.maxMem {
		s.rows = s.rows[len(s.rows)-s.maxMem:]
	}
	s.persist()
}

// Clear 删除记录并落盘；status 为空或 "all" 表示全部，否则只删该状态（done|failed|skipped）。
// 返回被删除的记录（供调用方同步清理其他状态，如下载去重表）。
func (s *Store) Clear(status string) []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := make([]Record, 0)
	kept := make([]Record, 0, len(s.rows))
	for _, r := range s.rows {
		if status == "" || status == "all" || r.Status == status {
			removed = append(removed, r)
			continue
		}
		kept = append(kept, r)
	}
	if len(removed) > 0 {
		s.rows = kept
		s.persist()
	}
	return removed
}

// Delete 删除单条记录（按 topicId + status + at 精确匹配）并落盘。
// 命中返回被删记录。
func (s *Store) Delete(topicID int64, status, at string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return Record{}, false
	}
	for i, r := range s.rows {
		if r.TopicID == topicID && r.Status == status && r.At.Equal(t) {
			out := r
			s.rows = append(s.rows[:i], s.rows[i+1:]...)
			s.persist()
			return out, true
		}
	}
	return Record{}, false
}

// RemoveByTopics 删除指定帖子 ID 集合的全部记录（删除作者连带清理时用）并落盘。
// 返回删除条数。
func (s *Store) RemoveByTopics(topicIDs []int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(topicIDs) == 0 {
		return 0
	}
	set := make(map[int64]struct{}, len(topicIDs))
	for _, id := range topicIDs {
		set[id] = struct{}{}
	}
	kept := make([]Record, 0, len(s.rows))
	removed := 0
	for _, r := range s.rows {
		if _, ok := set[r.TopicID]; ok {
			removed++
			continue
		}
		kept = append(kept, r)
	}
	if removed > 0 {
		s.rows = kept
		s.persist()
	}
	return removed
}

// RemoveByAuthor 删除指定作者的全部记录并落盘。返回删除条数。
func (s *Store) RemoveByAuthor(uid int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]Record, 0, len(s.rows))
	removed := 0
	for _, r := range s.rows {
		if r.AuthorUID == uid {
			removed++
			continue
		}
		kept = append(kept, r)
	}
	if removed > 0 {
		s.rows = kept
		s.persist()
	}
	return removed
}

// persist 落盘（调用方需持锁）。
func (s *Store) persist() {
	data, err := json.MarshalIndent(s.rows, "", " ")
	if err == nil {
		_ = os.WriteFile(s.path, data, 0o644)
	}
}

// List 返回记录，最新的在前；limit<=0 表示全部。
func (s *Store) List(limit int) []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Record, len(s.rows))
	// 逆序拷贝：最新在前
	for i, r := range s.rows {
		out[len(s.rows)-1-i] = r
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Count 统计。
func (s *Store) Count() (done, failed, skipped int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.rows {
		switch r.Status {
		case "done":
			done++
		case "failed":
			failed++
		default:
			skipped++
		}
	}
	return
}
