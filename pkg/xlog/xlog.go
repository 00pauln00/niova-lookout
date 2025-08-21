package xlog

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/sirupsen/logrus"
)

const (
	TRACE int = iota
	DEBUG
	INFO
	WARN
	ERROR
	FATAL
)

var Xlog *logrus.Logger

func Trace(args ...interface{}) {
	Xlog.Trace(args...)
}

func Debug(args ...interface{}) {
	Xlog.Debug(args...)
}

func Info(args ...interface{}) {
	Xlog.Info(args...)
}

func Warn(args ...interface{}) {
	Xlog.Warn(args...)
}

func Error(args ...interface{}) {
	Xlog.Error(args...)
}

func Fatal(args ...interface{}) {
	Xlog.Fatal(args...)
}

func Tracef(format string, args ...interface{}) {
	Xlog.Tracef(format, args...)
}

func Debugf(format string, args ...interface{}) {
	Xlog.Debugf(format, args...)
}

func Infof(format string, args ...interface{}) {
	Xlog.Infof(format, args...)
}

func Warnf(format string, args ...interface{}) {
	Xlog.Warnf(format, args...)
}

func Errorf(format string, args ...interface{}) {
	Xlog.Errorf(format, args...)
}

func Fatalf(format string, args ...interface{}) {
	Xlog.Fatalf(format, args...)
}

func WithLevel(level int, format string, args ...interface{}) {
	switch level {
	case TRACE:
		Xlog.Tracef(format, args...)
	case DEBUG:
		Xlog.Debugf(format, args...)
	case INFO:
		Xlog.Infof(format, args...)
	case WARN:
		Xlog.Warnf(format, args...)
	case ERROR:
		Xlog.Errorf(format, args...)
	case FATAL:
		Xlog.Fatalf(format, args...)
	default:
		Xlog.Printf(format, args...)
	}
}

func WithErr(err error, errLevel int, okLevel int, format string,
	args ...interface{}) {

	level := okLevel

	if err != nil {
		level = errLevel
	}

	WithLevel(level, format, args)
}

func WithDepth(level int, depth int, format string, args ...interface{}) {
	// Use logrus standard logger
	entry := Xlog.WithField("depth", depth)

	switch level {
	case TRACE:
		entry.Tracef(format, args...)
	case DEBUG:
		entry.Debugf(format, args...)
	case INFO:
		entry.Infof(format, args...)
	case WARN:
		entry.Warnf(format, args...)
	case ERROR:
		entry.Errorf(format, args...)
	case FATAL:
		entry.Fatalf(format, args...)
	default:
		entry.Printf(format, args...)
	}
}

func IsLevelEnabled(level int) bool {
	switch level {
	case TRACE:
		return Xlog.IsLevelEnabled(logrus.TraceLevel)
	case DEBUG:
		return Xlog.IsLevelEnabled(logrus.DebugLevel)
	case INFO:
		return Xlog.IsLevelEnabled(logrus.InfoLevel)
	case WARN:
		return Xlog.IsLevelEnabled(logrus.WarnLevel)
	case ERROR:
		return Xlog.IsLevelEnabled(logrus.ErrorLevel)
	case FATAL:
		return Xlog.IsLevelEnabled(logrus.FatalLevel)
	default:
	}

	return false
}

func goroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	// Example: "goroutine 18 [running]:\n"
	line := bytes.SplitN(buf[:n], []byte(" "), 3)
	id, _ := strconv.ParseUint(string(line[1]), 10, 64)
	return id
}

type MyFormatter struct{}

func (f *MyFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	// Format the timestamp
	ts := entry.Time.Format("2006-01-02T15:04:05-07:00")

	// Shorten file name
	fileBase := "unknown"
	funcBase := "unknown"
	line := 0
	depth := 0

	if d, ok := entry.Data["depth"].(int); ok {
		depth = d
	}

	var fn *runtime.Func

	for i := 0; i < 100; i++ {
		pc, file, xline, ok := runtime.Caller(i)
		if !ok {
			break
		}

		fn = runtime.FuncForPC(pc)

		if !strings.Contains(fn.Name(), "logrus") &&
			!strings.Contains(fn.Name(), "Logger") &&
			!strings.Contains(fn.Name(), "xlog") {
			if depth == 0 {
				fileBase = path.Base(file)
				funcBase = path.Base(fn.Name())
				line = xline
				break
			} else {
				depth--
			}
		}
	}

	// Upper-case first 4 letters of level (DEBU, INFO, WARN, etc)
	lvl := strings.ToUpper(entry.Level.String())
	if len(lvl) > 4 {
		lvl = lvl[:4]
	}
	lvl = fmt.Sprintf("%-4s", lvl) // pad to 4 chars

	trimmed := strings.TrimSpace(entry.Message)

	// Build final string
	msg := fmt.Sprintf("%s[%s][%d:%d]%s:%d %s :: %s\n",
		lvl, ts, syscall.Gettid(), goroutineID(), fileBase, line,
		funcBase, trimmed)

	trimmed = ""
	fileBase = ""
	funcBase = ""

	return []byte(msg), nil
}

func InitXlog(logFilePath string, logLevel *string) {
	// Create a single shared logger instance
	Xlog = logrus.New()
	Xlog.SetFormatter(&MyFormatter{})

	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		level, err = logrus.ParseLevel("warn")
		if err != nil {
			panic("failed to set default log level")
		}
	}

	Xlog.SetLevel(level)

	if logFilePath != "" {
		// Open the log file (file-only output, no stderr)
		file, err := os.OpenFile(logFilePath,
			os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			panic(fmt.Sprintf("Error opening log file: %v", err))
		}
		Xlog.SetOutput(file)
	} else {
		Xlog.SetOutput(os.Stderr)
	}
}
