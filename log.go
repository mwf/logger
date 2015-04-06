// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package log implements a simple logging package. It defines a type, Logger,
// with methods for formatting output. It also has a predefined 'standard'
// Logger accessible through helper functions Print[f|ln], Fatal[f|ln], and
// Panic[f|ln], which are easier to use than creating a Logger manually.
// That logger writes to standard error and prints the date and time
// of each logged message.
package logger

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// These constants identify the log levels in order of increasing severity
const (
	DEBUG = iota
	INFO
	WARNING
	ERROR
)

// These flags define which text to prefix to each log entry generated by the Logger.
const (
	// Bits or'ed together to control what's printed. There is no control over the
	// order they appear (the order listed here) or the format they present (as
	// described in the comments).  A colon appears after these items:
	//	2009.01.23 01:23:23.123123 /a/b/c/d.go:23: message
	Ldate         = 1 << iota     // the date: 2009.01.23
	Ltime                         // the time: 01:23:23
	Lmicroseconds                 // microsecond resolution: 01:23:23.123123.  assumes Ltime.
	Llongfile                     // full file name and line number: /a/b/c/d.go:23
	Lshortfile                    // final file name element and line number: d.go:23. overrides Llongfile
	LstdFlags     = Ldate | Ltime // initial values for the standard logger
)

var levelChar = [4]byte{'D', 'I', 'W', 'E'}

// A Logger represents an active logging object that generates lines of
// output to an io.Writer.  Each logging operation makes a single call to
// the Writer's Write method.  A Logger can be used simultaneously from
// multiple goroutines; it guarantees to serialize access to the Writer.
type Logger struct {
	mu     sync.Mutex // ensures atomic writes; protects the following fields
	prefix string     // prefix to write at beginning of each line
	flag   int        // properties
	level  int        // verbosity level
	out    io.Writer  // destination for output
	buf    []byte     // for accumulating text to write
}

// New creates a new Logger.   The out variable sets the
// destination to which log data will be written.
// The prefix appears at the beginning of each generated log line.
// The flag argument defines the logging properties.
func New(out io.Writer, prefix string, level int) *Logger {

	return &Logger{
		out:    out,
		prefix: prefix,
		flag:   LstdFlags,
		level:  level,
	}
}

var std = New(os.Stderr, "", DEBUG)

// Cheap integer to fixed-width decimal ASCII.  Give a negative width to avoid zero-padding.
func itoa(buf *[]byte, i int, wid int) {
	// Assemble decimal in reverse order.
	var b [20]byte
	bp := len(b) - 1
	for i >= 10 || wid > 1 {
		wid--
		q := i / 10
		b[bp] = byte('0' + i - q*10)
		bp--
		i = q
	}
	// i < 10
	b[bp] = byte('0' + i)
	*buf = append(*buf, b[bp:]...)
}

func formatHeader(buf *[]byte, t time.Time,
	file, prefix string, line, level, flag int) {

	if flag&(Ldate|Ltime|Lmicroseconds) != 0 {
		if flag&Ldate != 0 {
			year, month, day := t.Date()
			itoa(buf, year, 4)
			*buf = append(*buf, '.')
			itoa(buf, int(month), 2)
			*buf = append(*buf, '.')
			itoa(buf, day, 2)
			*buf = append(*buf, ' ')
		}
		if flag&(Ltime|Lmicroseconds) != 0 {
			hour, min, sec := t.Clock()
			itoa(buf, hour, 2)
			*buf = append(*buf, ':')
			itoa(buf, min, 2)
			*buf = append(*buf, ':')
			itoa(buf, sec, 2)
			if flag&Lmicroseconds != 0 {
				*buf = append(*buf, '.')
				itoa(buf, t.Nanosecond()/1e3, 6)
			}
			*buf = append(*buf, ' ')
		}
	}

	if flag&(Lshortfile|Llongfile) != 0 {
		if flag&Lshortfile != 0 {
			short := file
			for i := len(file) - 1; i > 0; i-- {
				if file[i] == '/' {
					short = file[i+1:]
					break
				}
			}
			file = short
		}
		*buf = append(*buf, file...)
		*buf = append(*buf, ':')
		itoa(buf, line, -1)
		*buf = append(*buf, ": "...)
	}

	if len(prefix) > 0 {
		*buf = append(*buf, prefix...)
		*buf = append(*buf, ' ')
	}

	*buf = append(*buf, '[', levelChar[level], ']', ' ')
}

// Output writes the output for a logging event.  The string s contains
// the text to print after the prefix specified by the flags of the
// Logger.  A newline is appended if the last character of s is not
// already a newline.  Calldepth is used to recover the PC and is
// provided for generality, although at the moment on all pre-defined
// paths it will be 2.
func (l *Logger) Output(calldepth, level int, s string) error {

	if level > ERROR || level < 0 {
		return fmt.Errorf("Unknown level %d", level)
	}

	now := time.Now() // get this early

	var file string
	var line int

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.level > level {
		return nil
	}

	if l.flag&(Lshortfile|Llongfile) != 0 {

		// release lock while getting caller info - it's expensive.
		l.mu.Unlock()
		var ok bool
		_, file, line, ok = runtime.Caller(calldepth)
		if !ok {
			file = "???"
			line = 0
		}
		l.mu.Lock()
	}
	l.buf = l.buf[:0]
	formatHeader(&l.buf, now, file, l.prefix, line, level, l.flag)
	l.buf = append(l.buf, s...)
	if len(s) > 0 && s[len(s)-1] != '\n' {
		l.buf = append(l.buf, '\n')
	}
	_, err := l.out.Write(l.buf)
	return err
}

// Debug calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Print.
func (l *Logger) Debug(v ...interface{}) {
	l.Output(2, DEBUG, fmt.Sprint(v...))
}

// Debugf calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Printf.
func (l *Logger) Debugf(format string, v ...interface{}) {
	l.Output(2, DEBUG, fmt.Sprintf(format, v...))
}

// Debugln calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Println.
func (l *Logger) Debugln(v ...interface{}) {
	l.Output(2, DEBUG, fmt.Sprintln(v...))
}

// Info calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Print.
func (l *Logger) Info(v ...interface{}) {
	l.Output(2, INFO, fmt.Sprint(v...))
}

// Infof calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Printf.
func (l *Logger) Infof(format string, v ...interface{}) {
	l.Output(2, INFO, fmt.Sprintf(format, v...))
}

// Infoln calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Println.
func (l *Logger) Infoln(v ...interface{}) {
	l.Output(2, INFO, fmt.Sprintln(v...))
}

// Warn calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Print.
func (l *Logger) Warn(v ...interface{}) {
	l.Output(2, WARNING, fmt.Sprint(v...))
}

// Warnf calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Printf.
func (l *Logger) Warnf(format string, v ...interface{}) {
	l.Output(2, WARNING, fmt.Sprintf(format, v...))
}

// Warnln calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Println.
func (l *Logger) Warnln(v ...interface{}) {
	l.Output(2, WARNING, fmt.Sprintln(v...))
}

// Error calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Print.
func (l *Logger) Error(v ...interface{}) {
	l.Output(2, ERROR, fmt.Sprint(v...))
}

// Errorf calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Printf.
func (l *Logger) Errorf(format string, v ...interface{}) {
	l.Output(2, ERROR, fmt.Sprintf(format, v...))
}

// Errorln calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Println.
func (l *Logger) Errorln(v ...interface{}) {
	l.Output(2, ERROR, fmt.Sprintln(v...))
}

// Flags returns the output flags for the logger.
func (l *Logger) Flags() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.flag
}

// SetFlags sets the output flags for the logger.
func (l *Logger) SetFlags(flag int) {
	l.mu.Lock()
	l.flag = flag
	l.mu.Unlock()
}

// Prefix returns the output prefix for the logger.
func (l *Logger) Prefix() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.prefix
}

// SetPrefix sets the output prefix for the logger.
func (l *Logger) SetPrefix(prefix string) {
	l.mu.Lock()
	l.prefix = prefix
	l.mu.Unlock()
}

func (l *Logger) Level() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.level
}

func (l *Logger) LevelString() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	switch l.level {
	case DEBUG:
		return "DEBUG"
	case ERROR:
		return "ERROR"
	case INFO:
		return "INFO"
	case WARNING:
		return "WARNING"
	default:
		return "UNKNOWN"
	}
}

func (l *Logger) SetLevel(level int) {
	l.mu.Lock()
	l.level = level
	l.mu.Unlock()
}

func (l *Logger) SetLevelString(level string) {

	level = strings.ToUpper(level)
	var lvl int

	switch level {
	case "DEBUG":
		lvl = DEBUG
	case "ERROR":
		lvl = ERROR
	case "INFO":
		lvl = INFO
	case "WARNING":
		lvl = WARNING
	default:
		return
	}

	l.mu.Lock()
	l.level = lvl
	l.mu.Unlock()
}

// SetOutput sets the output destination for the standard logger.
func SetOutput(w io.Writer) {
	std.mu.Lock()
	defer std.mu.Unlock()
	std.out = w
}

// Flags returns the output flags for the standard logger.
func Flags() int {
	return std.Flags()
}

// SetFlags sets the output flags for the standard logger.
func SetFlags(flag int) {
	std.SetFlags(flag)
}

// Prefix returns the output prefix for the standard logger.
func Prefix() string {
	return std.Prefix()
}

// SetPrefix sets the output prefix for the standard logger.
func SetPrefix(prefix string) {
	std.SetPrefix(prefix)
}

func Level() int {
	return std.Level()
}

func SetLevel(level int) {
	std.SetLevel(level)
}

// These functions write to the standard logger.

// Debug calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Print.
func Debug(v ...interface{}) {
	std.Output(2, DEBUG, fmt.Sprint(v...))
}

// Debugf calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Printf.
func Debugf(format string, v ...interface{}) {
	std.Output(2, DEBUG, fmt.Sprintf(format, v...))
}

// Debugln calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Println.
func Debugln(v ...interface{}) {
	std.Output(2, DEBUG, fmt.Sprintln(v...))
}

// Info calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Print.
func Info(v ...interface{}) {
	std.Output(2, INFO, fmt.Sprint(v...))
}

// Infof calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Printf.
func Infof(format string, v ...interface{}) {
	std.Output(2, INFO, fmt.Sprintf(format, v...))
}

// Infoln calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Println.
func Infoln(v ...interface{}) {
	std.Output(2, INFO, fmt.Sprintln(v...))
}

// Warn calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Print.
func Warn(v ...interface{}) {
	std.Output(2, WARNING, fmt.Sprint(v...))
}

// Warnf calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Printf.
func Warnf(format string, v ...interface{}) {
	std.Output(2, WARNING, fmt.Sprintf(format, v...))
}

// Warnln calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Println.
func Warnln(v ...interface{}) {
	std.Output(2, WARNING, fmt.Sprintln(v...))
}

// Error calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Print.
func Error(v ...interface{}) {
	std.Output(2, ERROR, fmt.Sprint(v...))
}

// Errorf calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Printf.
func Errorf(format string, v ...interface{}) {
	std.Output(2, ERROR, fmt.Sprintf(format, v...))
}

// Errorln calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Println.
func Errorln(v ...interface{}) {
	std.Output(2, ERROR, fmt.Sprintln(v...))
}
