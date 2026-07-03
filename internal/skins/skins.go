// Package skins loads skins.txt once into memory for O(1) lookups.
package skins

import (
	"bufio"
	"os"
	"strings"
	"sync"
)

// Entry is one skin row.
type Entry struct {
	ID   string
	Name string
}

// Index holds the parsed skins.txt.
type Index struct {
	byID   map[string]Entry
	byName map[string][]Entry
	all    []Entry
}

var (
	once     sync.Once
	instance *Index
	loadErr  error
)

// Load parses skins.txt from the given path. Called once.
func Load(path string) (*Index, error) {
	once.Do(func() {
		f, err := os.Open(path)
		if err != nil {
			loadErr = err
			return
		}
		defer f.Close()
		idx := &Index{
			byID:   make(map[string]Entry),
			byName: make(map[string][]Entry),
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		var curID, curName string
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "ID - ") {
				curID = strings.TrimPrefix(line, "ID - ")
			} else if strings.HasPrefix(line, "NAME - ") {
				curName = strings.TrimPrefix(line, "NAME - ")
				if curID != "" && curName != "" {
					e := Entry{ID: curID, Name: curName}
					idx.byID[curID] = e
					key := strings.ToLower(strings.Replace(curName, ".mod", "", 1))
					idx.byName[key] = append(idx.byName[key], e)
					idx.all = append(idx.all, e)
					curID, curName = "", ""
				}
			}
		}
		instance = idx
	})
	return instance, loadErr
}

// FindByID returns the skin with the exact ID.
func (i *Index) FindByID(id string) *Entry {
	if e, ok := i.byID[id]; ok {
		return &e
	}
	return nil
}

// FindByName returns all skins whose name contains the query.
func (i *Index) FindByName(query string) []Entry {
	q := strings.ToLower(strings.Replace(query, ".mod", "", 1))
	if entries, ok := i.byName[q]; ok {
		return entries
	}
	var out []Entry
	for key, entries := range i.byName {
		if strings.Contains(key, q) {
			out = append(out, entries...)
		}
	}
	return out
}

// Search tries ID first, then name.
func (i *Index) Search(query string) []Entry {
	if e := i.FindByID(query); e != nil {
		return []Entry{*e}
	}
	return i.FindByName(query)
}

// All returns all skins.
func (i *Index) All() []Entry { return i.all }
