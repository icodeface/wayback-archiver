package storage

import (
	"errors"
	"log"
	"sync"
)

// ConcurrencyManager 统一管理资源下载和按 key 串行化。
type ConcurrencyManager struct {
	downloadSem chan struct{}
	pageMu      *keyedMutex
	resourceMu  *keyedMutex
	pageTaskMu  sync.Mutex
	pageTaskSeq map[int64]uint64
	background  sync.WaitGroup
}

func NewConcurrencyManager(workers int) *ConcurrencyManager {
	if workers <= 0 {
		workers = 1
	}
	return &ConcurrencyManager{
		downloadSem: make(chan struct{}, workers),
		pageMu:      newKeyedMutex(),
		resourceMu:  newKeyedMutex(),
		pageTaskSeq: make(map[int64]uint64),
	}
}

func (m *ConcurrencyManager) AcquireDownload() func() {
	if m == nil || m.downloadSem == nil {
		return func() {}
	}
	m.downloadSem <- struct{}{}
	return func() { <-m.downloadSem }
}

func (m *ConcurrencyManager) LockPage(key string) func() {
	if m == nil || m.pageMu == nil {
		return func() {}
	}
	return m.pageMu.lock(key)
}

func (m *ConcurrencyManager) LockResource(key string) func() {
	if m == nil || m.resourceMu == nil {
		return func() {}
	}
	return m.resourceMu.lock(key)
}

func (m *ConcurrencyManager) Workers() int {
	if m == nil || m.downloadSem == nil {
		return 0
	}
	return cap(m.downloadSem)
}

func (m *ConcurrencyManager) NextPageTask(pageID int64) uint64 {
	m.pageTaskMu.Lock()
	defer m.pageTaskMu.Unlock()
	m.pageTaskSeq[pageID]++
	return m.pageTaskSeq[pageID]
}

func (m *ConcurrencyManager) IsLatestPageTask(pageID int64, seq uint64) bool {
	m.pageTaskMu.Lock()
	defer m.pageTaskMu.Unlock()
	return m.pageTaskSeq[pageID] == seq
}

func (m *ConcurrencyManager) finishPageTask(pageID int64, seq uint64) {
	m.pageTaskMu.Lock()
	defer m.pageTaskMu.Unlock()
	if m.pageTaskSeq[pageID] == seq {
		delete(m.pageTaskSeq, pageID)
	}
}

func (m *ConcurrencyManager) RunPageTask(pageID int64, seq uint64, label string, fn func() error, onError func(error)) {
	m.background.Add(1)
	go func() {
		defer m.background.Done()
		defer m.finishPageTask(pageID, seq)
		if err := fn(); err != nil {
			if errors.Is(err, errStalePageTask) {
				log.Printf("[%s] Skipped stale background task for page %d", label, pageID)
				return
			}
			if onError != nil {
				onError(err)
			}
			log.Printf("[%s] Background task failed for page %d: %v", label, pageID, err)
		}
	}()
}

func (m *ConcurrencyManager) WaitForBackgroundTasks() { m.background.Wait() }
