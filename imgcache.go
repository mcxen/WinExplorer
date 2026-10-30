package main

import (
	"container/list"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo/ui"
)

// imgcache.go 让看图翻页不卡：屏幕尺寸的解码图单独放一个按字节计的 LRU，
// 翻页时在后台池里预取翻页方向上的后两张和反方向的一张，换方向或跳走时
// 排队中的旧预取作废。qView / nomacs 都是这个做法：解码永远不在 UI 线程，
// 用户按下一张时那张往往已经解好了。
//
// 一张 2048 px 的 3:2 图是 11 MB，加上 mygo 为缩小绘制留的半尺寸副本再多
// 三分之一；预算 128 MB 约容 8 张，足够前后各几张来回翻。

const (
	screenCacheBudget = 128 << 20
	prefetchWorkers   = 2
	prefetchAhead     = 2 // 翻页方向上预取几张
	prefetchBehind    = 1 // 反方向预取几张
)

type screenKey struct {
	path  string
	size  int64
	mtime int64
	side  int
}

type screenEntry struct {
	key   screenKey
	bm    *ui.Bitmap
	bytes int
}

var (
	screenMu    sync.Mutex
	screenLRU   = list.New()
	screenIndex = map[screenKey]*list.Element{}
	screenBytes int
	// screenWait 是正在解码的键：第二个要同一张图的人等第一个，不重复解码。
	screenWait = map[screenKey]chan struct{}{}

	// 统计，供 spec 的命中率。
	screenHits, screenMisses atomic.Int64

	prefetchGen  atomic.Uint64
	prefetchMu   sync.Mutex
	prefetchJobs []prefetchJob
	prefetchCond = sync.NewCond(&prefetchMu)
	prefetchOnce sync.Once
)

type prefetchJob struct {
	path string
	gen  uint64
}

func screenKeyOf(path string, side int) (screenKey, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return screenKey{}, false
	}
	return screenKey{path, info.Size(), info.ModTime().UnixNano(), side}, true
}

// screenPeek 返回缓存里的图（不解码），没有则 nil。
func screenPeek(k screenKey) *ui.Bitmap {
	screenMu.Lock()
	defer screenMu.Unlock()
	if el, ok := screenIndex[k]; ok {
		screenLRU.MoveToFront(el)
		return el.Value.(*screenEntry).bm
	}
	return nil
}

// screenBitmap 是屏幕尺寸的图：先查 LRU，再等正在进行的同一解码，最后自己
// 解码（磁盘缓存 → 原图）。只在后台 goroutine 里调。
func screenBitmap(path string, side int, decode func(string, int) (*ui.Bitmap, error)) (*ui.Bitmap, bool, error) {
	k, ok := screenKeyOf(path, side)
	if !ok {
		bm, err := decode(path, side)
		return bm, false, err
	}
	for {
		screenMu.Lock()
		if el, ok := screenIndex[k]; ok {
			screenLRU.MoveToFront(el)
			screenMu.Unlock()
			screenHits.Add(1)
			return el.Value.(*screenEntry).bm, true, nil
		}
		if ch, busy := screenWait[k]; busy {
			screenMu.Unlock()
			<-ch // 预取正在解这张：等它，算命中
			screenMu.Lock()
			if el, ok := screenIndex[k]; ok {
				screenMu.Unlock()
				screenHits.Add(1)
				return el.Value.(*screenEntry).bm, true, nil
			}
			screenMu.Unlock()
			continue // 那次解码失败了，自己再来
		}
		ch := make(chan struct{})
		screenWait[k] = ch
		screenMu.Unlock()
		screenMisses.Add(1)

		bm, err := decode(path, side)
		screenMu.Lock()
		delete(screenWait, k)
		close(ch)
		if err == nil && bm != nil {
			screenPut(k, bm)
		}
		screenMu.Unlock()
		return bm, false, err
	}
}

// screenPut 加入一张图并按预算淘汰最久没用的。调用方持有 screenMu。
func screenPut(k screenKey, bm *ui.Bitmap) {
	if el, ok := screenIndex[k]; ok {
		screenBytes -= el.Value.(*screenEntry).bytes
		screenLRU.Remove(el)
	}
	e := &screenEntry{key: k, bm: bm, bytes: bitmapBytes(bm)}
	screenIndex[k] = screenLRU.PushFront(e)
	screenBytes += e.bytes
	for screenBytes > screenCacheBudget && screenLRU.Len() > 1 {
		old := screenLRU.Back()
		oe := old.Value.(*screenEntry)
		screenLRU.Remove(old)
		delete(screenIndex, oe.key)
		screenBytes -= oe.bytes
	}
}

// clearScreenCache 随 刷新 一起清空。
func clearScreenCache() {
	screenMu.Lock()
	screenLRU.Init()
	screenIndex = map[screenKey]*list.Element{}
	screenBytes = 0
	screenMu.Unlock()
}

// prefetchImages 把 paths 排进预取队列，替换掉还没开始的旧任务（它们属于
// 上一个位置，已经不需要了）。正在解的那张解完照样进缓存。
func prefetchImages(paths []string) {
	prefetchOnce.Do(func() {
		for range prefetchWorkers {
			go prefetchWorker()
		}
	})
	gen := prefetchGen.Add(1)
	prefetchMu.Lock()
	prefetchJobs = prefetchJobs[:0]
	for _, p := range paths {
		prefetchJobs = append(prefetchJobs, prefetchJob{p, gen})
	}
	prefetchMu.Unlock()
	prefetchCond.Broadcast()
}

func prefetchWorker() {
	for {
		prefetchMu.Lock()
		for len(prefetchJobs) == 0 {
			prefetchCond.Wait()
		}
		j := prefetchJobs[0]
		prefetchJobs = prefetchJobs[1:]
		prefetchMu.Unlock()
		if j.gen != prefetchGen.Load() {
			continue // 过期
		}
		_, _, _ = screenBitmap(j.path, previewImageSide, decodeScreenBitmap)
	}
}

// prefetchNeighbours 在 UI 线程上算出要预取的邻居：dir 是上次到这次的移动
// 方向（+1 往后，-1 往前），翻页方向上取 prefetchAhead 张，反方向
// prefetchBehind 张。只预取图片。
func prefetchNeighbours(vis []Entry, idx, dir int) []string {
	if dir == 0 {
		dir = 1
	}
	var out []string
	add := func(step, n int) {
		for i, got := idx+step, 0; i >= 0 && i < len(vis) && got < n; i += step {
			e := vis[i]
			if e.IsDir {
				continue
			}
			if !isPrefetchableImage(e.Path) {
				continue
			}
			out = append(out, e.Path)
			got++
		}
	}
	add(dir, prefetchAhead)
	add(-dir, prefetchBehind)
	return out
}

func isPrefetchableImage(path string) bool {
	ext := lowerExt(path)
	return stdlibImageExts[ext] && ext != "gif" && !strings.Contains(path, "\x00")
}

// prefetchState 记住上次预取的位置，算翻页方向。
type prefetchState struct {
	dir string
	idx int
	way int
}

// prefetchAround 在选中一张图且预览（窗格或空格预览）开着时，按翻页方向
// 预取邻居。e 是当前选中项。
func (a *App) prefetchAround(e Entry) {
	if e.Path == "" || e.IsDir || !(a.quickLook || a.previewOpen) || !isPrefetchableImage(e.Path) {
		return
	}
	vis := a.visibleEntries()
	idx := -1
	for i := range vis {
		if vis[i].Path == e.Path {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	dir := filepath.Dir(e.Path)
	way := a.imgPF.way
	if a.imgPF.dir == dir && idx != a.imgPF.idx {
		way = 1
		if idx < a.imgPF.idx {
			way = -1
		}
	}
	a.imgPF = prefetchState{dir: dir, idx: idx, way: way}
	prefetchImages(prefetchNeighbours(vis, idx, way))
}
