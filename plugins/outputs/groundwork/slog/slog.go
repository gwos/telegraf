package slog

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"

	"github.com/influxdata/telegraf"
)

// SLogHandler translates slog.Record into telegraf.Logger call
// inspired by https://github.com/golang/example/blob/master/slog-handler-guide/README.md
type SLogHandler struct {
	attrs  []slog.Attr
	groups []string

	once sync.Once

	GroupsFieldName  string
	MessageFieldName string

	Log telegraf.Logger
}

func (h *SLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	l := h.Log.Level()
	switch level {
	case slog.LevelDebug:
		return l >= telegraf.Debug
	case slog.LevelInfo:
		return l >= telegraf.Info
	case slog.LevelWarn:
		return l >= telegraf.Warn
	case slog.LevelError:
		return l >= telegraf.Error
	default:
		return l >= telegraf.Info
	}
}

func (h *SLogHandler) Handle(ctx context.Context, r slog.Record) error {
	h.once.Do(func() {
		if h.GroupsFieldName == "" {
			h.GroupsFieldName = "logger"
		}
		if h.MessageFieldName == "" {
			h.MessageFieldName = "message"
		}
	})

	var attrs []slog.Attr
	attrs = append(attrs, slog.String(h.MessageFieldName, r.Message))
	attrs = append(attrs, slog.String(h.GroupsFieldName, strings.Join(h.groups, ",")))
	for _, attr := range h.attrs {
		if v, ok := attr.Value.Any().(json.RawMessage); ok {
			attrs = append(attrs, slog.String(attr.Key, string(v)))
			continue
		}
		attrs = append(attrs, attr)
	}
	r.Attrs(func(attr slog.Attr) bool {
		if v, ok := attr.Value.Any().(json.RawMessage); ok {
			attrs = append(attrs, slog.String(attr.Key, string(v)))
			return true
		}
		attrs = append(attrs, attr)
		return true
	})

	var handle func(args ...interface{})
	switch r.Level {
	case slog.LevelDebug:
		handle = h.Log.Debug
	case slog.LevelInfo:
		handle = h.Log.Info
	case slog.LevelWarn:
		handle = h.Log.Warn
	case slog.LevelError:
		handle = h.Log.Error
	default:
		handle = h.Log.Info
	}
	handle(attrs)

	return nil
}

func (h *SLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	nested := &SLogHandler{GroupsFieldName: h.GroupsFieldName, MessageFieldName: h.MessageFieldName, Log: h.Log}
	nested.attrs = append(nested.attrs, h.attrs...)
	nested.groups = append(nested.groups, h.groups...)
	nested.attrs = append(nested.attrs, attrs...)
	return nested
}

func (h *SLogHandler) WithGroup(name string) slog.Handler {
	nested := &SLogHandler{GroupsFieldName: h.GroupsFieldName, MessageFieldName: h.MessageFieldName, Log: h.Log}
	nested.attrs = append(nested.attrs, h.attrs...)
	nested.groups = append(nested.groups, h.groups...)
	nested.groups = append(nested.groups, name)
	return nested
}
