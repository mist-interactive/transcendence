package realtime

import "errors"

// HandleMatchInvite is invoked when an online player issues a "match_invite_send" event.
// It packages the challenge into a MatchAction and pushes it to the Hub's action channel,
// where it will be recorded in the in-memory invites registry and forwarded to the target.
func (c *Client) HandleMatchInvite(payload MatchInvitePayload) error {
	if payload.Username == c.Username {
		return errors.New("cannot invite yourself to a match")
	}
	c.Hub.matchAction <- MatchAction{
		Type:   ActionInviteSend,
		Sender: c,
		Target: payload.Username,
	}
	return nil
}

// HandleMatchInviteResponse is invoked when an invited player answers with "match_invite_response".
// It dispatches the response (accepted/declined) to the Hub. The Hub will verify whether an invite
// was actually pending before creating a match or notifying the challenger of a decline.
func (c *Client) HandleMatchInviteResponse(payload MatchInvitePayload) error {
	c.Hub.matchAction <- MatchAction{
		Type:   ActionInviteResponse,
		Sender: c,
		Target: payload.Username,
		Status: payload.Status,
	}
	return nil
}

// HandleMatchInviteCancel is invoked when a challenger withdraws their pending invite via "match_invite_cancel".
// It dispatches the cancellation to the Hub to remove the challenge from memory and notify the target.
func (c *Client) HandleMatchInviteCancel(payload MatchInvitePayload) error {
	c.Hub.matchAction <- MatchAction{
		Type:   ActionInviteCancel,
		Sender: c,
		Target: payload.Username,
	}
	return nil
}
