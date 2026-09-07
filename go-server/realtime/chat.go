package realtime

import (
	"context"
	"log/slog"
)

func (c *Client) HandleSendMsg(payload DMPayload) error {
	if payload.Username == c.Username {
		slog.Warn("Direct message rejected: self message", "username", c.Username)
		return nil // Cannot DM yourself
	}
	savedMsg, err := c.Hub.store.SaveMessage(context.Background(), c.UserID, payload.Username, payload.Content) //persist message in DB
	if err != nil {
		slog.Error("Failed to save direct message in DB", "sender", c.Username, "recipient", payload.Username, "error", err)
		return err
	}
	// encode the message to be sent
	deliveryBytes, err := EncodeMessage(TypeDMRecv, DMPayload{
		ID:        savedMsg.ID,
		Username:  c.Username,
		Content:   savedMsg.Content,
		CreatedAt: &savedMsg.CreatedAt,
	})
	if err != nil {
		slog.Error("Failed to encode direct message", "error", err)
		return err
	}
	slog.Info("Direct message dispatched", "sender", c.Username, "recipient_id", savedMsg.RecipientID, "message_id", savedMsg.ID)
	// send the message
	c.Hub.SendToUser(savedMsg.RecipientID, deliveryBytes)
	return nil
}
