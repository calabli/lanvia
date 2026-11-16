package core

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Storage limit: every computer backs up the files of the other devices, so N computers mean N copies and the total has to be capped.
//   - Originals (sent from this machine / uploaded to this machine by others, where Origin is this machine) are never deleted, otherwise the file could end up missing on every device;
//   - When the limit is exceeded only replicas are deleted, starting with the oldest and leaving the "permanently kept" ones until last. The entries themselves are kept and can be fetched from other devices again when needed.

type StorageUsage struct {
	Own      int64 `json:"own"`      // space used by originals (bytes)
	Replicas int64 `json:"replicas"` // space used by replicas
	Count    int   `json:"count"`    // number of replicas
	QuotaMB  int   `json:"quotaMB"`  // limit in MB, 0 means unlimited
}

// reserve reserves space for the n bytes about to be written, cleaning up old replicas first if needed. Returns false when it does not fit.
// Concurrent downloads reserve separately and never exceed the limit in total; call release once the download ends (successfully or not).
// On success, rename the file into place before calling release, so that it is never briefly undercounted.
func (a *App) reserve(n int64) (release func(), ok bool) {
	a.quotaMu.Lock()
	defer a.quotaMu.Unlock()
	if !a.makeRoom(a.reserved + n) {
		return nil, false
	}
	a.reserved += n
	var once sync.Once
	return func() {
		once.Do(func() {
			a.quotaMu.Lock()
			a.reserved -= n
			a.quotaMu.Unlock()
		})
	}, true
}

type blobFile struct {
	it   *Item
	size int64
}

// scanBlobs totals the file space used on this machine and cleans up leftovers: interrupted temporary files and files whose entry is gone.
func (a *App) scanBlobs() (own []blobFile, replicas []blobFile) {
	self := a.cfgSnapshot().ID
	items := map[string]*Item{}
	for _, it := range a.store.List() {
		items[it.ID] = it
	}
	entries, _ := os.ReadDir(a.store.blobDir)
	for _, e := range entries {
		name := e.Name()
		info, err := e.Info()
		if err != nil || e.IsDir() {
			continue
		}
		if strings.HasSuffix(name, ".part") {
			if time.Since(info.ModTime()) > 24*time.Hour {
				os.Remove(filepath.Join(a.store.blobDir, name))
			}
			continue
		}
		it := items[name]
		if it == nil {
			// the entry was deleted or expired but the file is still there (for example it was being downloaded when it got deleted)
			if time.Since(info.ModTime()) > time.Hour {
				os.Remove(filepath.Join(a.store.blobDir, name))
			}
			continue
		}
		if it.Origin == self {
			own = append(own, blobFile{it, info.Size()})
		} else {
			replicas = append(replicas, blobFile{it, info.Size()})
		}
	}
	return
}

func (a *App) StorageUsage() StorageUsage {
	own, reps := a.scanBlobs()
	u := StorageUsage{QuotaMB: a.cfgSnapshot().StorageMB, Count: len(reps)}
	for _, b := range own {
		u.Own += b.size
	}
	for _, b := range reps {
		u.Replicas += b.size
	}
	return u
}

// makeRoom makes sure that writing need more bytes (including the already reserved ones) stays within the limit, deleting replicas if needed. Reports whether it fits.
// The caller must hold quotaMu.
func (a *App) makeRoom(need int64) bool {
	quota := int64(a.cfgSnapshot().StorageMB) << 20
	if quota <= 0 {
		return true
	}
	own, reps := a.scanBlobs()
	var used int64
	for _, b := range own {
		used += b.size
	}
	for _, b := range reps {
		used += b.size
	}
	if used+need <= quota {
		return true
	}
	// delete the ordinary ones first, then the "permanently kept" ones; within a group, delete the oldest first
	sort.Slice(reps, func(i, j int) bool {
		if reps[i].it.pinned() != reps[j].it.pinned() {
			return !reps[i].it.pinned()
		}
		return reps[i].it.TS < reps[j].it.TS
	})
	freed, n := int64(0), 0
	for _, b := range reps {
		if used+need-freed <= quota {
			break
		}
		if os.Remove(a.store.blobPath(b.it.ID)) == nil {
			freed += b.size
			n++
		}
	}
	if n > 0 {
		log.Printf(L("文件占用超过上限，已清理 %d 个副本（%.1f MB）", "Over the storage limit, removed %d copies (%.1f MB)"), n, float64(freed)/(1<<20))
	}
	return used+need-freed <= quota
}

// enforceQuota trims the replicas back within the limit after the limit was lowered or more originals arrived.
func (a *App) enforceQuota() {
	a.quotaMu.Lock()
	a.makeRoom(a.reserved)
	a.quotaMu.Unlock()
}

// ClearReplicas deletes all replicas (the entries are kept and can be fetched from other devices again when needed).
func (a *App) ClearReplicas() (int, int64) {
	a.quotaMu.Lock()
	defer a.quotaMu.Unlock()
	_, reps := a.scanBlobs()
	n, freed := 0, int64(0)
	for _, b := range reps {
		if os.Remove(a.store.blobPath(b.it.ID)) == nil {
			n++
			freed += b.size
		}
	}
	// do not auto-download them back during this run (after a restart only the most recent 3 days are fetched, see wantFetch)
	a.fetch.mu.Lock()
	for _, b := range reps {
		a.fetch.lastTry[b.it.ID] = time.Now().Add(100 * 365 * 24 * time.Hour)
	}
	a.fetch.mu.Unlock()
	return n, freed
}
