package web

import (
	"context"
	"time"
)

// contextWithTimeout layers a fresh deadline on top of an existing
// context, taking the *earlier* deadline when one already exists. We use
// it from handlers that perform long server-side work (Backup) so the
// per-handler bound doesn't override a shorter parent deadline (e.g.
// http.Server.WriteTimeout).
func contextWithTimeout(parent context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	if dl, ok := parent.Deadline(); ok {
		if time.Until(dl) < limit {
			return context.WithCancel(parent)
		}
	}
	return context.WithTimeout(parent, limit)
}
