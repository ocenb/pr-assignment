package logattr

import (
	"log/slog"
)

func Err(err error) slog.Attr {
	if err == nil {
		return slog.Any("error", nil)
	}
	return slog.String("error", err.Error())
}

func Op(op string) slog.Attr {
	return slog.String("op", op)
}
