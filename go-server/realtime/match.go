package realtime

import (
	"context"
	"log"
)

// HandleMatchInvite handles an incoming match challenge from client c to payload.Username
func (c *Client) HandleMatchInvite(payload MatchInvitePayload) error {
	if payload.Username == c.Username {
		return nil // Cannot challenge yourself
	}

	inviteBytes, err := EncodeMessage(TypeInviteRecv, MatchInvitePayload{
		Username: c.Username,
		Status:   "pending",
	})
	if err != nil {
		return err
	}

	c.Hub.SendToUsername(payload.Username, inviteBytes)
	return nil
}

// HandleMatchInviteResponse handles an accept/decline response from client c to challenger payload.Username
func (c *Client) HandleMatchInviteResponse(payload MatchInvitePayload) error {
	challenger := payload.Username
	if challenger == c.Username {
		return nil
	}

	if payload.Status == "accepted" {
		matchID, err := c.Hub.store.CreateMatch(context.Background(), challenger, c.Username)
		if err != nil {
			log.Printf("[WS] Failed to create match between %s and %s: %v", challenger, c.Username, err)
			return err
		}

		challengerMsg, err := EncodeMessage(TypeMatchStarted, MatchSessionPayload{
			MatchID:  matchID,
			Opponent: c.Username,
		})
		if err != nil {
			return err
		}
		c.Hub.SendToUsername(challenger, challengerMsg)

		selfMsg, err := EncodeMessage(TypeMatchStarted, MatchSessionPayload{
			MatchID:  matchID,
			Opponent: challenger,
		})
		if err != nil {
			return err
		}
		c.TrySend(selfMsg)
		return nil
	}

	if payload.Status == "declined" {
		declineBytes, err := EncodeMessage(TypeInviteResponse, MatchInvitePayload{
			Username: c.Username,
			Status:   "declined",
		})
		if err != nil {
			return err
		}
		c.Hub.SendToUsername(challenger, declineBytes)
	}

	return nil
}

// HandleMatchInviteCancel handles challenger c canceling their pending invite to payload.Username
func (c *Client) HandleMatchInviteCancel(payload MatchInvitePayload) error {
	if payload.Username == c.Username {
		return nil
	}

	cancelBytes, err := EncodeMessage(TypeInviteCancel, MatchInvitePayload{
		Username: c.Username,
		Status:   "canceled",
	})
	if err != nil {
		return err
	}

	c.Hub.SendToUsername(payload.Username, cancelBytes)
	return nil
}
