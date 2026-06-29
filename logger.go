package tencentcloud_cls_sdk_go

import (
	"go.uber.org/zap"
)

type Logger interface {
	Debug(msg string, args ...Field)
	Info(msg string, args ...Field)
	Warn(msg string, args ...Field)
	Error(msg string, args ...Field)
}

type Field struct {
	Key   string
	Value interface{}
}

type ZapLogger struct {
	logger *zap.Logger
}

func NewZapLogger(logger *zap.Logger) *ZapLogger {
	return &ZapLogger{
		logger: logger,
	}
}

func (z *ZapLogger) toArgs(args []Field) []zap.Field {
	var result = make([]zap.Field, len(args))
	for i, arg := range args {
		result[i] = zap.Any(arg.Key, arg.Value)
	}
	return result
}

func (z *ZapLogger) Debug(msg string, args ...Field) {
	z.logger.Debug(msg, z.toArgs(args)...)
}

func (z *ZapLogger) Info(msg string, args ...Field) {
	z.logger.Info(msg, z.toArgs(args)...)
}

func (z *ZapLogger) Warn(msg string, args ...Field) {
	z.logger.Warn(msg, z.toArgs(args)...)
}

func (z *ZapLogger) Error(msg string, args ...Field) {
	z.logger.Error(msg, z.toArgs(args)...)
}

// add logger init
func init() {
    // use json format log config
    config := zap.NewProductionConfig()
    config.Encoding = "json"
    config.OutputPaths = []string{"stdout"}
    config.ErrorOutputPaths = []string{"stderr"}
    
    logger, err := config.Build()
    if err != nil {
        panic(err)
    }
    zap.ReplaceGlobals(logger)
}

// provide get logger instance function
func GetZapLoggerAdapter() *ZapLogger {
    return NewZapLogger(zap.L())
}