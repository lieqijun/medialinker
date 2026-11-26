package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// State 已处理文件记录，避免重复刮削和重复整理
type State struct {
	mu sync.Mutex

	path string
	data map[string]Record
}

// Record 单条处理记录
type Record struct {
	Source    string `json:"source"`    // 源文件绝对路径
	Target    string `json:"target"`    // 目标文件路径
	TMDbID    int    `json:"tmdb_id"`
	Title     string `json:"title"`
	Processed string `json:"processed"` // 处理时间
}

func LoadState(path string) (*State, error) {
	s := &State{path: path, data: map[string]Record{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		// 记录文件损坏时不阻塞启动，重建即可
		s.data = map[string]Record{}
	}
	return s, nil
}

// Done 是否已处理过，且目标文件仍然存在
func (s *State) Done(src string) bool {
	s.mu.Lock()
	r, ok := s.data[src]
	s.mu.Unlock()
	if !ok {
		return false
	}
	if r.Target == "" {
		return true
	}
	if _, err := os.Stat(r.Target); err != nil {
		return false
	}
	return true
}

// Mark 记录一条处理结果并落盘
func (s *State) Mark(src string, rec Record) error {
	s.mu.Lock()
	rec.Processed = time.Now().Format(time.RFC3339)
	s.data[src] = rec
	s.mu.Unlock()
	return s.save()
}

func (s *State) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o775); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o664); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
