package realtime

// HandleMatchInvite handles an incoming match challenge from client c to payload.Username
func (c *Client) HandleMatchInvite(payload MatchInvitePayload) error {
	c.Hub.matchAction <- MatchAction{
		Type:   ActionInviteSend,
		Sender: c,
		Target: payload.Username,
	}
	return nil
}

// HandleMatchInviteResponse handles an accept/decline response from client c to challenger payload.Username
func (c *Client) HandleMatchInviteResponse(payload MatchInvitePayload) error {
	c.Hub.matchAction <- MatchAction{
		Type:   ActionInviteResponse,
		Sender: c,
		Target: payload.Username,
		Status: payload.Status,
	}
	return nil
}

// HandleMatchInviteCancel handles challenger c canceling their pending invite to payload.Username
func (c *Client) HandleMatchInviteCancel(payload MatchInvitePayload) error {
	c.Hub.matchAction <- MatchAction{
		Type:   ActionInviteCancel,
		Sender: c,
		Target: payload.Username,
	}
	return nil
}
