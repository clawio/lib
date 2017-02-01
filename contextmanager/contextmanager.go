package contextmanager

import (
	"context"
	"github.com/clawio/lib"
	"github.com/go-kit/kit/log/levels"
)

type manager struct{}

type contextKey int

const (
	// userKey is the key to use when storing an entities.User into a context.
	userKey contextKey = iota

	// logKey is the key to use when storing an *logrus.Entry into a context.
	logKey contextKey = iota

	// tokenKey is the key to use when storing a JWT token (string) into a context.
	tokenKey contextKey = iota

	// traceIDKey is the key to use when storing a trace identifies into a context.
	traceIDKey contextKey = iota
)

func New() lib.ContextManager {
	return &manager{}
}

func (m *manager) SetUser(ctx context.Context, user lib.User) context.Context {
	return context.WithValue(ctx, userKey, user)
}

func (m *manager) MustGetUser(ctx context.Context) lib.User {
	return ctx.Value(userKey).(lib.User)
}

func  (m *manager) GetUser(ctx context.Context) (lib.User, bool) {
	user, ok := ctx.Value(userKey).(lib.User)
	return user, ok
}

func (m *manager) SetLog(ctx context.Context, log *levels.Levels) context.Context {
	return context.WithValue(ctx, logKey, log)
}

func (m *manager) MustGetLog(ctx context.Context) *levels.Levels {
	return ctx.Value(logKey).(*levels.Levels)
}

func  (m *manager) GetLog(ctx context.Context) (*levels.Levels, bool) {
	logger, ok := ctx.Value(logKey).(*levels.Levels)
	return logger, ok
}

func (m *manager) SetAccessToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenKey, token)
}

func (m *manager) MustGetAccessToken(ctx context.Context) string {
	return ctx.Value(tokenKey).(string)
}

func  (m *manager) GetAccessToken(ctx context.Context) (string, bool) {
	token, err := ctx.Value(tokenKey).(string)
	return token, err
}

func (m *manager) SetTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

func (m *manager) MustGetTraceID(ctx context.Context) string {
	return ctx.Value(traceIDKey).(string)
}

func  (m *manager) GetTraceID(ctx context.Context) (string, bool) {
	traceID, err := ctx.Value(traceIDKey).(string)
	return traceID, err
}
