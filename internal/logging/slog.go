package logging

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"go.uber.org/zap"
)

func Slog(logger *zap.Logger) *slog.Logger {
	if logger == nil {
		logger = zap.NewNop()
	}
	return slog.New(&zapHandler{logger: logger})
}

type zapHandler struct {
	logger *zap.Logger
	attrs  []slog.Attr
	groups []string
}

func (h *zapHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h *zapHandler) Handle(_ context.Context, record slog.Record) error {
	fields := h.fields(record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		fields = append(fields, h.field(attr))
		return true
	})

	switch {
	case record.Level >= slog.LevelError:
		h.logger.Error(record.Message, fields...)
	case record.Level >= slog.LevelWarn:
		h.logger.Warn(record.Message, fields...)
	case record.Level <= slog.LevelDebug:
		h.logger.Debug(record.Message, fields...)
	default:
		h.logger.Info(record.Message, fields...)
	}
	return nil
}

func (h *zapHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := h.clone()
	next.attrs = append(next.attrs, attrs...)
	return next
}

func (h *zapHandler) WithGroup(name string) slog.Handler {
	if strings.TrimSpace(name) == "" {
		return h
	}
	next := h.clone()
	next.groups = append(next.groups, name)
	return next
}

func (h *zapHandler) clone() *zapHandler {
	next := *h
	next.attrs = append([]slog.Attr(nil), h.attrs...)
	next.groups = append([]string(nil), h.groups...)
	return &next
}

func (h *zapHandler) fields(extra int) []zap.Field {
	fields := make([]zap.Field, 0, len(h.attrs)+extra)
	for _, attr := range h.attrs {
		fields = append(fields, h.field(attr))
	}
	return fields
}

func (h *zapHandler) field(attr slog.Attr) zap.Field {
	attr.Value = attr.Value.Resolve()
	key := h.key(attr.Key)
	switch attr.Value.Kind() {
	case slog.KindBool:
		return zap.Bool(key, attr.Value.Bool())
	case slog.KindDuration:
		return zap.Duration(key, attr.Value.Duration())
	case slog.KindFloat64:
		return zap.Float64(key, attr.Value.Float64())
	case slog.KindInt64:
		return zap.Int64(key, attr.Value.Int64())
	case slog.KindString:
		return zap.String(key, attr.Value.String())
	case slog.KindTime:
		return zap.Time(key, attr.Value.Time())
	case slog.KindUint64:
		return zap.Uint64(key, attr.Value.Uint64())
	default:
		if value, ok := attr.Value.Any().(time.Duration); ok {
			return zap.Duration(key, value)
		}
		return zap.Any(key, attr.Value.Any())
	}
}

func (h *zapHandler) key(key string) string {
	if len(h.groups) == 0 {
		return key
	}
	parts := make([]string, 0, len(h.groups)+1)
	parts = append(parts, h.groups...)
	parts = append(parts, key)
	return strings.Join(parts, ".")
}
