//go:build !windows

package discovery

import (
	"context"
	"time"
)

func (b *Browser) browseOnce(ctx context.Context, duration time.Duration) {
	b.browseOnceZeroconf(ctx, duration)
}
