package model

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RepoLog mirrors a8.locus.ResolvedRepo.RepoLoggingService: a per-request
// accumulator of debug lines (dumped by ?action=debug) that also tees to the
// process logger via the optional sink.
type RepoLog struct {
	mu      sync.Mutex
	started time.Time
	lines   []string
	sink    func(level, msg string, err error)
}

// NewRepoLog creates a RepoLog. sink may be nil.
func NewRepoLog(sink func(level, msg string, err error)) *RepoLog {
	return &RepoLog{started: time.Now(), sink: sink}
}

func (l *RepoLog) log(level, msg string, err error) {
	if l == nil {
		return
	}
	l.mu.Lock()
	delta := time.Since(l.started).Milliseconds()
	line := fmt.Sprintf("%4d - %5s - %s", delta, level, msg)
	if err != nil {
		line += " - " + err.Error()
	}
	l.lines = append(l.lines, line)
	sink := l.sink
	l.mu.Unlock()
	if sink != nil {
		sink(level, msg, err)
	}
}

func (l *RepoLog) Trace(msg string)            { l.log("TRACE", msg, nil) }
func (l *RepoLog) Debug(msg string)            { l.log("DEBUG", msg, nil) }
func (l *RepoLog) Info(msg string)             { l.log("INFO", msg, nil) }
func (l *RepoLog) Warn(msg string, err error)  { l.log("WARN", msg, err) }
func (l *RepoLog) Error(msg string, err error) { l.log("ERROR", msg, err) }

func (l *RepoLog) Tracef(format string, a ...any) { l.Trace(fmt.Sprintf(format, a...)) }
func (l *RepoLog) Debugf(format string, a ...any) { l.Debug(fmt.Sprintf(format, a...)) }

// Lines returns the accumulated log lines in order.
func (l *RepoLog) Lines() []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.lines))
	copy(out, l.lines)
	return out
}

type repoLogKey struct{}

// WithRepoLog attaches a RepoLog to a context.
func WithRepoLog(ctx context.Context, l *RepoLog) context.Context {
	return context.WithValue(ctx, repoLogKey{}, l)
}

// LogFrom returns the context's RepoLog, or a no-op (nil-safe) one.
func LogFrom(ctx context.Context) *RepoLog {
	if l, ok := ctx.Value(repoLogKey{}).(*RepoLog); ok {
		return l
	}
	return nil
}
