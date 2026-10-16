package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"anonychater/internal/hub"
	"anonychater/internal/store"
)

// registerAdminRoutes 注册 /export、/exportlist、POST /archive。
func registerAdminRoutes(h *hub.Hub) {
	http.HandleFunc("/export", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r) {
			return
		}
		handleExport(h, w, r)
	})

	http.HandleFunc("/exportlist", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r) {
			return
		}
		handleExportList(w, r)
	})

	http.HandleFunc("/archive", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed, use POST", http.StatusMethodNotAllowed)
			return
		}
		handleArchive(h, w, r)
	})
}

// checkAuth 校验 Basic Auth。adminPass 为空时返回 404（端点隐身）。
func checkAuth(w http.ResponseWriter, r *http.Request) bool {
	if *adminPass == "" {
		http.NotFound(w, r)
		return false
	}
	_, pass, ok := r.BasicAuth()
	if !ok || subtle.ConstantTimeCompare([]byte(pass), []byte(*adminPass)) != 1 {
		w.Header().Set("WWW-Authenticate", `Basic realm="admin", charset="UTF-8"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

// ===== /export =====

func handleExport(h *hub.Hub, w http.ResponseWriter, r *http.Request) {
	dbParam := r.URL.Query().Get("db")

	var (
		msgs  []store.Message
		label string
		err   error
	)

	if dbParam == "" || dbParam == dbPath {
		label = "当前会话"
		msgs, err = h.Store().GetAllMessages(context.Background())
		if err != nil {
			log.Printf("export: %v", err)
			http.Error(w, "export failed", http.StatusInternalServerError)
			return
		}
	} else {
		if !isValidArchivePath(dbParam) {
			http.Error(w, "invalid archive path", http.StatusBadRequest)
			return
		}
		label = filepath.Base(dbParam)
		archQueries, archDB, oerr := openReadOnly(dbParam)
		if oerr != nil {
			log.Printf("open archive %s: %v", dbParam, oerr)
			http.Error(w, "cannot open archive", http.StatusInternalServerError)
			return
		}
		defer archDB.Close()
		msgs, err = archQueries.GetAllMessages(context.Background())
		if err != nil {
			log.Printf("query archive: %v", err)
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if rerr := renderExportHTML(w, msgs, label); rerr != nil {
		log.Printf("export render: %v", rerr)
	}
}

// ===== /exportlist =====

type dbEntry struct {
	Name    string
	Path    string
	Size    int64
	ModTime time.Time
	Current bool
}

func handleExportList(w http.ResponseWriter, r *http.Request) {
	entries := []dbEntry{}

	if st, err := os.Stat(dbPath); err == nil {
		entries = append(entries, dbEntry{
			Name:    "当前会话",
			Path:    dbPath,
			Size:    st.Size(),
			ModTime: st.ModTime(),
			Current: true,
		})
	}

	if files, err := os.ReadDir(archiveDir); err == nil {
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			name := f.Name()
			if !strings.HasPrefix(name, archivePre) || !strings.HasSuffix(name, ".db") {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			entries = append(entries, dbEntry{
				Name:    name,
				Path:    archiveDir + "/" + name,
				Size:    info.Size(),
				ModTime: info.ModTime(),
			})
		}
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Current {
			return true
		}
		if entries[j].Current {
			return false
		}
		return entries[i].ModTime.After(entries[j].ModTime)
	})

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	renderExportList(w, entries)
}

func renderExportList(w http.ResponseWriter, entries []dbEntry) {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<title>anonychater 归档列表</title>
<style>
* { box-sizing: border-box; }
body {
  margin: 0;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif;
  background: #f7f7f8;
  color: #1a1a1a;
  font-size: 14px;
  padding: 40px 20px;
  -webkit-font-smoothing: antialiased;
}
.container { max-width: 760px; margin: 0 auto; }
h1 { font-size: 20px; margin: 0 0 6px; }
.sub { color: #7a7a85; font-size: 13px; margin-bottom: 24px; }
.toolbar { margin-bottom: 16px; }
button {
  padding: 8px 16px;
  border: none;
  border-radius: 10px;
  background: #4d7cfe;
  color: #fff;
  font-family: inherit;
  font-size: 13.5px;
  font-weight: 500;
  cursor: pointer;
  transition: background .15s;
}
button:hover { background: #3d6ce8; }
table {
  width: 100%;
  border-collapse: collapse;
  background: #fff;
  border-radius: 12px;
  overflow: hidden;
  box-shadow: 0 1px 3px rgba(0,0,0,.06);
}
th, td {
  text-align: left;
  padding: 12px 16px;
  border-bottom: 1px solid #ececf1;
  font-size: 13.5px;
}
th { font-weight: 600; color: #7a7a85; font-size: 12px; background: #fafafb; }
tr:last-child td { border-bottom: none; }
td a { color: #4d7cfe; text-decoration: none; font-weight: 500; }
td a:hover { text-decoration: underline; }
.current-tag {
  display: inline-block;
  padding: 1px 8px;
  border-radius: 999px;
  background: #dcfce7;
  color: #15803d;
  font-size: 11px;
  font-weight: 600;
  margin-left: 6px;
}
.empty { color: #7a7a85; padding: 24px; text-align: center; }
</style>
</head>
<body>
<div class="container">
<h1>anonychater 归档</h1>
<div class="sub">列出当前会话数据库和所有归档。点「查看」在本页打开该档完整记录。</div>
<div class="toolbar">
  <form method="POST" action="/archive" onsubmit="return confirm('归档后当前会话将清空，且不能恢复。确定继续吗？')" style="display:inline">
    <button type="submit">归档当前会话并开启新房</button>
  </form>
</div>
<table>
<thead><tr><th>数据库</th><th>大小</th><th>修改时间</th><th></th></tr></thead>
<tbody>
`)

	if len(entries) == 0 {
		b.WriteString(`<tr><td colspan="4" class="empty">暂无数据</td></tr>`)
	}

	for _, e := range entries {
		b.WriteString(`<tr><td>`)
		b.WriteString(html.EscapeString(e.Name))
		if e.Current {
			b.WriteString(`<span class="current-tag">当前</span>`)
		}
		b.WriteString(`</td><td>`)
		b.WriteString(formatSize(e.Size))
		b.WriteString(`</td><td>`)
		b.WriteString(e.ModTime.Format("2006-01-02 15:04:05"))
		b.WriteString(`</td><td><a href="/export?db=`)
		b.WriteString(url.QueryEscape(e.Path))
		b.WriteString(`">查看</a></td></tr>`)
	}

	b.WriteString(`</tbody></table></div></body></html>`)
	w.Write([]byte(b.String()))
}

func formatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// ===== POST /archive =====

func handleArchive(h *hub.Hub, w http.ResponseWriter, r *http.Request) {
	// 1. WAL checkpoint + 关闭当前 DB
	if appDB != nil {
		_, _ = appDB.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
		if err := appDB.Close(); err != nil {
			log.Printf("close db: %v", err)
			http.Error(w, "close db failed", http.StatusInternalServerError)
			return
		}
		appDB = nil
	}

	// 2. 归档目录
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		log.Printf("mkdir archive: %v", err)
		http.Error(w, "mkdir failed", http.StatusInternalServerError)
		return
	}

	// 3. 改名
	ts := time.Now().Format("20060102-150405")
	target := filepath.Join(archiveDir, archivePre+ts+".db")

	if _, err := os.Stat(dbPath); err == nil {
		if err := os.Rename(dbPath, target); err != nil {
			log.Printf("rename db: %v", err)
			http.Error(w, "rename failed", http.StatusInternalServerError)
			return
		}
	}
	os.Remove(dbPath + "-wal")
	os.Remove(dbPath + "-shm")

	// 4. 打开新库
	newQueries, newDB, err := store.Open(dbPath)
	if err != nil {
		log.Printf("open new db: %v", err)
		http.Error(w, "open new db failed", http.StatusInternalServerError)
		return
	}
	appDB = newDB
	loadHotWords(webFS, newQueries)

	// 5. 切换 Hub 的 store
	h.SetStore(newQueries)

	log.Printf("archived -> %s, new session started", target)

	// 6. 回列表
	http.Redirect(w, r, "/exportlist", http.StatusSeeOther)
}

// ===== 辅助 =====

func isValidArchivePath(p string) bool {
	if !strings.HasPrefix(p, archiveDir+"/") {
		return false
	}
	if !strings.HasSuffix(p, ".db") {
		return false
	}
	if strings.Contains(p, "..") {
		return false
	}
	base := strings.TrimPrefix(p, archiveDir+"/")
	if !strings.HasPrefix(base, archivePre) {
		return false
	}
	return true
}

func openReadOnly(path string) (*store.Queries, *sql.DB, error) {
	dsn := "file:" + path + "?mode=ro&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, nil, err
	}
	return store.New(db), db, nil
}