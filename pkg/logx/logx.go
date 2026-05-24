package logx

import (
	"context"
	"os"

	"github.com/sirupsen/logrus"
)

func init() {
	logrus.SetFormatter(&logrus.TextFormatter{})
	logrus.SetLevel(logrus.TraceLevel)
	logrus.SetOutput(os.Stderr)
}

func WithContext(ctx context.Context) *logrus.Entry {
	return logrus.WithContext(ctx)
}

func New() *logrus.Entry {
	return logrus.WithFields(logrus.Fields{})
}
