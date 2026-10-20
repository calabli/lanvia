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

// 文件占用上限：每台电脑都会备份其他设备的文件，N 台电脑就是 N 份，所以要限制总量。
//   - 原件（本机发出 / 别人上传到本机的，Origin 是本机）永远不删，否则这份文件可能在所有设备上都没了；
//   - 超出上限时只删副本，从最旧的开始，“永久保留”的放到最后。条目本身保留，需要时再从其他设备取。

type StorageUsage struct {
	Own      int64 `json:"own"`      // 原件占用（字节）
	Replicas int64 `json:"replicas"` // 副本占用
	Count    int   `json:"count"`    // 副本个数
	QuotaMB  int   `json:"quotaMB"`  // 上限，0 为不限
}

// reserve 为即将写入的 n 字节预留空间：必要时先清理旧副本。放不下返回 false。
// 并发的下载各自预留，合计不会超过上限；下载结束（成功或失败）后调用 release。
// 成功时应先把文件改名就位再 release，这样中间不会有一刻少算。
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

// scanBlobs 统计本机的文件占用，并清理残留：中断的临时文件、已经没有对应条目的文件。
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
			// 条目已被删除或过期，但文件还在（例如删除时正在下载）
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

// makeRoom 确保再写入 need 字节（含已预留的）后不超过上限：必要时删除副本。返回能否放下。
// 调用方须持有 quotaMu。
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
	// 先删普通的，再删“永久保留”的；同类里先删旧的
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
		log.Printf("文件占用超过上限，已清理 %d 个副本（%.1f MB）", n, float64(freed)/(1<<20))
	}
	return used+need-freed <= quota
}

// enforceQuota 在上限调小或原件增多后，把副本清理到上限以内。
func (a *App) enforceQuota() {
	a.quotaMu.Lock()
	a.makeRoom(a.reserved)
	a.quotaMu.Unlock()
}

// ClearReplicas 删除所有副本（条目保留，需要时再从其他设备获取）。
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
	// 本次运行期间不再自动下载回来（重启后也只补最近 3 天的，见 wantFetch）
	a.fetch.mu.Lock()
	for _, b := range reps {
		a.fetch.lastTry[b.it.ID] = time.Now().Add(100 * 365 * 24 * time.Hour)
	}
	a.fetch.mu.Unlock()
	return n, freed
}
