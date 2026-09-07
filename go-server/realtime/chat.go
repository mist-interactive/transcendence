package realtime

import (
	"context"
	"log"
)

func (c *Client) HandleSendMsg(payload DMPayload) error {
	if payload.Username == c.Username {
		return nil // Cannot DM yourself
	}
	savedMsg, err := c.Hub.store.SaveMessage(context.Background(), c.UserID, payload.Username, payload.Content) //persist message in DB
	if err != nil {
		log.Printf("[WS] Failed to save DM from %s to %s: %v", c.Username, payload.Username, err)
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
		return err
	}
	// send the message
	c.Hub.SendToUser(savedMsg.RecipientID, deliveryBytes)
	return nil
}
