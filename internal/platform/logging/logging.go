package logging

import (
	"log/slog"
	"os"
)

// New returns a JSON slog.Logger at the given level.
func New(level string) *slog.Logger {
	lvl := slog.LevelInfo
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(h)
}

// MaskDoc masks a CPF/CNPJ for logs, never emitting the full document.
func MaskDoc(doc string) string {
	switch len(doc) {
	case 11: // CPF: ***.***.NNN-**
		return "***.***." + doc[6:9] + "-**"
	case 14: // CNPJ: **.***.***/NNNN-**
		return "**.***.***/" + doc[8:12] + "-**"
	case 0:
		return ""
	default:
		return "****"
	}
}
