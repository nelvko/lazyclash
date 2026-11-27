package mihomo

import (
	"context"
	"net/http"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

// UpdateGeo waits for Mihomo to update the databases it has initialized.
// Closing the connection does not cancel the core's updater.
func (c Client) UpdateGeo(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	const limit = 2 * time.Minute
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	var cl http.Client
	if c.HTTP != nil {
		cl = *c.HTTP
	}
	cl.Timeout = limit
	c.HTTP = &cl
	response, err := c.open(ctx, http.MethodPost, "/configs/geo", nil)
	if err != nil {
		if ctx.Err() != nil {
			return diagnostic.New("diagnostic.geo.updateWaitStopped", "stopped waiting for the GEO update, Mihomo may continue updating: %w", ctx.Err())
		}
		return err
	}
	_ = response.Close()
	return nil
}
