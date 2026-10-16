package main

import (
	"bufio"
	"context"
	"database/sql"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"anonychater/internal/hub"
	"anonychater/internal/store"
)

//go:embed all:web
var webFS embed.FS

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

const (
	dbPath     = "anonychater.db"
	archiveDir = "archives"
	archivePre = "anonychater-"
)

var (
	verbose   = flag.Bool("v", false, "enable verbose/debug logging")
	adminPass = flag.String("admin-pass", "", "static password to enable /export, /exportlist and POST /archive (empty = disabled)")
	port      = flag.Int("port", 8080, "HTTP listen port (ignored if -uds is set)")
	udsPath   = flag.String("uds", "", "unix domain socket path to listen on (e.g. /tmp/anonychater.sock); if set, overrides -port")
)

var appDB *sql.DB

func logV(format string, args ...any) {
	if *verbose {
		log.Printf(format, args...)
	}
}

func main() {
	flag.Parse()
	hub.Verbose = *verbose

	queries, db, err := store.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	appDB = db
	defer func() {
		if appDB != nil {
			appDB.Close()
		}
	}()

	loadHotWords(webFS, queries)

	h := hub.New(queries)

	// ===== WebSocket =====
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("upgrade: %v", err)
			return
		}
		clientID := r.URL.Query().Get("client_id")
		if clientID == "" {
			clientID = hub.NewID()
		}
		userID, name := h.EnsureIdentity(clientID)
		c := hub.NewClient(h, conn, clientID, userID, name)
		h.Register(c)
		h.OnJoin(c)
		go c.WritePump()
		c.ReadPump()
	})

	// ===== 管理端点（定义在 admin.go）=====
	registerAdminRoutes(h)

	// ===== 静态文件 =====
	webRoot, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}
	http.Handle("/", http.FileServer(http.FS(webRoot)))

	if *adminPass != "" {
		log.Println("admin enabled: /export, /exportlist, POST /archive")
	} else {
		log.Println("admin disabled (use -admin-pass=<password> to enable)")
	}

	// ===== 监听 =====
	if *udsPath != "" {
		// 先删掉旧 socket 文件，否则 Listen 会报 address already in use
		if _, err := os.Stat(*udsPath); err == nil {
			if err := os.Remove(*udsPath); err != nil {
				log.Fatalf("remove stale socket %s: %v", *udsPath, err)
			}
		}
		ln, err := net.Listen("unix", *udsPath)
		if err != nil {
			log.Fatalf("listen unix %s: %v", *udsPath, err)
		}
		// 让同组用户也能读写，按需调整
		if err := os.Chmod(*udsPath, 0o660); err != nil {
			log.Printf("chmod socket: %v", err)
		}
		log.Printf("listening on unix://%s", *udsPath)
		log.Fatal(http.Serve(ln, nil))
	}

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func loadHotWords(fsys fs.FS, q *store.Queries) {
	f, err := fsys.Open("web/words.txt")
	if err != nil {
		log.Printf("web/words.txt not found in embed, skip: %v", err)
		return
	}
	defer f.Close()

	ctx := context.Background()
	if err := q.ClearHotWords(ctx); err != nil {
		log.Printf("clear hot_words: %v", err)
		return
	}

	scanner := bufio.NewScanner(f)
	now := time.Now().UnixMilli()
	count := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		category := strings.TrimSpace(parts[0])
		word := strings.TrimSpace(parts[1])
		if category == "" || word == "" {
			continue
		}
		if err := q.InsertHotWord(ctx, store.InsertHotWordParams{
			Word:      word,
			Category:  category,
			CreatedAt: now,
		}); err != nil {
			log.Printf("insert hot_word %s:%s: %v", category, word, err)
			continue
		}
		count++
	}
	logV("loaded %d hot words", count)
}