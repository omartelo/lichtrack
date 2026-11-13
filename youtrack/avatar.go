package youtrack

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// A picture is drawn a couple of dozen pixels wide; a file past this is not
// the thumbnail the instance was asked for.
const maxAvatarBytes = 64 << 10

// ErrAvatarFormat is a picture that is neither PNG nor JPEG, such as the SVG
// YouTrack generates for a user who uploaded none.
var ErrAvatarFormat = errors.New("youtrack: avatar is neither PNG nor JPEG")

// Avatar downloads a user's picture from User.AvatarURL, which is a path on
// the instance or an absolute URL.
func (c *Client) Avatar(ctx context.Context, avatarURL string) ([]byte, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("youtrack: base url: %w", err)
	}
	ref, err := url.Parse(avatarURL)
	if err != nil {
		return nil, fmt.Errorf("youtrack: avatar url: %w", err)
	}
	target := base.ResolveReference(ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("youtrack: build request: %w", err)
	}
	// The picture may live on another host, which must not see the token.
	if target.Host == base.Host {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("youtrack: avatar: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, newAPIError(resp)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAvatarBytes+1))
	if err != nil {
		return nil, fmt.Errorf("youtrack: avatar: %w", err)
	}
	if len(data) > maxAvatarBytes {
		return nil, fmt.Errorf("youtrack: avatar is over %d bytes", maxAvatarBytes)
	}
	switch http.DetectContentType(data) {
	case "image/png", "image/jpeg":
		return data, nil
	}
	return nil, ErrAvatarFormat
}
