package hub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"

	pb "anonychater/proto"
	"anonychater/internal/store"
)

var Verbose = false

func debugf(format string, args ...any) {
	if Verbose {
		log.Printf(format, args...)
	}
}

type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}
	store   atomic.Pointer[store.Queries]
	seq     atomic.Uint64
}

func New(queries *store.Queries) *Hub {
	h := &Hub{
		clients: make(map[*Client]struct{}),
	}
	h.store.Store(queries)
	return h
}

func (h *Hub) Store() *store.Queries { return h.store.Load() }

func (h *Hub) SetStore(q *store.Queries) { h.store.Store(q) }

func (h *Hub) NextSeq() uint64 { return h.seq.Add(1) }

func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	h.broadcastOnlineCount()
}

func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	h.broadcastOnlineCount()
}

func (h *Hub) broadcastOnlineCount() {
	h.mu.RLock()
	count := len(h.clients)
	h.mu.RUnlock()

	h.Broadcast(&pb.ChatMessage{
		MessageId: NewID(),
		Seq:       h.NextSeq(),
		Type:      pb.MessageType_SYSTEM,
		Timestamp: time.Now().UnixMilli(),
		Content:   fmt.Sprintf("%d", count),
	}, nil)
}

func (h *Hub) Broadcast(msg *pb.ChatMessage, exclude *Client) {
	data, err := proto.Marshal(msg)
	if err != nil {
		log.Printf("marshal: %v", err)
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if c == exclude {
			continue
		}
		select {
		case c.Send <- data:
		default:
		}
	}
}

func (h *Hub) SendTo(c *Client, msg *pb.ChatMessage) {
	data, err := proto.Marshal(msg)
	if err != nil {
		return
	}
	select {
	case c.Send <- data:
	default:
	}
}

func (h *Hub) OnJoin(c *Client) {
	now := time.Now().UnixMilli()
	avatar := GenerateAvatar(c.UserID())

	h.mu.RLock()
	others := make([]*Client, 0, len(h.clients))
	for other := range h.clients {
		if other != c {
			others = append(others, other)
		}
	}
	h.mu.RUnlock()

	for _, other := range others {
		h.SendTo(c, &pb.ChatMessage{
			MessageId:  NewID(),
			Seq:        h.NextSeq(),
			Type:       pb.MessageType_JOIN,
			SenderId:   other.UserID(),
			SenderName: other.SenderName(),
			Timestamp:  now,
			AvatarSvg:  GenerateAvatar(other.UserID()),
		})
	}

	h.SendTo(c, &pb.ChatMessage{
		MessageId:  NewID(),
		Seq:        h.NextSeq(),
		Type:       pb.MessageType_JOIN,
		SenderId:   c.UserID(),
		SenderName: c.SenderName(),
		Timestamp:  now,
		Content:    "__welcome__",
		AvatarSvg:  avatar,
	})

	h.Broadcast(&pb.ChatMessage{
		MessageId:  NewID(),
		Seq:        h.NextSeq(),
		Type:       pb.MessageType_JOIN,
		SenderId:   c.UserID(),
		SenderName: c.SenderName(),
		Timestamp:  now,
		AvatarSvg:  avatar,
	}, c)
}

func (h *Hub) OnLeave(c *Client) {
	h.Broadcast(&pb.ChatMessage{
		MessageId:  NewID(),
		Seq:        h.NextSeq(),
		Type:       pb.MessageType_LEAVE,
		SenderId:   c.UserID(),
		SenderName: c.SenderName(),
		Timestamp:  time.Now().UnixMilli(),
	}, nil)
}

func (h *Hub) HandleMessage(c *Client, msg *pb.ChatMessage) {
	debugf("[recv] from=%s userID=%s type=%d content=%q imgLen=%d",
		c.SenderName(), c.UserID(), msg.Type, msg.Content, len(msg.ImageData))

	msg.MessageId = NewID()
	msg.Timestamp = time.Now().UnixMilli()
	msg.Seq = h.NextSeq()
	msg.SenderId = c.UserID()
	msg.SenderName = c.SenderName()

	switch msg.Type {
	case pb.MessageType_RENAME:
		h.handleRename(c, msg)
	case pb.MessageType_MESSAGE:
		h.handleChat(c, msg)
	default:
		debugf("[recv] unknown type %d", msg.Type)
	}
}

func (h *Hub) handleRename(c *Client, msg *pb.ChatMessage) {
	newName := strings.TrimSpace(msg.Content)
	if newName == "" {
		newName = h.GenerateNickname()
	}
	if newName == "" || len([]rune(newName)) > 32 {
		return
	}

	oldID := c.UserID()
	oldName := c.SenderName()

	newID, newName := h.ChangeIdentity(c.ClientID(), newName)
	c.SetIdentity(newID, newName)

	msg.SenderId = newID
	msg.SenderName = newName
	msg.Content = oldName
	msg.ReplyToSenderId = oldID
	msg.AvatarSvg = GenerateAvatar(newID)

	h.Broadcast(msg, nil)
}

func (h *Hub) handleChat(c *Client, msg *pb.ChatMessage) {
	if msg.Content == "" && len(msg.ImageData) == 0 {
		debugf("[chat] skip empty message")
		return
	}

	if msg.ReplyTo != "" {
		orig, err := h.Store().GetMessageByID(context.Background(), msg.ReplyTo)
		if err == nil {
			msg.ReplyToSenderId = orig.SenderID
			msg.ReplyToPreview = buildPreview(orig.SenderName, orig.Content, orig.ImageData)
		} else {
			msg.ReplyTo = ""
		}
	}

	err := h.Store().InsertMessage(context.Background(), store.InsertMessageParams{
		MessageID:       msg.MessageId,
		Seq:             int64(msg.Seq),
		Type:            int64(msg.Type),
		SenderID:        msg.SenderId,
		SenderName:      msg.SenderName,
		Timestamp:       msg.Timestamp,
		Content:         msg.Content,
		ImageData:       msg.ImageData,
		ReplyTo:         msg.ReplyTo,
		ReplyToSenderID: msg.ReplyToSenderId,
		ReplyToPreview:  msg.ReplyToPreview,
	})
	if err != nil {
		log.Printf("insert: %v", err)
	}

	h.Broadcast(msg, nil)
}

func (h *Hub) GenerateNickname() string {
	mod, err1 := h.Store().GetRandomModifier(context.Background())
	noun, err2 := h.Store().GetRandomNoun(context.Background())
	if err1 == nil && err2 == nil {
		name := RandomNickname(mod, noun)
		if name != "" {
			return name
		}
	}
	return "anon-" + NewID()[:6]
}

func buildPreview(name, content string, image []byte) string {
	if content != "" {
		runes := []rune(content)
		if len(runes) > 40 {
			content = string(runes[:40]) + "…"
		}
		return name + ": " + content
	}
	if len(image) > 0 {
		return name + ": [图片]"
	}
	return name
}

func NewID() string {
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}