package log

import (
	"context"
	"log"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/redpanda-data/common-go/kube"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	ctrl "sigs.k8s.io/controller-runtime"
)

var Logger logr.Logger

func Error(err error, msg string, keysAndValues ...any) {
	Logger.Error(err, msg, keysAndValues...)
}
func V(level int) logr.Logger {
	return Logger.V(level)
}
func Info(msg string, keysAndValues ...any) {
	Logger.Info(msg, keysAndValues...)
}
func WithName(name string) logr.Logger {
	return Logger.WithName(name)
}
func WithValues(keysAndValues ...any) logr.Logger {
	return Logger.WithValues(keysAndValues...)
}

func init() {
	kube.SetContextLoggerFactory(func(ctx context.Context) logr.Logger {
		return ctrl.LoggerFrom(ctx)
	})
	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(zapcore.Level(-1))
	zaplogger, err := config.Build(zap.AddStacktrace(zapcore.ErrorLevel))
	if err != nil {
		log.Fatal(err)
	}
	Logger = zapr.NewLogger(zaplogger)
	ctrl.SetLogger(Logger)
	kube.SetDebounceErrorFunc(DebounceError)
}
