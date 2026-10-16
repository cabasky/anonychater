package hub

import (
	"context"
	"database/sql"
	"log"
	"time"

	"anonychater/internal/store"
)

func (h *Hub) EnsureIdentity(clientID string) (userID, name string) {
	ctx := context.Background()

	row, err := h.Store().GetIdentity(ctx, clientID)
	if err == nil {
		return row.UserID, row.Name
	}
	if err != sql.ErrNoRows {
		log.Printf("get identity: %v", err)
	}

	userID = NewID()
	name = h.GenerateNickname()
	h.saveIdentity(clientID, userID, name)
	return
}

func (h *Hub) ChangeIdentity(clientID, newName string) (userID, name string) {
	userID = NewID()
	name = newName
	h.saveIdentity(clientID, userID, name)
	return
}

func (h *Hub) saveIdentity(clientID, userID, name string) {
	err := h.Store().UpsertIdentity(context.Background(), store.UpsertIdentityParams{
		ClientID:  clientID,
		UserID:    userID,
		Name:      name,
		UpdatedAt: time.Now().UnixMilli(),
	})
	if err != nil {
		log.Printf("upsert identity: %v", err)
	}
}