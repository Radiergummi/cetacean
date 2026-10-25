package docker

import (
	"context"

	"github.com/radiergummi/cetacean/internal/cache"
)

// AttributeTo registers every write made under a context carrying a
// cache.Actor with attributions, so the history entry it causes names the
// actor. Called once, before the client is shared.
func (c *Client) AttributeTo(attributions *cache.Attributions) {
	c.attributions = attributions
}

// expect registers the write x describes before it is made; the returned
// function withdraws it when handed the error of a write that failed.
func (c *Client) expect(ctx context.Context, x cache.Expectation) (settle func(error)) {
	actor, ok := cache.ActorFromContext(ctx)
	if c.attributions == nil || !ok {
		return func(error) {}
	}

	withdraw := c.attributions.Expect(x, actor)

	return func(err error) {
		if err != nil {
			withdraw()
		}
	}
}
